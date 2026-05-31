using System.IO;
using System.Net.Sockets;

namespace Nx2Gui.Protocol;

// BrokerConnection is the nx2 host's client side of the wire protocol: it dials
// the broker (TCP), runs the control-plane connect flow (resolve -> fetch+cache
// -> select_app), then pumps the opaque Data plane. It is app-agnostic — Data
// payloads are handed to the caller verbatim (the guest interprets them).
public sealed class BrokerConnection : IDisposable
{
    private readonly TcpClient _tcp = new();
    private NetworkStream _stream = null!;
    private readonly Capsule _cache;
    private readonly SemaphoreSlim _writeLock = new(1, 1);

    // Raised on the read-loop thread for each Data frame from the broker.
    public event Action<byte[]>? DataReceived;
    public event Action<string>? StatusChanged;

    public BrokerConnection(Capsule cache) => _cache = cache;

    public async Task ConnectAsync(string host, int port, CancellationToken ct)
    {
        StatusChanged?.Invoke($"connecting to {host}:{port}…");
        await _tcp.ConnectAsync(host, port, ct);
        _stream = _tcp.GetStream();
        StatusChanged?.Invoke("connected");
    }

    // Resolve an app name to its content hash (control round-trip, setup phase).
    public async Task<string> ResolveAsync(string app, CancellationToken ct)
    {
        await SendControlAsync(Control.TypeResolve, new Control.Resolve { App = app }, ct);
        while (true)
        {
            var (t, payload) = await Wire.ReadFrameAsync(_stream, ct);
            if (t != FrameType.Control) continue;
            var (type, el) = Control.Parse(payload);
            if (type != Control.TypeResolved) continue;
            var r = Control.PayloadAs<Control.Resolved>(el);
            if (r.Error) throw new IOException($"resolve {app}: {r.Message}");
            return r.Hash;
        }
    }

    // Fetch the guest module by hash, cache-first; verifies the hash on download.
    public async Task<byte[]> FetchAsync(string hash, CancellationToken ct)
    {
        var cached = _cache.Get(hash);
        if (cached != null) return cached;

        await SendControlAsync(Control.TypeFetch, new Control.Fetch { Hash = hash }, ct);
        using var buf = new MemoryStream();
        while (true)
        {
            var (t, payload) = await Wire.ReadFrameAsync(_stream, ct);
            if (t != FrameType.Control) continue;
            var (type, el) = Control.Parse(payload);
            if (type != Control.TypeChunk) continue;
            var c = Control.PayloadAs<Control.Chunk>(el);
            if (c.Error) throw new IOException($"fetch {hash}: {c.Message}");
            if (c.Data != null) buf.Write(c.Data, 0, c.Data.Length);
            if (c.Done) break;
        }
        var data = buf.ToArray();
        _cache.Put(hash, data);
        return data;
    }

    public Task SelectAppAsync(string app, string session, CancellationToken ct) =>
        SendControlAsync(Control.TypeSelectApp, new Control.SelectApp { App = app, Session = session }, ct);

    // SendData relays opaque bytes to the broker (guest -> companion).
    public Task SendDataAsync(byte[] data, CancellationToken ct) => SendFrameAsync(FrameType.Data, data, ct);

    // RunReadLoop pumps frames until the connection closes; Data frames fire
    // DataReceived. Call after the setup round-trips (resolve/fetch) complete.
    public async Task RunReadLoopAsync(CancellationToken ct)
    {
        try
        {
            while (!ct.IsCancellationRequested)
            {
                var (t, payload) = await Wire.ReadFrameAsync(_stream, ct);
                if (t == FrameType.Data) DataReceived?.Invoke(payload);
            }
        }
        catch (Exception ex) when (ex is IOException or EndOfStreamException or OperationCanceledException)
        {
            StatusChanged?.Invoke("disconnected");
        }
    }

    private Task SendControlAsync(string type, object payload, CancellationToken ct) =>
        SendFrameAsync(FrameType.Control, Control.Marshal(type, payload), ct);

    private async Task SendFrameAsync(FrameType t, byte[] payload, CancellationToken ct)
    {
        await _writeLock.WaitAsync(ct);
        try { await Wire.WriteFrameAsync(_stream, t, payload, ct); }
        finally { _writeLock.Release(); }
    }

    public void Dispose() { try { _stream?.Dispose(); } catch { } _tcp.Dispose(); }
}

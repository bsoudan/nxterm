using System.Net;
using System.Net.Sockets;
using System.Text;

namespace NxtermGui.Protocol;

// Test-only introspection server. Enabled by setting NXTERM_TEST_HOOK to a port
// ("9300") or host:port ("0.0.0.0:9300"); unset, it does nothing and is never
// started. It serves newline-delimited JSON request/response over TCP so the Go
// e2e harness (running on the Linux host, reaching the guest via a QEMU
// hostfwd) can read the client's rendered grid + connection state and observe
// sync markers. This is the GUI analog of PtyIO's virtual screen + OSC acks; it
// is not part of normal client behavior.
//
// Request/response is one JSON object per line. Requests carry an "op":
//   {"op":"state"}              -> full snapshot (grid cells, cursor, title,
//                                  dims, session/active-region/endpoint/status,
//                                  tabs)
//   {"op":"sync_seen","id":"X"} -> {"seen":true|false}
public sealed class TestHook
{
    private readonly Func<string, string> _handle;
    private readonly IPEndPoint _endpoint;

    private TestHook(IPEndPoint ep, Func<string, string> handle)
    {
        _endpoint = ep;
        _handle = handle;
    }

    // FromEnv returns null (no hook) unless NXTERM_TEST_HOOK is set. A bare port
    // binds 0.0.0.0 so a QEMU hostfwd can reach it from the host; host:port is
    // honored as given.
    public static TestHook? FromEnv(Func<string, string> handle)
    {
        var spec = Environment.GetEnvironmentVariable("NXTERM_TEST_HOOK");
        if (string.IsNullOrWhiteSpace(spec)) return null;

        IPAddress addr = IPAddress.Any;
        string portStr = spec;
        int colon = spec.LastIndexOf(':');
        if (colon > 0)
        {
            portStr = spec[(colon + 1)..];
            if (IPAddress.TryParse(spec[..colon], out var parsed) && parsed != null) addr = parsed;
        }
        if (!int.TryParse(portStr, out var port) || port <= 0) return null;
        return new TestHook(new IPEndPoint(addr, port), handle);
    }

    public void Start() => _ = Task.Run(AcceptLoopAsync);

    private async Task AcceptLoopAsync()
    {
        var listener = new TcpListener(_endpoint);
        listener.Start();
        while (true)
        {
            TcpClient client;
            try { client = await listener.AcceptTcpClientAsync(); }
            catch { break; }
            _ = Task.Run(() => ServeAsync(client));
        }
    }

    private async Task ServeAsync(TcpClient client)
    {
        try
        {
            using (client)
            {
                client.NoDelay = true;
                using var stream = client.GetStream();
                using var reader = new StreamReader(stream, new UTF8Encoding(false));
                using var writer = new StreamWriter(stream, new UTF8Encoding(false)) { AutoFlush = true, NewLine = "\n" };
                string? line;
                while ((line = await reader.ReadLineAsync()) != null)
                {
                    if (line.Length == 0) continue;
                    string resp;
                    try { resp = _handle(line); }
                    catch (Exception ex) { resp = "{\"error\":" + JsonString(ex.Message) + "}"; }
                    await writer.WriteLineAsync(resp);
                }
            }
        }
        catch { /* client disconnected */ }
    }

    // JsonString quotes and escapes a string for embedding in a JSON document.
    public static string JsonString(string? s)
    {
        var sb = new StringBuilder(s?.Length + 2 ?? 2);
        sb.Append('"');
        foreach (var ch in s ?? "")
            switch (ch)
            {
                case '"': sb.Append("\\\""); break;
                case '\\': sb.Append("\\\\"); break;
                case '\n': sb.Append("\\n"); break;
                case '\r': sb.Append("\\r"); break;
                case '\t': sb.Append("\\t"); break;
                default:
                    if (ch < 0x20) sb.Append("\\u").Append(((int)ch).ToString("x4"));
                    else sb.Append(ch);
                    break;
            }
        sb.Append('"');
        return sb.ToString();
    }
}

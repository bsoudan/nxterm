using System.Linq;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using NxtermGui.Terminal;

namespace NxtermGui.Protocol;

// Minimal nxterm protocol client for the render spike: newline-delimited JSON
// over a raw TCP transport (transport spec "tcp:host:port"). Connects, attaches
// to the default session, subscribes to its first region, and surfaces screen
// snapshots and terminal-event batches. Events fire on the receive thread.
public sealed class NxtermClient : IDisposable
{
    private TcpClient? _tcp;
    private NetworkStream? _stream;
    private StreamReader? _reader;
    private readonly object _writeLock = new();
    private int _reqId;

    public string? ActiveRegion { get; private set; }

    public event Action<TermCell[][], int, int, string?>? ScreenUpdated;
    public event Action<List<TermEvent>>? EventsReceived;
    public event Action<string>? StatusChanged;

    public async Task ConnectAsync(string host, int port, int cols, int rows)
    {
        StatusChanged?.Invoke($"connecting to {host}:{port}…");
        _tcp = new TcpClient { NoDelay = true };
        await _tcp.ConnectAsync(host, port);
        _stream = _tcp.GetStream();
        _reader = new StreamReader(_stream, new UTF8Encoding(false));

        SendLine("{\"type\":\"identify\",\"hostname\":\"nxterm-gui\",\"username\":\"gui\",\"pid\":0,\"process\":\"nxterm-gui\"}");
        SendLine($"{{\"type\":\"session_connect_request\",\"session\":\"\",\"width\":{cols},\"height\":{rows},\"req_id\":{++_reqId}}}");

        _ = Task.Run(ReceiveLoopAsync);
    }

    private async Task ReceiveLoopAsync()
    {
        try
        {
            string? line;
            while ((line = await _reader!.ReadLineAsync()) != null)
            {
                if (line.Length > 0) Dispatch(line);
            }
            StatusChanged?.Invoke("disconnected");
        }
        catch (Exception ex)
        {
            StatusChanged?.Invoke("disconnected: " + ex.Message);
        }
    }

    private void Dispatch(string line)
    {
        using var doc = JsonDocument.Parse(line);
        var root = doc.RootElement;
        if (!root.TryGetProperty("type", out var typeEl)) return;

        switch (typeEl.GetString())
        {
            case "session_connect_response":
                if (root.TryGetProperty("regions", out var regions) && regions.GetArrayLength() > 0)
                {
                    ActiveRegion = regions[0].GetProperty("region_id").GetString();
                    StatusChanged?.Invoke($"subscribing to {ActiveRegion}");
                    SendLine($"{{\"type\":\"subscribe_request\",\"region_id\":\"{ActiveRegion}\",\"req_id\":{++_reqId}}}");
                }
                break;

            case "screen_update":
                HandleScreenUpdate(root);
                break;

            case "terminal_events":
                EventsReceived?.Invoke(ParseEvents(root));
                break;
        }
    }

    private void HandleScreenUpdate(JsonElement root)
    {
        if (!root.TryGetProperty("cells", out var cellsEl) || cellsEl.ValueKind != JsonValueKind.Array)
            return;
        var cells = ParseCells(cellsEl);
        int cr = root.TryGetProperty("cursor_row", out var crEl) ? crEl.GetInt32() : 0;
        int cc = root.TryGetProperty("cursor_col", out var ccEl) ? ccEl.GetInt32() : 0;
        string? title = root.TryGetProperty("title", out var t) ? t.GetString() : null;
        ScreenUpdated?.Invoke(cells, cr, cc, title);
    }

    private static TermCell[][] ParseCells(JsonElement arr)
    {
        var rows = new TermCell[arr.GetArrayLength()][];
        int ri = 0;
        foreach (var rowEl in arr.EnumerateArray())
        {
            var row = new TermCell[rowEl.GetArrayLength()];
            int ci = 0;
            foreach (var cellEl in rowEl.EnumerateArray())
            {
                string c = cellEl.TryGetProperty("c", out var cv) ? (cv.GetString() ?? " ") : " ";
                if (c.Length == 0) c = " ";
                string? fg = cellEl.TryGetProperty("fg", out var f) ? f.GetString() : null;
                string? bg = cellEl.TryGetProperty("bg", out var b) ? b.GetString() : null;
                byte a = cellEl.TryGetProperty("a", out var av) ? (byte)av.GetInt32() : (byte)0;
                row[ci++] = new TermCell { Text = c, Fg = TermColor.Parse(fg), Bg = TermColor.Parse(bg), Attrs = (CellAttr)a };
            }
            rows[ri++] = row;
        }
        return rows;
    }

    private static List<TermEvent> ParseEvents(JsonElement root)
    {
        var list = new List<TermEvent>();
        if (!root.TryGetProperty("events", out var events)) return list;
        foreach (var ev in events.EnumerateArray())
        {
            var te = new TermEvent { Op = ev.GetProperty("op").GetString() ?? "" };
            if (ev.TryGetProperty("data", out var d)) te.Data = d.GetString();
            if (ev.TryGetProperty("params", out var pr) && pr.ValueKind == JsonValueKind.Array)
                te.Params = pr.EnumerateArray().Select(x => x.GetInt32()).ToArray();
            if (ev.TryGetProperty("how", out var h)) te.How = h.GetInt32();
            if (ev.TryGetProperty("attrs", out var at) && at.ValueKind == JsonValueKind.Array)
                te.Attrs = at.EnumerateArray().Select(x => x.GetInt32()).ToArray();
            if (ev.TryGetProperty("private", out var pv)) te.Private = pv.GetBoolean();
            list.Add(te);
        }
        return list;
    }

    public void SendInput(byte[] data)
    {
        if (ActiveRegion == null || data.Length == 0) return;
        SendLine($"{{\"type\":\"input\",\"region_id\":\"{ActiveRegion}\",\"data\":\"{Convert.ToBase64String(data)}\"}}");
    }

    public void SendResize(int cols, int rows)
    {
        if (ActiveRegion == null) return;
        SendLine($"{{\"type\":\"resize_request\",\"region_id\":\"{ActiveRegion}\",\"width\":{cols},\"height\":{rows},\"req_id\":{++_reqId}}}");
    }

    private void SendLine(string json)
    {
        if (_stream == null) return;
        var bytes = Encoding.UTF8.GetBytes(json + "\n");
        lock (_writeLock)
        {
            _stream.Write(bytes, 0, bytes.Length);
            _stream.Flush();
        }
    }

    public void Dispose()
    {
        try { _reader?.Dispose(); } catch { }
        try { _stream?.Dispose(); } catch { }
        try { _tcp?.Dispose(); } catch { }
    }
}

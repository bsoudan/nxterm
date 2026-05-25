using System.Linq;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using NxtermGui.Terminal;

namespace NxtermGui.Protocol;

// nxterm protocol client: newline-delimited JSON over a raw TCP transport
// (transport spec "tcp:host:port"). Connects, tracks the regions in the default
// session, and surfaces screen snapshots / event batches for whichever region
// is currently active. Events fire on the receive thread.
public sealed class NxtermClient : IDisposable
{
    private TcpClient? _tcp;
    private NetworkStream? _stream;
    private StreamReader? _reader;
    private readonly object _writeLock = new();
    private int _reqId;

    public string Session { get; private set; } = "";
    public string? ActiveRegion { get; private set; }

    // session name, regions (id, name)
    public event Action<string, List<(string Id, string Name)>>? SessionReady;
    public event Action<string, string>? RegionAdded;        // id, name
    public event Action<string>? RegionRemoved;              // id
    public event Action<string, string, bool, string>? RegionSpawned; // id, name, error, message
    public event Action<TermCell[][], int, int, string?>? ScreenUpdated; // active region only
    public event Action<List<TermEvent>>? EventsReceived;    // active region only
    public event Action<string>? StatusChanged;

    public async Task ConnectAsync(string host, int port, int cols, int rows, string session = "")
    {
        StatusChanged?.Invoke($"connecting to {host}:{port}…");
        _tcp = new TcpClient { NoDelay = true };
        await _tcp.ConnectAsync(host, port);
        _stream = _tcp.GetStream();
        _reader = new StreamReader(_stream, new UTF8Encoding(false));

        SendLine("{\"type\":\"identify\",\"hostname\":\"nxterm-gui\",\"username\":\"gui\",\"pid\":0,\"process\":\"nxterm-gui\"}");
        SendLine($"{{\"type\":\"session_connect_request\",\"session\":\"{session}\",\"width\":{cols},\"height\":{rows},\"req_id\":{++_reqId}}}");

        _ = Task.Run(ReceiveLoopAsync);
    }

    // Switch the subscribed region: drop the previous subscription, subscribe the
    // new one (the server replies with a fresh screen_update snapshot).
    public void Activate(string regionId)
    {
        if (regionId == ActiveRegion) return;
        if (ActiveRegion != null)
            SendLine($"{{\"type\":\"unsubscribe_request\",\"region_id\":\"{ActiveRegion}\",\"req_id\":{++_reqId}}}");
        ActiveRegion = regionId;
        SendLine($"{{\"type\":\"subscribe_request\",\"region_id\":\"{regionId}\",\"req_id\":{++_reqId}}}");
    }

    public void Spawn(string program = "")
        => SendLine($"{{\"type\":\"spawn_request\",\"session\":\"{Session}\",\"program\":\"{program}\"}}");

    public void Kill(string regionId)
        => SendLine($"{{\"type\":\"kill_region_request\",\"region_id\":\"{regionId}\"}}");

    private async Task ReceiveLoopAsync()
    {
        try
        {
            string? line;
            while ((line = await _reader!.ReadLineAsync()) != null)
                if (line.Length > 0) Dispatch(line);
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
                Session = root.TryGetProperty("session", out var s) ? (s.GetString() ?? "") : "";
                var regions = new List<(string, string)>();
                if (root.TryGetProperty("regions", out var rs) && rs.ValueKind == JsonValueKind.Array)
                    foreach (var r in rs.EnumerateArray())
                        regions.Add((r.GetProperty("region_id").GetString() ?? "",
                                     r.TryGetProperty("name", out var n) ? (n.GetString() ?? "") : ""));
                StatusChanged?.Invoke("connected");
                SessionReady?.Invoke(Session, regions);
                break;

            // The server announces region add/remove via the tree, not via
            // region_created/region_destroyed (those broadcasts were removed).
            case "tree_events":
                HandleTreeEvents(root);
                break;

            case "spawn_response":
                RegionSpawned?.Invoke(
                    root.TryGetProperty("region_id", out var sid) ? (sid.GetString() ?? "") : "",
                    root.TryGetProperty("name", out var snm) ? (snm.GetString() ?? "") : "",
                    root.TryGetProperty("error", out var se) && se.GetBoolean(),
                    root.TryGetProperty("message", out var sm) ? (sm.GetString() ?? "") : "");
                break;

            case "screen_update":
                if (RegionOf(root) == ActiveRegion) HandleScreenUpdate(root);
                break;

            case "terminal_events":
                if (RegionOf(root) == ActiveRegion) EventsReceived?.Invoke(ParseEvents(root));
                break;
        }
    }

    private static string? RegionOf(JsonElement root)
        => root.TryGetProperty("region_id", out var r) ? r.GetString() : null;

    // Track region add/remove for our session from tree ops:
    //   set /regions/<id> {…name…}   and   add|remove /sessions/<session>/region_ids "<id>"
    private void HandleTreeEvents(JsonElement root)
    {
        if (Session.Length == 0) return;
        if (!root.TryGetProperty("ops", out var ops) || ops.ValueKind != JsonValueKind.Array) return;

        var names = new Dictionary<string, string>();
        foreach (var op in ops.EnumerateArray())
        {
            if ((op.TryGetProperty("op", out var k) ? k.GetString() : null) != "set") continue;
            var path = op.TryGetProperty("path", out var p) ? p.GetString() : null;
            if (path == null || !path.StartsWith("/regions/") || path.IndexOf('/', 9) >= 0) continue;
            if (op.TryGetProperty("value", out var v) && v.ValueKind == JsonValueKind.Object &&
                v.TryGetProperty("name", out var nm))
                names[path[9..]] = nm.GetString() ?? "";
        }

        var prefix = $"/sessions/{Session}/region_ids";
        foreach (var op in ops.EnumerateArray())
        {
            if ((op.TryGetProperty("path", out var p) ? p.GetString() : null) != prefix) continue;
            var kind = op.TryGetProperty("op", out var k) ? k.GetString() : null;
            // add carries the id in "value"; remove carries it in "match".
            if (kind == "add" && op.TryGetProperty("value", out var v) && v.ValueKind == JsonValueKind.String)
                RegionAdded?.Invoke(v.GetString()!, names.TryGetValue(v.GetString()!, out var n) ? n : "shell");
            else if (kind == "remove" && op.TryGetProperty("match", out var m) && m.ValueKind == JsonValueKind.String)
                RegionRemoved?.Invoke(m.GetString()!);
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

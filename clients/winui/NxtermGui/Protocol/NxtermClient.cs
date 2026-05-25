using System.Linq;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using NxtermGui.Terminal;

namespace NxtermGui.Protocol;

// ScreenSnapshot is a screen_update / get_screen snapshot for the active region,
// including the server's scrollback bookkeeping (total/desync/delta) used to
// reconcile local scrollback by absolute seq.
public sealed class ScreenSnapshot
{
    public TermCell[][] Cells = Array.Empty<TermCell[]>();
    public int CursorRow, CursorCol;
    public string? Title;
    public ulong ScrollbackTotal;     // server's monotonic rows-ever-added
    public TermCell[][]? ScrollbackDelta; // rows scrolled into history during a 2026 batch
    public bool ScrollbackDesync;     // local scrollback fell behind; re-fetch
}

// ScrollbackChunk is one get_scrollback_response chunk. Chunks arrive
// newest-first; the snapshot covers seq [ScrollbackTotal - Total, ScrollbackTotal).
public sealed class ScrollbackChunk
{
    public TermCell[][] Lines = Array.Empty<TermCell[]>();
    public int Total;
    public bool Done;
    public ulong ScrollbackTotal;
}

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

    // Connection parameters, retained so the reconnect loop can re-dial and
    // re-run the handshake after the socket drops.
    private string _host = "";
    private int _port;
    private int _cols = 80, _rows = 24;
    private string _sessionArg = "";
    private volatile bool _disposed;

    public string Session { get; private set; } = "";
    public string? ActiveRegion { get; private set; }

    // All session names known from the server tree (snapshot + tree_events),
    // for the session switcher. Guarded by _sessionsLock (touched on the receive
    // thread; read on the UI thread).
    private readonly object _sessionsLock = new();
    private readonly SortedSet<string> _sessions = new(StringComparer.Ordinal);

    public IReadOnlyList<string> Sessions
    {
        get { lock (_sessionsLock) return _sessions.ToList(); }
    }

    public event Action? SessionsChanged;

    // Number of times the connection dropped and entered the reconnect loop.
    // Latched (monotonic) so a test can observe that a reconnect happened even
    // though the transient "reconnecting…" status is too brief to poll reliably.
    public int Reconnects { get; private set; }

    // session name, regions (id, name)
    public event Action<string, List<(string Id, string Name)>>? SessionReady;
    public event Action<string, string>? RegionAdded;        // id, name
    public event Action<string>? RegionRemoved;              // id
    public event Action<string, string, bool, string>? RegionSpawned; // id, name, error, message
    public event Action<ScreenSnapshot>? ScreenUpdated;      // active region only
    public event Action<List<TermEvent>>? EventsReceived;    // active region only
    public event Action<ScrollbackChunk>? ScrollbackReceived; // active region only
    public event Action<string>? StatusChanged;

    public async Task ConnectAsync(string host, int port, int cols, int rows, string session = "")
    {
        _host = host; _port = port; _cols = cols; _rows = rows; _sessionArg = session;
        await OpenAsync(reconnecting: false);
    }

    // OpenAsync dials the server and performs the identify + session_connect
    // handshake, then starts the receive loop. Shared by the initial connect and
    // the reconnect loop, so a reconnect re-runs the exact same handshake. On a
    // reconnect the "reconnecting…" status is left in place through the (local,
    // sub-second) re-dial so observers reliably see it, rather than flipping to
    // a "connecting…" frame that races the handshake.
    private async Task OpenAsync(bool reconnecting)
    {
        if (!reconnecting) StatusChanged?.Invoke($"connecting to {_host}:{_port}…");
        var tcp = new TcpClient { NoDelay = true };
        await tcp.ConnectAsync(_host, _port);
        lock (_writeLock)
        {
            _tcp = tcp;
            _stream = tcp.GetStream();
            _reader = new StreamReader(_stream, new UTF8Encoding(false));
        }
        // Reconnect to the resolved session (set after the first response) so the
        // server rejoins the existing session rather than creating a new one.
        var sess = Session.Length > 0 ? Session : _sessionArg;
        SendLine("{\"type\":\"identify\",\"hostname\":\"nxterm-gui\",\"username\":\"gui\",\"pid\":0,\"process\":\"nxterm-gui\"}");
        SendLine($"{{\"type\":\"session_connect_request\",\"session\":\"{sess}\",\"width\":{_cols},\"height\":{_rows},\"req_id\":{++_reqId}}}");

        _ = Task.Run(ReceiveLoopAsync);
    }

    // Switch the subscribed region: drop the previous subscription, subscribe the
    // new one (the server replies with a fresh screen_update snapshot). force
    // re-subscribes even when regionId is already active — used on reconnect,
    // where the socket changed but ActiveRegion did not.
    public void Activate(string regionId, bool force = false)
    {
        if (!force && regionId == ActiveRegion) return;
        if (ActiveRegion != null && ActiveRegion != regionId)
            SendLine($"{{\"type\":\"unsubscribe_request\",\"region_id\":\"{ActiveRegion}\",\"req_id\":{++_reqId}}}");
        ActiveRegion = regionId;
        SendLine($"{{\"type\":\"subscribe_request\",\"region_id\":\"{regionId}\",\"req_id\":{++_reqId}}}");
    }

    public void Spawn(string program = "")
        => SendLine($"{{\"type\":\"spawn_request\",\"session\":\"{Session}\",\"program\":\"{program}\"}}");

    // SwitchSession re-runs session_connect for a different session on the same
    // connection; the response drives the tab rebuild + re-subscribe upstream.
    public void SwitchSession(string name)
    {
        if (name.Length == 0 || name == Session) return;
        SendLine($"{{\"type\":\"session_connect_request\",\"session\":\"{name}\",\"width\":{_cols},\"height\":{_rows},\"req_id\":{++_reqId}}}");
    }

    public void Kill(string regionId)
        => SendLine($"{{\"type\":\"kill_region_request\",\"region_id\":\"{regionId}\"}}");

    private async Task ReceiveLoopAsync()
    {
        var reader = _reader;
        try
        {
            string? line;
            while (reader != null && (line = await reader.ReadLineAsync()) != null)
                if (line.Length > 0) Dispatch(line);
        }
        catch { /* socket dropped — fall through to reconnect */ }

        if (_disposed) { StatusChanged?.Invoke("disconnected"); return; }
        await ReconnectLoopAsync();
    }

    // ReconnectLoopAsync re-dials with capped backoff after the socket drops,
    // surfacing "reconnecting…" while it retries. On success OpenAsync starts a
    // fresh receive loop (which re-runs the handshake) and this returns; the
    // session_connect_response then drives tab restore + re-subscribe upstream.
    private async Task ReconnectLoopAsync()
    {
        CloseSocket();
        Reconnects++;
        var delayMs = 250;
        while (!_disposed)
        {
            StatusChanged?.Invoke("reconnecting…");
            try { await OpenAsync(reconnecting: true); return; }
            catch
            {
                await Task.Delay(delayMs);
                delayMs = Math.Min(3000, delayMs * 2);
            }
        }
    }

    private void CloseSocket()
    {
        lock (_writeLock)
        {
            try { _reader?.Dispose(); } catch { }
            try { _stream?.Dispose(); } catch { }
            try { _tcp?.Dispose(); } catch { }
            _reader = null; _stream = null; _tcp = null;
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

            // Full tree on connect: seed the known-session set for the switcher.
            case "tree_snapshot":
                HandleTreeSnapshot(root);
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

            case "get_scrollback_response":
                HandleScrollbackResponse(root);
                break;
        }
    }

    private static string? RegionOf(JsonElement root)
        => root.TryGetProperty("region_id", out var r) ? r.GetString() : null;

    // Track session create/delete from tree ops (set|delete /sessions/<name>,
    // not the /region_ids sub-path) so the switcher's list stays current.
    private void TrackSessions(JsonElement ops)
    {
        bool changed = false;
        lock (_sessionsLock)
        {
            foreach (var op in ops.EnumerateArray())
            {
                var path = op.TryGetProperty("path", out var p) ? p.GetString() : null;
                if (path == null || !path.StartsWith("/sessions/")) continue;
                var rest = path["/sessions/".Length..];
                if (rest.Length == 0 || rest.Contains('/')) continue; // skip /region_ids etc.
                var kind = op.TryGetProperty("op", out var k) ? k.GetString() : null;
                if (kind == "set") changed |= _sessions.Add(rest);
                else if (kind is "delete" or "remove") changed |= _sessions.Remove(rest);
            }
        }
        if (changed) SessionsChanged?.Invoke();
    }

    // Seed the known-session set from a full tree snapshot (sent on connect).
    private void HandleTreeSnapshot(JsonElement root)
    {
        if (!root.TryGetProperty("tree", out var tree) ||
            !tree.TryGetProperty("sessions", out var sessions) ||
            sessions.ValueKind != JsonValueKind.Object)
            return;
        lock (_sessionsLock)
        {
            _sessions.Clear();
            foreach (var s in sessions.EnumerateObject()) _sessions.Add(s.Name);
        }
        SessionsChanged?.Invoke();
    }

    // Track region add/remove for our session from tree ops:
    //   set /regions/<id> {…name…}   and   add|remove /sessions/<session>/region_ids "<id>"
    private void HandleTreeEvents(JsonElement root)
    {
        if (!root.TryGetProperty("ops", out var ops) || ops.ValueKind != JsonValueKind.Array) return;
        TrackSessions(ops);
        if (Session.Length == 0) return;

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
        var snap = new ScreenSnapshot
        {
            Cells = ParseCells(cellsEl),
            CursorRow = root.TryGetProperty("cursor_row", out var crEl) ? crEl.GetInt32() : 0,
            CursorCol = root.TryGetProperty("cursor_col", out var ccEl) ? ccEl.GetInt32() : 0,
            Title = root.TryGetProperty("title", out var t) ? t.GetString() : null,
            ScrollbackTotal = root.TryGetProperty("scrollback_total", out var st) ? st.GetUInt64() : 0,
            ScrollbackDesync = root.TryGetProperty("scrollback_desync", out var sd) && sd.GetBoolean(),
        };
        if (root.TryGetProperty("scrollback_delta", out var deltaEl) && deltaEl.ValueKind == JsonValueKind.Array)
            snap.ScrollbackDelta = ParseCells(deltaEl);
        ScreenUpdated?.Invoke(snap);
    }

    private void HandleScrollbackResponse(JsonElement root)
    {
        if (RegionOf(root) != ActiveRegion) return;
        var chunk = new ScrollbackChunk
        {
            Lines = root.TryGetProperty("lines", out var l) && l.ValueKind == JsonValueKind.Array
                ? ParseCells(l) : Array.Empty<TermCell[]>(),
            Total = root.TryGetProperty("total", out var tot) ? tot.GetInt32() : 0,
            Done = root.TryGetProperty("done", out var d) && d.GetBoolean(),
            ScrollbackTotal = root.TryGetProperty("scrollback_total", out var st) ? st.GetUInt64() : 0,
        };
        ScrollbackReceived?.Invoke(chunk);
    }

    // RequestScrollback asks the server for the active region's full scrollback;
    // the response streams back as one or more get_scrollback_response chunks.
    public void RequestScrollback()
    {
        if (ActiveRegion == null) return;
        SendLine($"{{\"type\":\"get_scrollback_request\",\"region_id\":\"{ActiveRegion}\"}}");
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
        _disposed = true;
        CloseSocket();
    }
}

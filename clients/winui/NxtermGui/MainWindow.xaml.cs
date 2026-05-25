using System.Collections.ObjectModel;
using System.Linq;
using System.Text.Json;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Text;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using NxtermGui.Input;
using NxtermGui.Protocol;
using NxtermGui.Terminal;
using NxtermGui.Ui;
using Windows.ApplicationModel.DataTransfer;
using Windows.System;
using Windows.UI;
using Windows.UI.Core;

namespace NxtermGui;

public sealed partial class MainWindow : Window
{
    private readonly NxtermClient _client = new();
    private readonly object _gridLock = new();
    private readonly ObservableCollection<TabItem> _tabs = new();
    private TerminalGrid _grid = new(80, 24);

    private CanvasTextFormat _font = null!, _fontBold = null!, _fontItalic = null!, _fontBoldItalic = null!;
    private double _cellW = 8, _cellH = 16;
    private bool _ready;
    private int _lastCols = 80, _lastRows = 24;
    private string _status = "starting…";
    private string _title = "";
    private string _endpoint = "";

    private (int row, int col) _selAnchor, _selCaret;
    private bool _selecting, _hasSelection;
    private bool _mouseDown;
    private (int row, int col) _lastMouseCell = (-1, -1);

    // Test introspection (NXTERM_TEST_HOOK). _syncSeen records sync-marker ids
    // the grid has processed, so the e2e harness can wait deterministically.
    private TestHook? _testHook;
    private readonly object _syncLock = new();
    private readonly HashSet<string> _syncSeen = new();

    public MainWindow()
    {
        this.InitializeComponent();
        TabStrip.ItemsSource = _tabs;

        // Windows-Terminal-style: draw the tab strip into the caption area and
        // make the strip the draggable title bar. Interactive children (tabs,
        // + button) automatically pass input through.
        ExtendsContentIntoTitleBar = true;
        SetTitleBar(AppTitleBar);
        var tb = AppWindow.TitleBar;
        tb.ButtonBackgroundColor = Microsoft.UI.Colors.Transparent;
        tb.ButtonInactiveBackgroundColor = Microsoft.UI.Colors.Transparent;
        tb.ButtonForegroundColor = Color.FromArgb(255, 0xC8, 0xC8, 0xC8);
        tb.ButtonInactiveForegroundColor = Color.FromArgb(255, 0x8A, 0x8A, 0x8A);
        tb.ButtonHoverBackgroundColor = Color.FromArgb(255, 0x3A, 0x3A, 0x3A);
        tb.ButtonHoverForegroundColor = Microsoft.UI.Colors.White;
        tb.ButtonPressedBackgroundColor = Color.FromArgb(255, 0x2A, 0x2A, 0x2A);
        tb.ButtonPressedForegroundColor = Microsoft.UI.Colors.White;

        _client.SessionReady += OnSessionReady;
        _client.RegionAdded += OnRegionAdded;
        _client.RegionRemoved += OnRegionRemoved;
        _client.RegionSpawned += OnRegionSpawned;
        _client.ScreenUpdated += OnScreenUpdated;
        _client.EventsReceived += OnEventsReceived;
        _client.StatusChanged += OnStatusChanged;
    }

    private async void TerminalCanvas_Loaded(object sender, RoutedEventArgs e)
    {
        MeasureFont();
        _ready = true;

        TerminalCanvas.AddHandler(UIElement.KeyDownEvent,
            new Microsoft.UI.Xaml.Input.KeyEventHandler(TerminalCanvas_KeyDown), handledEventsToo: true);

        (_lastCols, _lastRows) = SizeToGrid(TerminalCanvas.ActualWidth, TerminalCanvas.ActualHeight);
        lock (_gridLock) _grid = new TerminalGrid(_lastCols, _lastRows);

        TerminalCanvas.Focus(FocusState.Programmatic);
        SetForegroundWindow(WinRT.Interop.WindowNative.GetWindowHandle(this));

        // endpoint and (optional) session come from the command line, falling
        // back to env vars then defaults: NxtermGui.exe <host:port> [session]
        var argv = Environment.GetCommandLineArgs();
        _endpoint = argv.Length > 1 ? argv[1] : (Environment.GetEnvironmentVariable("NXTERM_ENDPOINT") ?? "127.0.0.1:7654");
        var session = argv.Length > 2 ? argv[2] : (Environment.GetEnvironmentVariable("NXTERM_SESSION") ?? "");
        _testHook = TestHook.FromEnv(HandleTestRequest);
        _testHook?.Start();

        var (host, port) = ParseEndpoint(_endpoint);
        try { await _client.ConnectAsync(host, port, _lastCols, _lastRows, session); }
        catch (Exception ex) { OnStatusChanged("connect failed: " + ex.Message); }
    }

    private void MeasureFont()
    {
        _font = MakeFont(false, false);
        _fontBold = MakeFont(true, false);
        _fontItalic = MakeFont(false, true);
        _fontBoldItalic = MakeFont(true, true);
        var dev = CanvasDevice.GetSharedDevice();
        using var probe = new CanvasTextLayout(dev, new string('M', 10), _font, 10000, 1000);
        _cellW = probe.LayoutBounds.Width / 10.0;
        _cellH = probe.LayoutBounds.Height;
    }

    private static CanvasTextFormat MakeFont(bool bold, bool italic) => new()
    {
        FontFamily = "Consolas",
        FontSize = 16,
        FontWeight = new Windows.UI.Text.FontWeight { Weight = (ushort)(bold ? 700 : 400) },
        FontStyle = italic ? Windows.UI.Text.FontStyle.Italic : Windows.UI.Text.FontStyle.Normal,
        WordWrapping = CanvasWordWrapping.NoWrap,
    };

    private CanvasTextFormat Font(CellAttr a)
    {
        bool b = (a & CellAttr.Bold) != 0, i = (a & CellAttr.Italic) != 0;
        return b ? (i ? _fontBoldItalic : _fontBold) : (i ? _fontItalic : _font);
    }

    private (int cols, int rows) SizeToGrid(double w, double h) =>
        (w > 0 ? Math.Max(1, (int)(w / _cellW)) : 80, h > 0 ? Math.Max(1, (int)(h / _cellH)) : 24);

    private static (string host, int port) ParseEndpoint(string spec)
    {
        int i = spec.LastIndexOf(':');
        if (i <= 0) return (spec, 7654);
        return (spec[..i], int.TryParse(spec[(i + 1)..], out var p) ? p : 7654);
    }

    // --- tab / region management (marshalled to the UI thread) --------------

    private void OnSessionReady(string session, List<(string Id, string Name)> regions) => OnUi(() =>
    {
        _tabs.Clear();
        foreach (var (id, name) in regions) _tabs.Add(new TabItem(id, name));
        if (_tabs.Count > 0) ActivateRegion(_tabs[0].RegionId);
        UiRefresh();
    });

    private void OnRegionAdded(string id, string name) => OnUi(() =>
    {
        if (_tabs.All(t => t.RegionId != id)) _tabs.Add(new TabItem(id, name));
    });

    private void OnRegionRemoved(string id) => OnUi(() =>
    {
        var tab = _tabs.FirstOrDefault(t => t.RegionId == id);
        if (tab == null) return;
        bool wasActive = id == _client.ActiveRegion;
        int idx = _tabs.IndexOf(tab);
        _tabs.Remove(tab);
        if (wasActive && _tabs.Count > 0)
            ActivateRegion(_tabs[Math.Min(idx, _tabs.Count - 1)].RegionId);
        UiRefresh();
    });

    private void OnRegionSpawned(string id, string name, bool error, string message) => OnUi(() =>
    {
        if (error) { _status = "spawn failed: " + message; UiRefresh(); return; }
        if (_tabs.All(t => t.RegionId != id)) _tabs.Add(new TabItem(id, name));
        ActivateRegion(id);
    });

    private void ActivateRegion(string id)
    {
        _client.Activate(id);
        lock (_gridLock) _grid = new TerminalGrid(_lastCols, _lastRows);
        _client.SendResize(_lastCols, _lastRows);
        foreach (var t in _tabs) t.IsActive = t.RegionId == id;
        _title = "";
        UiRefresh();
    }

    private void Tab_Click(object sender, RoutedEventArgs e)
    {
        if ((sender as FrameworkElement)?.DataContext is TabItem t) ActivateRegion(t.RegionId);
    }

    private void TabClose_Click(object sender, RoutedEventArgs e)
    {
        if ((sender as FrameworkElement)?.DataContext is TabItem t) _client.Kill(t.RegionId);
    }

    private void NewTab_Click(object sender, RoutedEventArgs e) => _client.Spawn();

    // --- server callbacks (receive thread) ---------------------------------

    private void OnScreenUpdated(TermCell[][] cells, int cr, int cc, string? title)
    {
        if (title is { Length: > 0 }) _title = title.Trim();
        lock (_gridLock) _grid.ApplySnapshot(cells, cr, cc, title);
        OnUi(UiRefresh);
    }

    private void OnEventsReceived(List<TermEvent> events)
    {
        lock (_gridLock)
        {
            _grid.Apply(events);
            if (_grid.Title.Length > 0) _title = _grid.Title.Trim();
        }
        // Record sync markers only after the grid has fully applied the batch,
        // so "sync id seen" implies the grid reflects everything before it —
        // the same render-complete guarantee PtyIO gives via OSC acks.
        foreach (var ev in events)
            if (ev.Op == "sync" && !string.IsNullOrEmpty(ev.Data))
                lock (_syncLock) _syncSeen.Add(ev.Data!);
        OnUi(UiRefresh);
    }

    private void OnStatusChanged(string status)
    {
        _status = status;
        OnUi(UiRefresh);
    }

    private void OnUi(Action a) => DispatcherQueue.TryEnqueue(() => a());

    private void UiRefresh()
    {
        this.Title = _title.Length > 0 ? $"nxterm — {_title}" : $"nxterm ({_status})";
        var active = _tabs.FirstOrDefault(t => t.RegionId == _client.ActiveRegion);
        if (active != null && _title.Length > 0) active.Title = _title;
        StatusLeft.Text = string.IsNullOrEmpty(_client.Session) ? _endpoint : $"{_client.Session}@{_endpoint}";
        ActiveRegionId.Text = _client.ActiveRegion ?? "";
        StatusRight.Text = $"{_lastCols}×{_lastRows}   {_status}";
        TerminalCanvas.Invalidate();
    }

    // --- rendering ----------------------------------------------------------

    private void TerminalCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        var ds = args.DrawingSession;
        float cw = (float)_cellW, ch = (float)_cellH;
        lock (_gridLock)
        {
            var g = _grid;
            int style = g.CursorStyle;            // 0/1/2 block, 3/4 underline, 5/6 bar
            bool blockShape = style <= 2;
            bool hasSel = _hasSelection;
            (int r, int c) selS = _selAnchor, selE = _selCaret;
            if (hasSel && !LessEq(selS, selE)) (selS, selE) = (selE, selS);
            for (int r = 0; r < g.Rows; r++)
            {
                for (int c = 0; c < g.Cols; c++)
                {
                    var cell = g[r, c];
                    var attrs = cell.Attrs;
                    uint fg = Palette.Resolve(cell.Fg, Palette.DefaultForeground);
                    uint bg = Palette.Resolve(cell.Bg, Palette.DefaultBackground);
                    if ((attrs & CellAttr.Reverse) != 0) (fg, bg) = (bg, fg);
                    if ((attrs & CellAttr.Faint) != 0) fg = Dim(fg);

                    bool isCursor = g.CursorVisible && r == g.CursorRow && c == g.CursorCol;
                    bool blockCursor = isCursor && blockShape;
                    if (blockCursor) (fg, bg) = (bg, fg);
                    if (hasSel && !blockCursor && r >= selS.r && r <= selE.r
                        && (r > selS.r || c >= selS.c) && (r < selE.r || c <= selE.c))
                        bg = 0x264F78;   // selection highlight

                    float x = c * cw, y = r * ch;
                    if (bg != Palette.DefaultBackground || blockCursor)
                        ds.FillRectangle(x, y, cw + 1, ch, Rgb(bg));

                    var fgc = Rgb(fg);
                    if ((attrs & CellAttr.Conceal) == 0 && !string.IsNullOrWhiteSpace(cell.Text))
                        ds.DrawText(cell.Text, x, y, fgc, Font(attrs));
                    if ((attrs & CellAttr.Underline) != 0)
                        ds.DrawLine(x, y + ch - 1.5f, x + cw, y + ch - 1.5f, fgc, 1.2f);
                    if ((attrs & CellAttr.Strikethrough) != 0)
                        ds.DrawLine(x, y + ch / 2, x + cw, y + ch / 2, fgc, 1.2f);

                    // underline / bar cursor shapes (block handled by the fill above)
                    if (isCursor && !blockShape)
                    {
                        var cursorColor = Rgb(Palette.DefaultForeground);
                        if (style is 3 or 4) ds.FillRectangle(x, y + ch - 2.5f, cw, 2.5f, cursorColor);
                        else ds.FillRectangle(x, y, 2.5f, ch, cursorColor);
                    }
                }
            }
        }
    }

    private static Color Rgb(uint v) => Color.FromArgb(255, (byte)(v >> 16), (byte)(v >> 8), (byte)v);
    private static uint Dim(uint v)
    {
        byte r = (byte)((byte)(v >> 16) * 2 / 3), g = (byte)((byte)(v >> 8) * 2 / 3), b = (byte)((byte)v * 2 / 3);
        return (uint)((r << 16) | (g << 8) | b);
    }

    // --- input --------------------------------------------------------------

    private void TerminalCanvas_KeyDown(object sender, Microsoft.UI.Xaml.Input.KeyRoutedEventArgs e)
    {
        bool ctrl = IsDown(VirtualKey.Control), shift = IsDown(VirtualKey.Shift), alt = IsDown(VirtualKey.Menu);
        if (ctrl && shift && e.Key == VirtualKey.C) { CopySelection(); e.Handled = true; return; }
        if (ctrl && shift && e.Key == VirtualKey.V) { _ = PasteAsync(); e.Handled = true; return; }

        var bytes = KeyEncoder.Encode(e.Key, ctrl, shift, alt);
        if (bytes != null)
        {
            if (_hasSelection) { _hasSelection = false; TerminalCanvas.Invalidate(); }
            _client.SendInput(bytes);
            e.Handled = true;
        }
    }

    private static bool IsDown(VirtualKey k) =>
        (InputKeyboardSource.GetKeyStateForCurrentThread(k) & CoreVirtualKeyStates.Down) == CoreVirtualKeyStates.Down;

    private void TerminalCanvas_PointerPressed(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        TerminalCanvas.Focus(FocusState.Pointer);
        SetForegroundWindow(WinRT.Interop.WindowNative.GetWindowHandle(this));
        var (row, col) = CellAt(e.GetCurrentPoint(TerminalCanvas).Position);
        TerminalCanvas.CapturePointer(e.Pointer);

        if (_grid.MouseReportsPress)        // forward to the app instead of selecting
        {
            _mouseDown = true;
            _lastMouseCell = (row, col);
            SendMouse(0, col, row, press: true);
            return;
        }
        _selAnchor = _selCaret = (row, col);
        _selecting = true;
        _hasSelection = false;
        TerminalCanvas.Invalidate();
    }

    private void TerminalCanvas_PointerMoved(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        var (row, col) = CellAt(e.GetCurrentPoint(TerminalCanvas).Position);
        if (_grid.MouseReportsPress)
        {
            bool report = _grid.MouseReportsAny || (_grid.MouseReportsDrag && _mouseDown);
            if (report && (row, col) != _lastMouseCell)
            {
                _lastMouseCell = (row, col);
                SendMouse((_mouseDown ? 0 : 3) + 32, col, row, press: true);  // +32 = motion
            }
            return;
        }
        if (!_selecting) return;
        _selCaret = (row, col);
        _hasSelection = _selCaret != _selAnchor;
        TerminalCanvas.Invalidate();
    }

    private void TerminalCanvas_PointerReleased(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        var (row, col) = CellAt(e.GetCurrentPoint(TerminalCanvas).Position);
        if (_grid.MouseReportsPress)
        {
            if (_mouseDown) SendMouse(0, col, row, press: false);
            _mouseDown = false;
            TerminalCanvas.ReleasePointerCapture(e.Pointer);
            return;
        }
        if (_selecting)
        {
            // Finalize from the release point too, in case PointerMoved didn't
            // fire (e.g. a synthetic pointer during automated testing).
            _selCaret = (row, col);
            _hasSelection = _selCaret != _selAnchor;
            TerminalCanvas.Invalidate();
        }
        _selecting = false;
        TerminalCanvas.ReleasePointerCapture(e.Pointer);
    }

    private void TerminalCanvas_PointerWheelChanged(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
    {
        if (!_grid.MouseReportsPress) return;   // (scrollback handled in a later phase)
        var pp = e.GetCurrentPoint(TerminalCanvas);
        var (row, col) = CellAt(pp.Position);
        SendMouse(pp.Properties.MouseWheelDelta > 0 ? 64 : 65, col, row, press: true);
        e.Handled = true;
    }

    // Encode an xterm mouse event (SGR 1006 if enabled, else legacy X10) and send it.
    private void SendMouse(int button, int col, int row, bool press)
    {
        int cb = button;
        if (IsDown(VirtualKey.Shift)) cb += 4;
        if (IsDown(VirtualKey.Menu)) cb += 8;
        if (IsDown(VirtualKey.Control)) cb += 16;
        int x = col + 1, y = row + 1;

        byte[] seq;
        if (_grid.MouseSgr)
            seq = System.Text.Encoding.ASCII.GetBytes($"\x1b[<{cb};{x};{y}{(press ? 'M' : 'm')}");
        else
        {
            int b = press ? cb : 3;   // legacy release reports button 3
            seq = new byte[] { 0x1b, (byte)'[', (byte)'M', (byte)(b + 32), (byte)(x + 32), (byte)(y + 32) };
        }
        if (LastInput != null) LastInput.Text = Escape(seq);
        _client.SendInput(seq);
    }

    private static string Escape(byte[] b)
    {
        var sb = new System.Text.StringBuilder();
        foreach (var x in b) sb.Append(x == 0x1b ? "\\e" : ((char)x).ToString());
        return sb.ToString();
    }

    private (int row, int col) CellAt(Windows.Foundation.Point p) =>
        (Math.Clamp((int)(p.Y / _cellH), 0, Math.Max(0, _lastRows - 1)),
         Math.Clamp((int)(p.X / _cellW), 0, Math.Max(0, _lastCols - 1)));

    private static bool LessEq((int r, int c) a, (int r, int c) b) => a.r < b.r || (a.r == b.r && a.c <= b.c);

    private void CopySelection()
    {
        string text;
        lock (_gridLock)
        {
            if (!_hasSelection) return;
            var g = _grid;
            (int r, int c) s = _selAnchor, e = _selCaret;
            if (!LessEq(s, e)) (s, e) = (e, s);
            var sb = new System.Text.StringBuilder();
            for (int r = s.r; r <= e.r && r < g.Rows; r++)
            {
                int c0 = r == s.r ? s.c : 0;
                int c1 = r == e.r ? e.c : g.Cols - 1;
                var line = new System.Text.StringBuilder();
                for (int c = c0; c <= c1 && c < g.Cols; c++)
                {
                    var t = g[r, c].Text;
                    line.Append(string.IsNullOrEmpty(t) ? " " : t);
                }
                sb.Append(line.ToString().TrimEnd());
                if (r < e.r) sb.Append('\n');
            }
            text = sb.ToString();
        }
        if (text.Length == 0) return;
        var dp = new DataPackage();
        dp.SetText(text);
        Clipboard.SetContent(dp);
    }

    private async System.Threading.Tasks.Task PasteAsync()
    {
        var content = Clipboard.GetContent();
        if (!content.Contains(StandardDataFormats.Text)) return;
        string text;
        try { text = await content.GetTextAsync(); } catch { return; }
        if (string.IsNullOrEmpty(text)) return;

        var body = System.Text.Encoding.UTF8.GetBytes(text);
        if (_grid.BracketedPaste)
        {
            var pre = System.Text.Encoding.ASCII.GetBytes("\x1b[200~");
            var post = System.Text.Encoding.ASCII.GetBytes("\x1b[201~");
            var wrapped = new byte[pre.Length + body.Length + post.Length];
            Buffer.BlockCopy(pre, 0, wrapped, 0, pre.Length);
            Buffer.BlockCopy(body, 0, wrapped, pre.Length, body.Length);
            Buffer.BlockCopy(post, 0, wrapped, pre.Length + body.Length, post.Length);
            body = wrapped;
        }
        _client.SendInput(body);
    }

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hWnd);

    // --- resize -------------------------------------------------------------

    private void TerminalCanvas_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        if (!_ready) return;
        var (cols, rows) = SizeToGrid(e.NewSize.Width, e.NewSize.Height);
        if (cols == _lastCols && rows == _lastRows) return;
        _lastCols = cols; _lastRows = rows;
        lock (_gridLock) _grid.Resize(cols, rows);
        _client.SendResize(cols, rows);
        UiRefresh();
    }

    // --- test introspection (NXTERM_TEST_HOOK) ------------------------------

    private string HandleTestRequest(string line)
    {
        using var doc = JsonDocument.Parse(line);
        var op = doc.RootElement.TryGetProperty("op", out var o) ? o.GetString() : null;
        switch (op)
        {
            case "state":
                return OnUiSync(BuildStateJson);
            case "sync_seen":
                var id = doc.RootElement.TryGetProperty("id", out var idEl) ? idEl.GetString() : null;
                bool seen;
                lock (_syncLock) seen = id != null && _syncSeen.Contains(id);
                return "{\"seen\":" + (seen ? "true" : "false") + "}";
            default:
                return "{\"error\":\"unknown op\"}";
        }
    }

    // Build a full state snapshot. Runs on the UI thread (via OnUiSync) so tab /
    // status reads are race-free; the grid read takes _gridLock as usual.
    private string BuildStateJson()
    {
        object dto;
        lock (_gridLock)
        {
            var g = _grid;
            var rows = new object[g.Rows];
            for (int r = 0; r < g.Rows; r++)
            {
                var cells = new object[g.Cols];
                for (int c = 0; c < g.Cols; c++) cells[c] = CellDto(g[r, c]);
                rows[r] = cells;
            }
            dto = new
            {
                cols = g.Cols,
                rows_count = g.Rows,
                cursor_row = g.CursorRow,
                cursor_col = g.CursorCol,
                cursor_visible = g.CursorVisible,
                title = g.Title,
                rows,
                session = _client.Session,
                active_region = _client.ActiveRegion ?? "",
                endpoint = _endpoint,
                status = _status,
                tabs = _tabs.Select(t => new { id = t.RegionId, title = t.Title, active = t.IsActive }).ToArray(),
            };
        }
        return JsonSerializer.Serialize(dto);
    }

    private static object CellDto(TermCell cell) =>
        new { c = cell.Text, fg = ColorSpec(cell.Fg), bg = ColorSpec(cell.Bg), a = (int)cell.Attrs };

    // Reverse of TermColor.Parse: emit the server's wire spec. Indexed colors
    // are always "5;N"; the Go side decides ANSI16 vs ANSI256 by N < 16.
    private static string ColorSpec(TermColor c) => c.Kind switch
    {
        ColorKind.Indexed => "5;" + c.Index,
        ColorKind.Rgb => "2;" + c.Rgb.ToString("x6"),
        _ => "",
    };

    private T OnUiSync<T>(Func<T> f)
    {
        if (DispatcherQueue.HasThreadAccess) return f();
        var tcs = new TaskCompletionSource<T>();
        if (!DispatcherQueue.TryEnqueue(() =>
            {
                try { tcs.SetResult(f()); }
                catch (Exception e) { tcs.SetException(e); }
            }))
            throw new InvalidOperationException("UI dispatcher unavailable");
        return tcs.Task.GetAwaiter().GetResult();
    }
}

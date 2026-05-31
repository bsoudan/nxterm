using System.Text.Json;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Text;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Nx2Gui.Protocol;
using Nx2Gui.Wasm;
using Windows.System;
using Windows.UI;
using Windows.UI.Core;

namespace Nx2Gui;

// The nx2 WinUI host. It connects to the broker, runs the client-side WASM guest
// (the terminal app by default), paints the guest's cell-grid frames with Win2D,
// and forwards keystrokes. The guest does all VT work; this host never parses
// terminal protocol — it relays opaque Data frames and decodes cell-grid frames.
public sealed partial class MainWindow : Window
{
    private readonly object _guestLock = new();   // serializes all guest calls
    private readonly object _frameLock = new();    // protects _frame
    private GuestInstance? _guest;
    private BrokerConnection? _conn;
    private GridFrame? _frame;

    private CanvasTextFormat _font = null!, _fontBold = null!, _fontItalic = null!, _fontBoldItalic = null!;
    private double _cellW = 8, _cellH = 16;
    private bool _ready;
    private int _cols = 80, _rows = 24;
    private string _status = "starting…";
    private string _app = "term", _session = "", _endpoint = "";
    private TestHook? _testHook;
    private readonly CancellationTokenSource _cts = new();

    private static void Trace(string msg)
    {
        try { System.IO.File.AppendAllText(@"C:\nx2gui-startup.log", $"{System.DateTime.Now:HH:mm:ss.fff} MW {msg}\r\n"); } catch { }
    }

    public MainWindow()
    {
        Trace("ctor: begin");
        this.InitializeComponent();
        Title = "nx2";
        Trace("ctor: done");
    }

    private void TerminalCanvas_Loaded(object sender, RoutedEventArgs e)
    {
        Trace("Canvas_Loaded: begin");
        MeasureFont();
        _ready = true;
        (_cols, _rows) = SizeToGrid(TerminalCanvas.ActualWidth, TerminalCanvas.ActualHeight);

        TerminalCanvas.Focus(FocusState.Programmatic);
        SetForegroundWindow(WinRT.Interop.WindowNative.GetWindowHandle(this));

        var argv = Environment.GetCommandLineArgs();
        _endpoint = argv.Length > 1 && argv[1].Length > 0
            ? argv[1]
            : (Environment.GetEnvironmentVariable("NX2_ENDPOINT") ?? "10.0.2.2:7777");
        _app = argv.Length > 2 ? argv[2] : (Environment.GetEnvironmentVariable("NX2_APP") ?? "term");
        _session = argv.Length > 3 ? argv[3] : (Environment.GetEnvironmentVariable("NX2_SESSION") ?? "");

        _testHook = TestHook.FromEnv(HandleTestRequest);
        _testHook?.Start();

        Trace($"Canvas_Loaded: connecting to {_endpoint} app={_app}");
        _ = ConnectAsync();
    }

    private async Task ConnectAsync()
    {
        try
        {
            var (host, port) = ParseEndpoint(_endpoint);
            var cacheDir = Path.Combine(Path.GetTempPath(), "nx2-cache");
            _conn = new BrokerConnection(new Capsule(cacheDir));
            _conn.StatusChanged += s => OnUi(() => { _status = s; RefreshStatus(); });
            _conn.DataReceived += OnData;

            Trace($"ConnectAsync: dialing {host}:{port}");
            await _conn.ConnectAsync(host, port, _cts.Token);
            Trace("ConnectAsync: connected; resolving");
            string hash = await _conn.ResolveAsync(_app, _cts.Token);
            Trace($"ConnectAsync: resolved {hash}; fetching");
            byte[] wasm = await _conn.FetchAsync(hash, _cts.Token);
            Trace($"ConnectAsync: fetched {wasm.Length} bytes; instantiating guest");

            var guest = new GuestInstance(wasm)
            {
                OnFrame = OnFrame,
                OnChannelSend = bytes => { _ = _conn!.SendDataAsync(bytes, _cts.Token); },
            };
            Trace("ConnectAsync: guest instantiated; configuring");
            lock (_guestLock)
            {
                guest.Configure(_cols, _rows);
                _guest = guest;
            }
            await _conn.SelectAppAsync(_app, _session, _cts.Token);
            Trace("ConnectAsync: select_app sent; entering read loop");
            OnUi(() => { _status = $"running {_app}"; RefreshStatus(); });
            _ = _conn.RunReadLoopAsync(_cts.Token);
        }
        catch (Exception ex)
        {
            Trace("ConnectAsync EXCEPTION: " + ex);
            OnUi(() => { _status = "error: " + ex.Message; RefreshStatus(); });
        }
    }

    // Broker Data frame -> guest. feed+render run under the guest lock; OnFrame
    // fires synchronously inside render (already holding the lock).
    private void OnData(byte[] data)
    {
        lock (_guestLock)
        {
            if (_guest == null) return;
            _guest.Feed(data);
            _guest.Render();
        }
    }

    // Guest produced a frame (called inside a guest call, under _guestLock).
    private void OnFrame(GridFrame f)
    {
        lock (_frameLock) _frame = f;
        OnUi(() => TerminalCanvas.Invalidate());
    }

    // --- rendering ----------------------------------------------------------

    private void TerminalCanvas_Draw(CanvasControl sender, CanvasDrawEventArgs args)
    {
        GridFrame? f;
        lock (_frameLock) f = _frame;
        if (f == null) return;

        var ds = args.DrawingSession;
        float cw = (float)_cellW, ch = (float)_cellH;
        for (int r = 0; r < f.Rows; r++)
        {
            for (int c = 0; c < f.Cols; c++)
            {
                var cell = f.At(r, c);
                uint fg = Palette.Resolve(cell.Fg, Palette.DefaultForeground);
                uint bg = Palette.Resolve(cell.Bg, Palette.DefaultBackground);
                if ((cell.Attrs & CellAttr.Reverse) != 0) (fg, bg) = (bg, fg);
                if ((cell.Attrs & CellAttr.Faint) != 0) fg = Dim(fg);

                bool isCursor = !f.CursorHidden && r == f.CursorRow && c == f.CursorCol;
                if (isCursor) (fg, bg) = (bg, fg);

                float x = c * cw, y = r * ch;
                if (bg != Palette.DefaultBackground || isCursor)
                    ds.FillRectangle(x, y, cw + 1, ch, Rgb(bg));

                if ((cell.Attrs & CellAttr.Conceal) == 0 && !string.IsNullOrWhiteSpace(cell.Data))
                    ds.DrawText(cell.Data, x, y, Rgb(fg), Font(cell.Attrs));
                if ((cell.Attrs & CellAttr.Underline) != 0)
                    ds.DrawLine(x, y + ch - 1.5f, x + cw, y + ch - 1.5f, Rgb(fg), 1.2f);
                if ((cell.Attrs & CellAttr.Strikethrough) != 0)
                    ds.DrawLine(x, y + ch / 2, x + cw, y + ch / 2, Rgb(fg), 1.2f);
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
        var bytes = KeyEncoder.Encode(e.Key, ctrl, shift, alt);
        if (bytes == null) return;
        lock (_guestLock) _guest?.Input(bytes);
        e.Handled = true;
    }

    private static bool IsDown(VirtualKey k) =>
        (InputKeyboardSource.GetKeyStateForCurrentThread(k) & CoreVirtualKeyStates.Down) == CoreVirtualKeyStates.Down;

    // --- resize -------------------------------------------------------------

    private void TerminalCanvas_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        if (!_ready) return;
        var (cols, rows) = SizeToGrid(e.NewSize.Width, e.NewSize.Height);
        if (cols == _cols && rows == _rows) return;
        _cols = cols; _rows = rows;
        lock (_guestLock) { _guest?.Resize(cols, rows); _guest?.Render(); }
        RefreshStatus();
    }

    // --- helpers ------------------------------------------------------------

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
        if (i <= 0) return (spec, 7777);
        return (spec[..i], int.TryParse(spec[(i + 1)..], out var p) ? p : 7777);
    }

    private void OnUi(Action a) => DispatcherQueue.TryEnqueue(() => a());

    private void RefreshStatus() => StatusBar.Text = $"{_endpoint}  {_app}  {_cols}×{_rows}  {_status}";

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hWnd);

    // --- test hook (NX2_TEST_HOOK) ------------------------------------------

    private string HandleTestRequest(string line)
    {
        using var doc = JsonDocument.Parse(line);
        var op = doc.RootElement.TryGetProperty("op", out var o) ? o.GetString() : null;
        return op switch
        {
            "state" => BuildStateJson(),
            _ => "{\"error\":\"unknown op\"}",
        };
    }

    private string BuildStateJson()
    {
        GridFrame? f;
        lock (_frameLock) f = _frame;
        object dto;
        if (f == null)
        {
            dto = new { ready = false, status = _status };
        }
        else
        {
            var rows = new object[f.Rows];
            for (int r = 0; r < f.Rows; r++)
            {
                var cells = new object[f.Cols];
                for (int c = 0; c < f.Cols; c++)
                {
                    var cell = f.At(r, c);
                    cells[c] = new { c = cell.Data, a = (int)cell.Attrs };
                }
                rows[r] = cells;
            }
            dto = new
            {
                ready = true,
                status = _status,
                cols = f.Cols,
                rows_count = f.Rows,
                cursor_row = f.CursorRow,
                cursor_col = f.CursorCol,
                cursor_hidden = f.CursorHidden,
                rows,
            };
        }
        return JsonSerializer.Serialize(dto);
    }
}

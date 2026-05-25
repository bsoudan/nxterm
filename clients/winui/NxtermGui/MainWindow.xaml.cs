using System.Collections.ObjectModel;
using System.Linq;
using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Text;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using NxtermGui.Input;
using NxtermGui.Protocol;
using NxtermGui.Terminal;
using NxtermGui.Ui;
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
        var bytes = KeyEncoder.Encode(e.Key, IsDown(VirtualKey.Control), IsDown(VirtualKey.Shift), IsDown(VirtualKey.Menu));
        if (bytes != null) { _client.SendInput(bytes); e.Handled = true; }
    }

    private static bool IsDown(VirtualKey k) =>
        (InputKeyboardSource.GetKeyStateForCurrentThread(k) & CoreVirtualKeyStates.Down) == CoreVirtualKeyStates.Down;

    private void TerminalCanvas_PointerPressed(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
        => TerminalCanvas.Focus(FocusState.Pointer);

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
}

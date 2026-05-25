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

    private CanvasTextFormat _font = null!;
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
        _font = new CanvasTextFormat { FontFamily = "Consolas", FontSize = 16, WordWrapping = CanvasWordWrapping.NoWrap };
        var dev = CanvasDevice.GetSharedDevice();
        using var probe = new CanvasTextLayout(dev, new string('M', 10), _font, 10000, 1000);
        _cellW = probe.LayoutBounds.Width / 10.0;
        _cellH = probe.LayoutBounds.Height;
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

    private void Tab_Tapped(object sender, Microsoft.UI.Xaml.Input.TappedRoutedEventArgs e)
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
        lock (_gridLock)
        {
            var g = _grid;
            for (int r = 0; r < g.Rows; r++)
            {
                for (int c = 0; c < g.Cols; c++)
                {
                    var cell = g[r, c];
                    uint fg = Palette.Resolve(cell.Fg, Palette.DefaultForeground);
                    uint bg = Palette.Resolve(cell.Bg, Palette.DefaultBackground);
                    bool reverse = (cell.Attrs & CellAttr.Reverse) != 0;
                    bool cursor = g.CursorVisible && r == g.CursorRow && c == g.CursorCol;
                    if (reverse ^ cursor) (fg, bg) = (bg, fg);
                    if ((cell.Attrs & CellAttr.Faint) != 0) fg = Dim(fg);

                    float x = (float)(c * _cellW), y = (float)(r * _cellH);
                    if (bg != Palette.DefaultBackground)
                        ds.FillRectangle(x, y, (float)_cellW + 1, (float)_cellH, Rgb(bg));
                    if (!string.IsNullOrWhiteSpace(cell.Text))
                        ds.DrawText(cell.Text, x, y, Rgb(fg), _font);
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

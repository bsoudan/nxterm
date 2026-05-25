using Microsoft.Graphics.Canvas;
using Microsoft.Graphics.Canvas.Text;
using Microsoft.Graphics.Canvas.UI.Xaml;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using NxtermGui.Input;
using NxtermGui.Protocol;
using NxtermGui.Terminal;
using Windows.System;
using Windows.UI;
using Windows.UI.Core;

namespace NxtermGui;

public sealed partial class MainWindow : Window
{
    private readonly NxtermClient _client = new();
    private readonly object _gridLock = new();
    private TerminalGrid _grid = new(80, 24);

    private CanvasTextFormat _font = null!;
    private double _cellW = 8, _cellH = 16;
    private bool _ready;
    private int _lastCols, _lastRows;
    private string _status = "starting…";
    private string _title = "";

    public MainWindow()
    {
        this.InitializeComponent();
        _client.ScreenUpdated += OnScreenUpdated;
        _client.EventsReceived += OnEventsReceived;
        _client.StatusChanged += OnStatusChanged;
    }

    private async void TerminalCanvas_Loaded(object sender, RoutedEventArgs e)
    {
        MeasureFont();
        _ready = true;

        var (cols, rows) = SizeToGrid(TerminalCanvas.ActualWidth, TerminalCanvas.ActualHeight);
        _lastCols = cols; _lastRows = rows;
        lock (_gridLock) _grid = new TerminalGrid(cols, rows);

        // CanvasControl can mark KeyDown handled before a normal XAML handler
        // runs, so register with handledEventsToo.
        TerminalCanvas.AddHandler(UIElement.KeyDownEvent,
            new Microsoft.UI.Xaml.Input.KeyEventHandler(TerminalCanvas_KeyDown), handledEventsToo: true);

        TerminalCanvas.Focus(FocusState.Programmatic);

        // Pull the window to the foreground so it owns keyboard focus on launch.
        SetForegroundWindow(WinRT.Interop.WindowNative.GetWindowHandle(this));

        var (host, port) = ParseEndpoint(Environment.GetEnvironmentVariable("NXTERM_ENDPOINT") ?? "127.0.0.1:7654");
        try { await _client.ConnectAsync(host, port, cols, rows); }
        catch (Exception ex) { OnStatusChanged("connect failed: " + ex.Message); }
    }

    private void MeasureFont()
    {
        _font = new CanvasTextFormat
        {
            FontFamily = "Consolas",
            FontSize = 16,
            WordWrapping = CanvasWordWrapping.NoWrap,
        };
        var dev = CanvasDevice.GetSharedDevice();
        using var probe = new CanvasTextLayout(dev, new string('M', 10), _font, 10000, 1000);
        _cellW = probe.LayoutBounds.Width / 10.0;
        _cellH = probe.LayoutBounds.Height;
    }

    private (int cols, int rows) SizeToGrid(double w, double h)
    {
        int cols = w > 0 ? Math.Max(1, (int)(w / _cellW)) : 80;
        int rows = h > 0 ? Math.Max(1, (int)(h / _cellH)) : 24;
        return (cols, rows);
    }

    private static (string host, int port) ParseEndpoint(string spec)
    {
        int i = spec.LastIndexOf(':');
        if (i <= 0) return (spec, 7654);
        return (spec[..i], int.TryParse(spec[(i + 1)..], out var p) ? p : 7654);
    }

    // --- server callbacks (receive thread) ---------------------------------

    private void OnScreenUpdated(TermCell[][] cells, int cr, int cc, string? title)
    {
        if (title is { Length: > 0 }) _title = title.Trim();
        lock (_gridLock) _grid.ApplySnapshot(cells, cr, cc, title);
        Redraw();
    }

    private void OnEventsReceived(List<TermEvent> events)
    {
        lock (_gridLock)
        {
            _grid.Apply(events);
            if (_grid.Title.Length > 0) _title = _grid.Title.Trim();
        }
        Redraw();
    }

    private void OnStatusChanged(string status)
    {
        _status = status;
        Redraw();
    }

    private void Redraw()
    {
        DispatcherQueue.TryEnqueue(() =>
        {
            this.Title = _title.Length > 0 ? $"nxterm — {_title}" : $"nxterm ({_status})";
            TerminalCanvas.Invalidate();
        });
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
        if (bytes != null)
        {
            _client.SendInput(bytes);
            e.Handled = true;
        }
    }

    private static bool IsDown(VirtualKey k) =>
        (InputKeyboardSource.GetKeyStateForCurrentThread(k) & CoreVirtualKeyStates.Down) == CoreVirtualKeyStates.Down;

    [System.Runtime.InteropServices.DllImport("user32.dll")]
    private static extern bool SetForegroundWindow(IntPtr hWnd);

    private void TerminalCanvas_PointerPressed(object sender, Microsoft.UI.Xaml.Input.PointerRoutedEventArgs e)
        => TerminalCanvas.Focus(FocusState.Pointer);

    // --- resize -------------------------------------------------------------

    private void TerminalCanvas_SizeChanged(object sender, SizeChangedEventArgs e)
    {
        if (!_ready) return;
        var (cols, rows) = SizeToGrid(e.NewSize.Width, e.NewSize.Height);
        if (cols == _lastCols && rows == _lastRows) return;
        _lastCols = cols; _lastRows = rows;
        lock (_gridLock) _grid.Resize(cols, rows);
        _client.SendResize(cols, rows);
        TerminalCanvas.Invalidate();
    }
}

using System.ComponentModel;
using Microsoft.UI.Xaml.Media;
using Windows.UI;

namespace NxtermGui.Ui;

// View-model for one tab in the Windows-Terminal-style strip. One tab == one
// server region. Bindings update live as the title or active state change.
public sealed class TabItem : INotifyPropertyChanged
{
    public string RegionId { get; }

    private string _title;
    private bool _active;

    public TabItem(string regionId, string title)
    {
        RegionId = regionId;
        _title = string.IsNullOrEmpty(title) ? "shell" : title;
    }

    public string Title
    {
        get => _title;
        set { if (_title != value) { _title = value; Raise(nameof(Title)); } }
    }

    public bool IsActive
    {
        get => _active;
        set { if (_active != value) { _active = value; Raise(nameof(IsActive)); Raise(nameof(Background)); Raise(nameof(Foreground)); } }
    }

    // Active tab blends into the terminal background; inactive tabs are dim.
    public Brush Background => new SolidColorBrush(_active
        ? Color.FromArgb(255, 0x0C, 0x0C, 0x0C)
        : Color.FromArgb(255, 0x2B, 0x2B, 0x2B));

    public Brush Foreground => new SolidColorBrush(_active
        ? Color.FromArgb(255, 0xF2, 0xF2, 0xF2)
        : Color.FromArgb(255, 0x9A, 0x9A, 0x9A));

    public event PropertyChangedEventHandler? PropertyChanged;
    private void Raise(string n) => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(n));
}

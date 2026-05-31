using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Input;
using Windows.System;

namespace HelloApp;

public sealed partial class MainWindow : Window
{
    public MainWindow()
    {
        this.InitializeComponent();
        this.Title = "Hello App";
    }

    private void GreetButton_Click(object sender, RoutedEventArgs e) => Greet();

    private void NameBox_KeyDown(object sender, KeyRoutedEventArgs e)
    {
        if (e.Key == VirtualKey.Enter)
        {
            Greet();
            e.Handled = true;
        }
    }

    private void Greet()
    {
        var name = NameBox.Text.Trim();
        GreetingText.Text = string.IsNullOrEmpty(name) ? string.Empty : $"hello, {name}";
    }
}

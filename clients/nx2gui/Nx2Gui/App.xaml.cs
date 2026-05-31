using Microsoft.UI.Xaml;

namespace Nx2Gui;

public partial class App : Application
{
    private Window? _window;

    // Startup breadcrumb log to a fixed, session-independent path so a headless
    // VM run can see exactly how far launch got (and any exception). Debug aid;
    // harmless in normal use.
    private static void Trace(string msg)
    {
        try
        {
            System.IO.File.AppendAllText(@"C:\nx2gui-startup.log",
                $"{System.DateTime.Now:HH:mm:ss.fff} {msg}\r\n");
        }
        catch { }
    }

    public App()
    {
        Trace("App ctor: begin");
        this.UnhandledException += (s, e) => Trace("UnhandledException: " + e.Exception);
        AppDomain.CurrentDomain.UnhandledException += (s, e) => Trace("AppDomain unhandled: " + e.ExceptionObject);
        this.InitializeComponent();
        Trace("App ctor: InitializeComponent done");
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        Trace("OnLaunched: begin");
        try
        {
            _window = new MainWindow();
            Trace("OnLaunched: MainWindow created");
            _window.Activate();
            Trace("OnLaunched: Activate done");
        }
        catch (System.Exception ex)
        {
            Trace("OnLaunched EXCEPTION: " + ex);
            throw;
        }
    }
}

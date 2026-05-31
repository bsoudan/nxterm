using System.IO;
using System.Net.Sockets;
using System.Text;
using System.Threading;

namespace Nx2Gui;

// Newline-delimited JSON control socket for headless e2e tests. When
// NX2_TEST_HOOK=host:port is set, the host dials it and answers one request per
// line via the supplied handler, letting Go e2e tests introspect the GUI's live
// state without screen-scraping. Same shape as the old client's TestHook.
public sealed class TestHook
{
    private readonly string _host;
    private readonly int _port;
    private readonly Func<string, string> _handler;
    private Thread? _thread;

    private TestHook(string host, int port, Func<string, string> handler)
    {
        _host = host; _port = port; _handler = handler;
    }

    public static TestHook? FromEnv(Func<string, string> handler)
    {
        var spec = Environment.GetEnvironmentVariable("NX2_TEST_HOOK");
        if (string.IsNullOrWhiteSpace(spec)) return null;
        int i = spec.LastIndexOf(':');
        if (i <= 0) return null;
        var host = spec[..i];
        if (!int.TryParse(spec[(i + 1)..], out var port)) return null;
        return new TestHook(host, port, handler);
    }

    public void Start()
    {
        _thread = new Thread(Run) { IsBackground = true, Name = "nx2-test-hook" };
        _thread.Start();
    }

    private void Run()
    {
        try
        {
            using var client = new TcpClient();
            client.Connect(_host, _port);
            using var stream = client.GetStream();
            using var reader = new StreamReader(stream, Encoding.UTF8);
            using var writer = new StreamWriter(stream, new UTF8Encoding(false)) { AutoFlush = true };
            string? line;
            while ((line = reader.ReadLine()) != null)
            {
                string resp;
                try { resp = _handler(line); }
                catch (Exception e) { resp = "{\"error\":\"" + e.Message.Replace("\"", "'") + "\"}"; }
                writer.WriteLine(resp);
            }
        }
        catch { /* test-only; never crash the app */ }
    }
}

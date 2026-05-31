using Nx2Gui.Protocol;
using Wasmtime;

namespace Nx2Gui.Wasm;

// GuestInstance runs an nx2 client-side WASM app under wasmtime-dotnet and
// bridges the core-wasm (ptr,len) ABI (mirrors Go nx2/internal/wasmhost). The
// guest is a wasip1 c-shared reactor: it exports alloc/configure/feed/render/
// resize/input and imports nx2.submit_cells / nx2.channel_send.
//
// A wasm instance is single-threaded — callers must serialize entry (the host
// does so by only driving it from the UI thread / one read pump).
public sealed class GuestInstance : IDisposable
{
    private readonly Engine _engine;
    private readonly Store _store;
    private readonly Instance _instance;
    private readonly Memory _memory;
    private readonly Function _alloc, _configure, _feed, _render, _resize;
    private readonly Function? _input;

    // Called (synchronously, inside Feed/Render/Input) with a decoded frame.
    public Action<GridFrame>? OnFrame;
    // Called (synchronously, inside Input) with opaque bytes for the companion.
    public Action<byte[]>? OnChannelSend;

    public GuestInstance(byte[] wasm)
    {
        _engine = new Engine();
        var module = Module.FromBytes(_engine, "guest", wasm);
        var linker = new Linker(_engine);
        _store = new Store(_engine);

        linker.DefineWasi();
        _store.SetWasiConfiguration(new WasiConfiguration()
            .WithInheritedStandardOutput().WithInheritedStandardError());

        linker.Define("nx2", "submit_cells", Function.FromCallback(_store,
            (Caller caller, int ptr, int len) =>
            {
                if (len <= 0 || OnFrame == null) return;
                var mem = caller.GetMemory("memory");
                if (mem is null) return;
                var bytes = mem.GetSpan(ptr, len).ToArray();
                var f = CellGrid.Decode(bytes);
                if (f != null) OnFrame(f);
            }));

        linker.Define("nx2", "channel_send", Function.FromCallback(_store,
            (Caller caller, int ptr, int len) =>
            {
                if (len <= 0 || OnChannelSend == null) return;
                var mem = caller.GetMemory("memory");
                if (mem is null) return;
                OnChannelSend(mem.GetSpan(ptr, len).ToArray());
            }));

        _instance = linker.Instantiate(_store, module);
        _instance.GetFunction("_initialize")?.Invoke(); // reactor init

        _memory = _instance.GetMemory("memory") ?? throw new InvalidOperationException("guest has no memory export");
        _alloc = Req("alloc");
        _configure = Req("configure");
        _feed = Req("feed");
        _render = Req("render");
        _resize = Req("resize");
        _input = _instance.GetFunction("input");
    }

    private Function Req(string name) =>
        _instance.GetFunction(name) ?? throw new InvalidOperationException($"guest missing export {name}");

    public void Configure(int cols, int rows) => _configure.Invoke(cols, rows);
    public void Resize(int cols, int rows) => _resize.Invoke(cols, rows);
    public void Render() => _render.Invoke();

    // Feed delivers opaque companion bytes (a Data frame) to the guest.
    public void Feed(byte[] data)
    {
        if (data.Length == 0) return;
        int ptr = (int)(_alloc.Invoke(data.Length) ?? throw new InvalidOperationException("alloc returned null"));
        data.CopyTo(_memory.GetSpan(ptr, data.Length));
        _feed.Invoke(ptr, data.Length);
    }

    // Input hands user-input bytes to the guest, which wraps + relays them via
    // OnChannelSend. No-op if the guest doesn't export input.
    public void Input(byte[] data)
    {
        if (data.Length == 0 || _input == null) return;
        int ptr = (int)(_alloc.Invoke(data.Length) ?? throw new InvalidOperationException("alloc returned null"));
        data.CopyTo(_memory.GetSpan(ptr, data.Length));
        _input.Invoke(ptr, data.Length);
    }

    public void Dispose()
    {
        _store.Dispose();
        _engine.Dispose();
    }
}

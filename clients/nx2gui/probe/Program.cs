// nx2 WASM-host linchpin probe (C# / wasmtime-dotnet).
//
// Loads the nx2 terminal guest (Go pkg/te compiled to a wasip1 c-shared reactor),
// supplies the "nx2" host imports (submit_cells / channel_send) plus WASI, calls
// the reactor init + ABI (configure/alloc/feed/render), and checks that one
// batched cell-grid frame comes back decodably with the expected glyphs/colour.
//
// If this prints OK, the WinUI nx2 host's runtime path is proven and the rest is
// UI plumbing. Run in the Windows VM via clients/nx2gui/probe/run.sh.

using System;
using System.Buffers.Binary;
using System.IO;
using System.Text;
using Wasmtime;

string wasmPath = args.Length > 0 ? args[0] : "terminal-guest.wasm";
if (!File.Exists(wasmPath)) { Console.Error.WriteLine($"FAIL: wasm not found: {wasmPath}"); return 2; }

using var engine = new Engine();
using var module = Module.FromFile(engine, wasmPath);
using var linker = new Linker(engine);
using var store = new Store(engine);

linker.DefineWasi();
store.SetWasiConfiguration(new WasiConfiguration()
    .WithInheritedStandardOutput()
    .WithInheritedStandardError());

byte[]? lastFrame = null;

// Host import nx2.submit_cells(ptr,len): the guest hands us one encoded frame.
linker.Define("nx2", "submit_cells", Function.FromCallback(store,
    (Caller caller, int ptr, int len) =>
    {
        if (len <= 0) return;
        var mem = caller.GetMemory("memory");
        if (mem is null) return;
        lastFrame = mem.GetSpan(ptr, len).ToArray();
    }));

// Host import nx2.channel_send(ptr,len): guest -> companion bytes (unused here).
linker.Define("nx2", "channel_send", Function.FromCallback(store,
    (Caller caller, int ptr, int len) => { }));

var instance = linker.Instantiate(store, module);

// Reactor init: Go c-shared exposes "_initialize", not "_start".
instance.GetFunction("_initialize")?.Invoke();

var configure = instance.GetFunction("configure") ?? throw new Exception("no configure export");
var alloc = instance.GetFunction("alloc") ?? throw new Exception("no alloc export");
var feed = instance.GetFunction("feed") ?? throw new Exception("no feed export");
var render = instance.GetFunction("render") ?? throw new Exception("no render export");
var memory = instance.GetMemory("memory") ?? throw new Exception("no memory export");

const int cols = 20, rows = 3;
configure.Invoke(cols, rows);

// Build a proto.Raw frame: [kind:1=0][u32 len][payload]; payload = SGR green "hi".
byte[] payload = Encoding.ASCII.GetBytes("\x1b[32mhi");
byte[] frame = new byte[1 + 4 + payload.Length];
frame[0] = 0; // proto.Raw
BinaryPrimitives.WriteUInt32LittleEndian(frame.AsSpan(1, 4), (uint)payload.Length);
payload.CopyTo(frame.AsSpan(5));

int p = (int)(alloc.Invoke(frame.Length) ?? throw new Exception("alloc returned null"));
frame.CopyTo(memory.GetSpan(p, frame.Length));
feed.Invoke(p, frame.Length);
render.Invoke();

if (lastFrame is null) { Console.Error.WriteLine("FAIL: submit_cells never fired"); return 1; }

// Decode the cell-grid frame (see nx2/internal/cellgrid): "NX2F" magic, u16 ver,
// cols,rows,curRow,curCol,flags, then cols*rows cells.
var f = lastFrame;
int off = 0;
uint magic = BinaryPrimitives.ReadUInt32LittleEndian(f.AsSpan(off, 4)); off += 4;
if (magic != 0x4E583246) { Console.Error.WriteLine($"FAIL: bad magic 0x{magic:X}"); return 1; }
ushort ver = BinaryPrimitives.ReadUInt16LittleEndian(f.AsSpan(off, 2)); off += 2;
int fc = BinaryPrimitives.ReadUInt16LittleEndian(f.AsSpan(off, 2)); off += 2;
int fr = BinaryPrimitives.ReadUInt16LittleEndian(f.AsSpan(off, 2)); off += 2;
off += 2 + 2 + 2; // curRow, curCol, flags

string firstTwo = "";
int fgMode0 = -1, fgIdx0 = -1;
for (int i = 0; i < fc * fr; i++)
{
    int dlen = BinaryPrimitives.ReadUInt16LittleEndian(f.AsSpan(off, 2)); off += 2;
    string data = dlen > 0 ? Encoding.UTF8.GetString(f, off, dlen) : ""; off += dlen;
    int fgM = f[off]; int fgI = f[off + 1]; off += 4; // fg: mode,idx/r,g,b
    off += 4;                                          // bg
    off += 2;                                          // attrs
    if (i == 0) { fgMode0 = fgM; fgIdx0 = fgI; }
    if (i < 2) firstTwo += data.Length > 0 ? data : " ";
}

Console.WriteLine($"frame: {fc}x{fr} ver={ver}, row0head=\"{firstTwo}\", fg0=mode{fgMode0}/idx{fgIdx0}");
bool ok = fc == cols && fr == rows && firstTwo.StartsWith("hi") && fgMode0 == 1 && fgIdx0 == 2;
Console.WriteLine(ok ? "OK: wasmtime-dotnet ran the nx2 guest and rendered a frame" : "FAIL: frame contents unexpected");
return ok ? 0 : 1;

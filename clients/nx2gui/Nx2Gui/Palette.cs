using Nx2Gui.Protocol;

namespace Nx2Gui;

// Maps cell-grid colors (default / ANSI-16 / 256 / truecolor) to packed 0xRRGGBB.
// Same palette the Go TUI uses, so renders match across hosts.
public static class Palette
{
    private static readonly uint[] Ansi16 =
    {
        0x000000, 0x800000, 0x008000, 0x808000, 0x000080, 0x800080, 0x008080, 0xc0c0c0,
        0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
    };

    public const uint DefaultForeground = 0xC8C8C8;
    public const uint DefaultBackground = 0x000000;

    public static uint Resolve(CellColor c, uint fallback) => c.Mode switch
    {
        ColorMode.Ansi16 => c.Index < 16 ? Ansi16[c.Index] : Xterm256(c.Index),
        ColorMode.Ansi256 => Xterm256(c.Index),
        ColorMode.TrueColor => (uint)((c.R << 16) | (c.G << 8) | c.B),
        _ => fallback,
    };

    public static uint Xterm256(int idx)
    {
        if (idx < 16) return Ansi16[idx];
        if (idx >= 232) { int v = 8 + (idx - 232) * 10; return (uint)((v << 16) | (v << 8) | v); }
        idx -= 16;
        int r = idx / 36, g = (idx / 6) % 6, b = idx % 6;
        static int s(int x) => x == 0 ? 0 : 55 + x * 40;
        return (uint)((s(r) << 16) | (s(g) << 8) | s(b));
    }
}

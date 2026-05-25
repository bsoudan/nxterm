namespace NxtermGui.Terminal;

// Resolves terminal colors to 0xRRGGBB. The 16 base colors use the Campbell
// scheme (Windows Terminal's default) so output looks at home on Windows.
public static class Palette
{
    public const uint DefaultForeground = 0xCCCCCC;
    public const uint DefaultBackground = 0x0C0C0C;

    private static readonly uint[] Table = Build();

    public static uint Resolve(TermColor c, uint fallback) => c.Kind switch
    {
        ColorKind.Rgb => c.Rgb,
        ColorKind.Indexed => Table[c.Index],
        _ => fallback,
    };

    private static uint[] Build()
    {
        var t = new uint[256];
        uint[] basic =
        {
            0x0C0C0C, 0xC50F1F, 0x13A10E, 0xC19C00, 0x0037DA, 0x881798, 0x3A96DD, 0xCCCCCC,
            0x767676, 0xE74856, 0x16C60C, 0xF9F1A5, 0x3B78FF, 0xB4009E, 0x61D6D6, 0xF2F2F2,
        };
        Array.Copy(basic, t, 16);

        // 16-231: 6x6x6 color cube.
        int[] steps = { 0, 95, 135, 175, 215, 255 };
        int i = 16;
        for (int r = 0; r < 6; r++)
            for (int g = 0; g < 6; g++)
                for (int b = 0; b < 6; b++)
                    t[i++] = (uint)((steps[r] << 16) | (steps[g] << 8) | steps[b]);

        // 232-255: 24-step grayscale ramp.
        for (int s = 0; s < 24; s++)
        {
            int v = 8 + s * 10;
            t[232 + s] = (uint)((v << 16) | (v << 8) | v);
        }
        return t;
    }
}

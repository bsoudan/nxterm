namespace NxtermGui.Terminal;

// Mirrors the wire encoding of a cell's color (internal/protocol/color.go):
//   ""          -> default
//   "red"       -> ANSI-16 by name
//   "5;208"     -> 256-color palette index
//   "2;ff8700"  -> 24-bit RGB
public enum ColorKind : byte { Default, Indexed, Rgb }

public readonly struct TermColor
{
    public ColorKind Kind { get; }
    public byte Index { get; }   // ANSI-16 / 256 palette index
    public uint Rgb { get; }     // 0xRRGGBB

    private TermColor(ColorKind kind, byte index, uint rgb) { Kind = kind; Index = index; Rgb = rgb; }

    public static readonly TermColor Default = new(ColorKind.Default, 0, 0);
    public static TermColor Indexed16(byte i) => new(ColorKind.Indexed, i, 0);
    public static TermColor FromRgb(uint rgb) => new(ColorKind.Rgb, 0, rgb);

    // ANSI-16 names as encoded by the server (color.go ansiNames).
    private static readonly Dictionary<string, byte> NameIndex = new()
    {
        ["black"] = 0, ["red"] = 1, ["green"] = 2, ["brown"] = 3,
        ["blue"] = 4, ["magenta"] = 5, ["cyan"] = 6, ["white"] = 7,
        ["brightblack"] = 8, ["brightred"] = 9, ["brightgreen"] = 10, ["brightbrown"] = 11,
        ["brightblue"] = 12, ["brightmagenta"] = 13, ["brightcyan"] = 14, ["brightwhite"] = 15,
    };

    public static TermColor Parse(string? spec)
    {
        if (string.IsNullOrEmpty(spec)) return Default;
        if (NameIndex.TryGetValue(spec, out var idx)) return Indexed16(idx);

        var semi = spec.IndexOf(';');
        if (semi > 0)
        {
            var tag = spec[..semi];
            var rest = spec[(semi + 1)..];
            if (tag == "5" && byte.TryParse(rest, out var palIdx))
                return new TermColor(ColorKind.Indexed, palIdx, 0);
            if (tag == "2" && uint.TryParse(rest, System.Globalization.NumberStyles.HexNumber, null, out var rgb))
                return FromRgb(rgb);
        }
        return Default;
    }
}

// Attribute bitfield matching ScreenCell.A (internal/protocol/protocol.go).
[Flags]
public enum CellAttr : byte
{
    None = 0, Bold = 1, Italic = 2, Underline = 4, Strikethrough = 8,
    Reverse = 16, Blink = 32, Conceal = 64, Faint = 128,
}

public struct TermCell
{
    public string Text;   // grapheme cluster; " " for blank, "" for wide-char continuation
    public TermColor Fg;
    public TermColor Bg;
    public CellAttr Attrs;

    public static TermCell Blank(TermColor bg = default) =>
        new() { Text = " ", Fg = TermColor.Default, Bg = bg, Attrs = CellAttr.None };
}

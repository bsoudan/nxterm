using System.Buffers.Binary;
using System.Text;

namespace Nx2Gui.Protocol;

// Decoder for the batched cell-grid frame the guest hands the host via
// submit_cells. Mirrors Go nx2/internal/cellgrid (little-endian).
public enum ColorMode : byte { Default = 0, Ansi16 = 1, Ansi256 = 2, TrueColor = 3 }

[Flags]
public enum CellAttr : ushort
{
    None = 0,
    Bold = 1 << 0, Faint = 1 << 1, Italic = 1 << 2, Underline = 1 << 3,
    Strikethrough = 1 << 4, Reverse = 1 << 5, Blink = 1 << 6, Conceal = 1 << 7,
    Protected = 1 << 8,
}

public readonly struct CellColor
{
    public readonly ColorMode Mode;
    public readonly byte Index, R, G, B;
    public CellColor(ColorMode m, byte index, byte r, byte g, byte b) { Mode = m; Index = index; R = r; G = g; B = b; }
}

public readonly struct GridCell
{
    public readonly string Data;
    public readonly CellColor Fg, Bg;
    public readonly CellAttr Attrs;
    public GridCell(string data, CellColor fg, CellColor bg, CellAttr attrs) { Data = data; Fg = fg; Bg = bg; Attrs = attrs; }
}

public sealed class GridFrame
{
    public int Cols, Rows, CursorRow, CursorCol;
    public bool CursorHidden;
    public GridCell[] Cells = Array.Empty<GridCell>();
    public GridCell At(int r, int c) => Cells[r * Cols + c];
}

public static class CellGrid
{
    private const uint Magic = 0x4E583246; // "NX2F"
    private const ushort FlagCursorHidden = 1 << 0;

    public static GridFrame? Decode(byte[] b)
    {
        int off = 0;
        if (b.Length < 14) return null;
        if (BinaryPrimitives.ReadUInt32LittleEndian(b.AsSpan(off)) != Magic) return null; off += 4;
        ushort ver = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
        if (ver != 0) return null;
        var f = new GridFrame
        {
            Cols = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)),
        };
        off += 2;
        f.Rows = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
        f.CursorRow = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
        f.CursorCol = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
        ushort flags = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
        f.CursorHidden = (flags & FlagCursorHidden) != 0;

        int n = f.Cols * f.Rows;
        if (n < 0) return null;
        f.Cells = new GridCell[n];
        for (int i = 0; i < n; i++)
        {
            if (off + 2 > b.Length) return null;
            int dlen = BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
            if (off + dlen + 10 > b.Length) return null;
            string data = dlen > 0 ? Encoding.UTF8.GetString(b, off, dlen) : ""; off += dlen;
            var fg = ReadColor(b, ref off);
            var bg = ReadColor(b, ref off);
            var attrs = (CellAttr)BinaryPrimitives.ReadUInt16LittleEndian(b.AsSpan(off)); off += 2;
            f.Cells[i] = new GridCell(data, fg, bg, attrs);
        }
        return f;
    }

    private static CellColor ReadColor(byte[] b, ref int off)
    {
        var mode = (ColorMode)b[off];
        byte a = b[off + 1], c = b[off + 2], d = b[off + 3];
        off += 4;
        return mode == ColorMode.TrueColor
            ? new CellColor(mode, 0, a, c, d)
            : new CellColor(mode, a, 0, 0, 0);
    }
}

namespace NxtermGui.Terminal;

// One terminal event already parsed by the server (no escape-sequence parsing
// needed here). Mirrors internal/protocol TerminalEvent.
public sealed class TermEvent
{
    public string Op = "";
    public string? Data;
    public int[]? Params;
    public int How;
    public int[]? Attrs;
    public bool Private;
}

// The client-side cell grid. Holds the visible screen and replays terminal
// events into it (the same model the Go TUI keeps via pkg/te). Not thread-safe;
// the owner serializes access.
public sealed class TerminalGrid
{
    public int Cols { get; private set; }
    public int Rows { get; private set; }
    public int CursorRow { get; private set; }
    public int CursorCol { get; private set; }
    public bool CursorVisible { get; private set; } = true;
    public int CursorStyle { get; private set; }   // DECSCUSR: 0/1/2 block, 3/4 underline, 5/6 bar
    public bool BracketedPaste { get; private set; } // DECSET 2004
    public string Title { get; private set; } = "";

    private TermCell[][] _buf = Array.Empty<TermCell[]>();
    private int _top, _bottom;                 // scroll region (inclusive)
    private TermColor _penFg = TermColor.Default, _penBg = TermColor.Default;
    private CellAttr _penAttrs = CellAttr.None;
    private string _lastChar = " ";

    // Saved cursor (DECSC/DECRC).
    private int _savRow, _savCol;
    private TermColor _savFg = TermColor.Default, _savBg = TermColor.Default;
    private CellAttr _savAttrs;

    // Alternate screen (DECSET 1049/1047/47): the primary buffer + cursor are
    // parked here while a full-screen app draws, and restored on exit.
    private bool _altActive;
    private TermCell[][]? _altBuf;
    private int _altRow, _altCol, _altTop, _altBottom;
    private TermColor _altFg = TermColor.Default, _altBg = TermColor.Default;
    private CellAttr _altAttrs;

    public TerminalGrid(int cols, int rows) => Allocate(cols, rows);

    public TermCell this[int r, int c] => _buf[r][c];

    private void Allocate(int cols, int rows)
    {
        Cols = Math.Max(1, cols);
        Rows = Math.Max(1, rows);
        _buf = new TermCell[Rows][];
        for (int r = 0; r < Rows; r++) _buf[r] = NewRow(TermColor.Default);
        _top = 0; _bottom = Rows - 1;
        CursorRow = CursorCol = 0;
    }

    private TermCell[] NewRow(TermColor bg)
    {
        var row = new TermCell[Cols];
        for (int c = 0; c < Cols; c++) row[c] = TermCell.Blank(bg);
        return row;
    }

    public void Resize(int cols, int rows)
    {
        if (cols == Cols && rows == Rows) return;
        var old = _buf; var oldAlt = _altBuf; int oldRows = Rows, oldCols = Cols;
        int curR = CursorRow, curC = CursorCol;
        Allocate(cols, rows);
        CopyOverlap(_buf, old, oldRows, oldCols);
        if (oldAlt != null)
        {
            _altBuf = new TermCell[Rows][];
            for (int r = 0; r < Rows; r++) _altBuf[r] = NewRow(TermColor.Default);
            CopyOverlap(_altBuf, oldAlt, oldRows, oldCols);
        }
        CursorRow = Math.Min(curR, Rows - 1);
        CursorCol = Math.Min(curC, Cols - 1);
    }

    private void CopyOverlap(TermCell[][] dst, TermCell[][] src, int srcRows, int srcCols)
    {
        for (int r = 0; r < Math.Min(srcRows, Rows); r++)
            for (int c = 0; c < Math.Min(srcCols, Cols); c++)
                dst[r][c] = src[r][c];
    }

    // Install a full snapshot (subscribe response / mode-2026 sync).
    public void ApplySnapshot(TermCell[][] cells, int curRow, int curCol, string? title)
    {
        if (cells.Length == 0) return;
        Rows = cells.Length;
        Cols = cells[0].Length;
        _buf = cells;
        _top = 0; _bottom = Rows - 1;
        CursorRow = Math.Clamp(curRow, 0, Rows - 1);
        CursorCol = Math.Clamp(curCol, 0, Cols - 1);
        if (title != null) Title = title;
    }

    public void Apply(IEnumerable<TermEvent> events)
    {
        foreach (var e in events) ApplyOne(e);
    }

    private void ApplyOne(TermEvent e)
    {
        switch (e.Op)
        {
            case "draw":
                foreach (var rune in EnumerateGraphemes(e.Data ?? "")) Put(rune);
                break;
            case "sgr": ApplySgr(e.Attrs); break;

            case "cup":
                CursorRow = Math.Clamp(P(e.Params, 0, 1) - 1, 0, Rows - 1);
                CursorCol = Math.Clamp(P(e.Params, 1, 1) - 1, 0, Cols - 1);
                break;
            case "cuu": case "cuu1": CursorRow = Math.Max(0, CursorRow - N(e.Params)); break;
            case "cud": case "cud1": CursorRow = Math.Min(Rows - 1, CursorRow + N(e.Params)); break;
            case "cuf": CursorCol = Math.Min(Cols - 1, CursorCol + N(e.Params)); break;
            case "cub": CursorCol = Math.Max(0, CursorCol - N(e.Params)); break;
            case "cha": case "hpa": CursorCol = Math.Clamp(P(e.Params, 0, 1) - 1, 0, Cols - 1); break;
            case "vpa": CursorRow = Math.Clamp(P(e.Params, 0, 1) - 1, 0, Rows - 1); break;

            case "cr": CursorCol = 0; break;
            case "lf": case "ind": LineFeed(); break;
            case "nel": CursorCol = 0; LineFeed(); break;
            case "ri": ReverseIndex(); break;
            case "bs": CursorCol = Math.Max(0, CursorCol - 1); break;
            case "tab": case "cht": for (int i = 0; i < N(e.Params); i++) TabForward(); break;
            case "cbt": for (int i = 0; i < N(e.Params); i++) TabBack(); break;

            case "ed": EraseDisplay(e.How); break;
            case "el": EraseLine(e.How); break;

            case "su": ScrollRegion(_top, _bottom, N(e.Params)); break;
            case "sd": ScrollRegion(_top, _bottom, -N(e.Params)); break;
            case "il": ScrollRegion(CursorRow, _bottom, -N(e.Params)); break;
            case "dl": ScrollRegion(CursorRow, _bottom, N(e.Params)); break;
            case "ich": ShiftCells(N(e.Params)); break;
            case "dch": ShiftCells(-N(e.Params)); break;
            case "ech": EraseChars(N(e.Params)); break;
            case "rep": for (int i = 0; i < N(e.Params); i++) Put(_lastChar); break;

            case "decstbm":
                _top = Math.Clamp(P(e.Params, 0, 1) - 1, 0, Rows - 1);
                _bottom = Math.Clamp(P(e.Params, 1, Rows) - 1, 0, Rows - 1);
                CursorRow = _top; CursorCol = 0;
                break;

            case "sc": case "decsc": SaveCursor(); break;
            case "rc": case "decrc": RestoreCursor(); break;

            case "sm":
                if (HasParam(e.Params, 25)) CursorVisible = true;
                if (HasParam(e.Params, 2004)) BracketedPaste = true;
                if (IsAltParam(e.Params)) EnterAlt();
                break;
            case "rm":
                if (HasParam(e.Params, 25)) CursorVisible = false;
                if (HasParam(e.Params, 2004)) BracketedPaste = false;
                if (IsAltParam(e.Params)) LeaveAlt();
                break;
            case "decscusr": CursorStyle = (e.Params != null && e.Params.Length > 0) ? e.Params[0] : 1; break;

            case "title": Title = e.Data ?? Title; break;
            case "reset": case "decstr": FullReset(); break;
            // bell, icon, charset, DEC rectangles, winop, etc. — ignored for the spike.
        }
    }

    private void Put(string grapheme)
    {
        if (CursorCol >= Cols) { CursorCol = 0; LineFeed(); }
        _buf[CursorRow][CursorCol] = new TermCell { Text = grapheme, Fg = _penFg, Bg = _penBg, Attrs = _penAttrs };
        _lastChar = grapheme;
        CursorCol++;
    }

    private void LineFeed()
    {
        if (CursorRow == _bottom) ScrollRegion(_top, _bottom, 1);
        else CursorRow = Math.Min(Rows - 1, CursorRow + 1);
    }

    private void ReverseIndex()
    {
        if (CursorRow == _top) ScrollRegion(_top, _bottom, -1);
        else CursorRow = Math.Max(0, CursorRow - 1);
    }

    private void TabForward() => CursorCol = Math.Min(Cols - 1, (CursorCol / 8 + 1) * 8);
    private void TabBack() => CursorCol = Math.Max(0, (CursorCol - 1) / 8 * 8);

    // n>0 scrolls up (content moves toward top), n<0 scrolls down.
    private void ScrollRegion(int top, int bottom, int n)
    {
        if (n == 0 || top > bottom) return;
        int count = Math.Min(Math.Abs(n), bottom - top + 1);
        if (n > 0)
        {
            for (int r = top; r <= bottom; r++)
            {
                int src = r + count;
                _buf[r] = src <= bottom ? _buf[src] : NewRow(_penBg);
            }
        }
        else
        {
            for (int r = bottom; r >= top; r--)
            {
                int src = r - count;
                _buf[r] = src >= top ? _buf[src] : NewRow(_penBg);
            }
        }
    }

    // n>0 inserts blanks at cursor (shift right), n<0 deletes (shift left).
    private void ShiftCells(int n)
    {
        var row = _buf[CursorRow];
        int count = Math.Min(Math.Abs(n), Cols - CursorCol);
        if (n > 0)
        {
            for (int c = Cols - 1; c >= CursorCol + count; c--) row[c] = row[c - count];
            for (int c = CursorCol; c < CursorCol + count; c++) row[c] = TermCell.Blank(_penBg);
        }
        else
        {
            for (int c = CursorCol; c < Cols - count; c++) row[c] = row[c + count];
            for (int c = Cols - count; c < Cols; c++) row[c] = TermCell.Blank(_penBg);
        }
    }

    private void EraseChars(int n)
    {
        var row = _buf[CursorRow];
        for (int c = CursorCol; c < Math.Min(Cols, CursorCol + n); c++) row[c] = TermCell.Blank(_penBg);
    }

    private void EraseLine(int how)
    {
        var row = _buf[CursorRow];
        int from = how == 0 ? CursorCol : 0;
        int to = how == 1 ? CursorCol : Cols - 1;
        for (int c = from; c <= to && c < Cols; c++) row[c] = TermCell.Blank(_penBg);
    }

    private void EraseDisplay(int how)
    {
        if (how == 2) { for (int r = 0; r < Rows; r++) _buf[r] = NewRow(_penBg); return; }
        EraseLine(how);
        if (how == 0) for (int r = CursorRow + 1; r < Rows; r++) _buf[r] = NewRow(_penBg);
        else for (int r = 0; r < CursorRow; r++) _buf[r] = NewRow(_penBg);
    }

    private void SaveCursor() { _savRow = CursorRow; _savCol = CursorCol; _savFg = _penFg; _savBg = _penBg; _savAttrs = _penAttrs; }
    private void RestoreCursor() { CursorRow = _savRow; CursorCol = _savCol; _penFg = _savFg; _penBg = _savBg; _penAttrs = _savAttrs; }

    private static bool IsAltParam(int[]? p) => HasParam(p, 1049) || HasParam(p, 1047) || HasParam(p, 47);

    private void EnterAlt()
    {
        if (_altActive) return;
        _altBuf = _buf;
        _altRow = CursorRow; _altCol = CursorCol; _altTop = _top; _altBottom = _bottom;
        _altFg = _penFg; _altBg = _penBg; _altAttrs = _penAttrs;

        _buf = new TermCell[Rows][];
        for (int r = 0; r < Rows; r++) _buf[r] = NewRow(TermColor.Default);
        _top = 0; _bottom = Rows - 1; CursorRow = CursorCol = 0;
        _altActive = true;
    }

    private void LeaveAlt()
    {
        if (!_altActive || _altBuf == null) return;
        _buf = _altBuf;
        CursorRow = Math.Min(_altRow, Rows - 1); CursorCol = Math.Min(_altCol, Cols - 1);
        _top = _altTop; _bottom = _altBottom;
        _penFg = _altFg; _penBg = _altBg; _penAttrs = _altAttrs;
        _altBuf = null; _altActive = false;
    }

    private void FullReset()
    {
        Allocate(Cols, Rows);
        _penFg = TermColor.Default; _penBg = TermColor.Default; _penAttrs = CellAttr.None;
        CursorVisible = true;
        _altActive = false; _altBuf = null; CursorStyle = 0;
    }

    private void ApplySgr(int[]? a)
    {
        if (a == null || a.Length == 0) { ResetPen(); return; }
        for (int i = 0; i < a.Length; i++)
        {
            int p = a[i];
            switch (p)
            {
                case 0: ResetPen(); break;
                case 1: _penAttrs |= CellAttr.Bold; break;
                case 2: _penAttrs |= CellAttr.Faint; break;
                case 3: _penAttrs |= CellAttr.Italic; break;
                case 4: _penAttrs |= CellAttr.Underline; break;
                case 5: _penAttrs |= CellAttr.Blink; break;
                case 7: _penAttrs |= CellAttr.Reverse; break;
                case 8: _penAttrs |= CellAttr.Conceal; break;
                case 9: _penAttrs |= CellAttr.Strikethrough; break;
                case 22: _penAttrs &= ~(CellAttr.Bold | CellAttr.Faint); break;
                case 23: _penAttrs &= ~CellAttr.Italic; break;
                case 24: _penAttrs &= ~CellAttr.Underline; break;
                case 25: _penAttrs &= ~CellAttr.Blink; break;
                case 27: _penAttrs &= ~CellAttr.Reverse; break;
                case 28: _penAttrs &= ~CellAttr.Conceal; break;
                case 29: _penAttrs &= ~CellAttr.Strikethrough; break;
                case 39: _penFg = TermColor.Default; break;
                case 49: _penBg = TermColor.Default; break;
                case 38: _penFg = ExtendedColor(a, ref i) ?? _penFg; break;
                case 48: _penBg = ExtendedColor(a, ref i) ?? _penBg; break;
                default:
                    if (p >= 30 && p <= 37) _penFg = TermColor.Indexed16((byte)(p - 30));
                    else if (p >= 40 && p <= 47) _penBg = TermColor.Indexed16((byte)(p - 40));
                    else if (p >= 90 && p <= 97) _penFg = TermColor.Indexed16((byte)(p - 90 + 8));
                    else if (p >= 100 && p <= 107) _penBg = TermColor.Indexed16((byte)(p - 100 + 8));
                    break;
            }
        }
    }

    // Consumes "5;n" or "2;r;g;b" following a 38/48; advances i past them.
    private static TermColor? ExtendedColor(int[] a, ref int i)
    {
        if (i + 1 >= a.Length) return null;
        int mode = a[++i];
        if (mode == 5 && i + 1 < a.Length) return TermColor.Indexed16((byte)a[++i]);
        if (mode == 2 && i + 3 < a.Length)
        {
            int r = a[++i], g = a[++i], b = a[++i];
            return TermColor.FromRgb((uint)((r << 16) | (g << 8) | b));
        }
        return null;
    }

    private void ResetPen() { _penFg = TermColor.Default; _penBg = TermColor.Default; _penAttrs = CellAttr.None; }

    private static int N(int[]? p) => (p != null && p.Length > 0 && p[0] > 0) ? p[0] : 1;
    private static int P(int[]? p, int i, int def) => (p != null && i < p.Length && p[i] > 0) ? p[i] : def;
    private static bool HasParam(int[]? p, int v) => p != null && Array.IndexOf(p, v) >= 0;

    private static IEnumerable<string> EnumerateGraphemes(string s)
    {
        var e = System.Globalization.StringInfo.GetTextElementEnumerator(s);
        while (e.MoveNext()) yield return (string)e.Current;
    }
}

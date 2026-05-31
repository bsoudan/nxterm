using Windows.System;

namespace Nx2Gui;

// Translates WinUI key events into the byte sequences a PTY expects (US layout).
// Copied from the old nxterm client — the encoding is host/app-agnostic; the
// guest forwards these to the terminal companion's PTY.
public static class KeyEncoder
{
    public static byte[]? Encode(VirtualKey key, bool ctrl, bool shift, bool alt)
    {
        if (ctrl && !alt)
        {
            if (key >= VirtualKey.A && key <= VirtualKey.Z)
                return new[] { (byte)(key - VirtualKey.A + 1) };
            switch (key)
            {
                case VirtualKey.Space: return new byte[] { 0 };
                case (VirtualKey)219: return new byte[] { 27 };  // Ctrl-[
                case (VirtualKey)221: return new byte[] { 29 };  // Ctrl-]
                case (VirtualKey)220: return new byte[] { 28 };  // Ctrl-backslash
            }
        }

        switch (key)
        {
            case VirtualKey.Enter: return new byte[] { (byte)'\r' };
            case VirtualKey.Back: return new byte[] { 0x7f };
            case VirtualKey.Tab: return shift ? new byte[] { 0x1b, (byte)'[', (byte)'Z' } : new byte[] { (byte)'\t' };
            case VirtualKey.Escape: return new byte[] { 0x1b };

            case VirtualKey.Up: return Csi(shift, alt, ctrl, 'A');
            case VirtualKey.Down: return Csi(shift, alt, ctrl, 'B');
            case VirtualKey.Right: return Csi(shift, alt, ctrl, 'C');
            case VirtualKey.Left: return Csi(shift, alt, ctrl, 'D');
            case VirtualKey.Home: return new byte[] { 0x1b, (byte)'[', (byte)'H' };
            case VirtualKey.End: return new byte[] { 0x1b, (byte)'[', (byte)'F' };
            case VirtualKey.PageUp: return new byte[] { 0x1b, (byte)'[', (byte)'5', (byte)'~' };
            case VirtualKey.PageDown: return new byte[] { 0x1b, (byte)'[', (byte)'6', (byte)'~' };
            case VirtualKey.Insert: return new byte[] { 0x1b, (byte)'[', (byte)'2', (byte)'~' };
            case VirtualKey.Delete: return new byte[] { 0x1b, (byte)'[', (byte)'3', (byte)'~' };
        }

        char ch = KeyToChar(key, shift);
        if (ch != '\0') return System.Text.Encoding.UTF8.GetBytes(ch.ToString());
        return null;
    }

    private static byte[] Csi(bool shift, bool alt, bool ctrl, char final)
    {
        int mod = 1 + (shift ? 1 : 0) + (alt ? 2 : 0) + (ctrl ? 4 : 0);
        return mod > 1
            ? System.Text.Encoding.ASCII.GetBytes($"\x1b[1;{mod}{final}")
            : System.Text.Encoding.ASCII.GetBytes($"\x1b[{final}");
    }

    private static char KeyToChar(VirtualKey key, bool shift)
    {
        if (key >= VirtualKey.A && key <= VirtualKey.Z)
        {
            char c = (char)('a' + (key - VirtualKey.A));
            return shift ? char.ToUpperInvariant(c) : c;
        }
        if (key >= VirtualKey.Number0 && key <= VirtualKey.Number9)
        {
            const string lower = "0123456789";
            const string upper = ")!@#$%^&*(";
            int i = key - VirtualKey.Number0;
            return shift ? upper[i] : lower[i];
        }
        switch (key)
        {
            case VirtualKey.Space: return ' ';
            case (VirtualKey)186: return shift ? ':' : ';';
            case (VirtualKey)187: return shift ? '+' : '=';
            case (VirtualKey)188: return shift ? '<' : ',';
            case (VirtualKey)189: return shift ? '_' : '-';
            case (VirtualKey)190: return shift ? '>' : '.';
            case (VirtualKey)191: return shift ? '?' : '/';
            case (VirtualKey)192: return shift ? '~' : '`';
            case (VirtualKey)219: return shift ? '{' : '[';
            case (VirtualKey)220: return shift ? '|' : '\\';
            case (VirtualKey)221: return shift ? '}' : ']';
            case (VirtualKey)222: return shift ? '"' : '\'';
        }
        return '\0';
    }
}

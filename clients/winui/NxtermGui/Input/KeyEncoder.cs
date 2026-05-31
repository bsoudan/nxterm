using System.Text;
using Windows.System;

namespace NxtermGui.Input;

// Translates a key press into the bytes a terminal expects (xterm-style),
// assuming a US layout. Good enough for the spike; proper layout/IME handling
// (via CoreTextEditContext) is a later phase.
public static class KeyEncoder
{
    public static byte[]? Encode(VirtualKey key, bool ctrl, bool shift, bool alt)
    {
        var seq = EncodeCore(key, ctrl, shift);
        if (seq == null) return null;
        if (alt && (seq.Length == 0 || seq[0] != 0x1b))
        {
            var meta = new byte[seq.Length + 1];
            meta[0] = 0x1b;
            Array.Copy(seq, 0, meta, 1, seq.Length);
            return meta;
        }
        return seq;
    }

    private static byte[]? EncodeCore(VirtualKey key, bool ctrl, bool shift)
    {
        if (key >= VirtualKey.A && key <= VirtualKey.Z)
        {
            int idx = key - VirtualKey.A;
            if (ctrl) return new[] { (byte)(idx + 1) };          // Ctrl+A..Z -> 0x01..0x1a
            return new[] { (byte)((shift ? 'A' : 'a') + idx) };
        }
        if (key >= VirtualKey.Number0 && key <= VirtualKey.Number9)
        {
            int n = key - VirtualKey.Number0;
            return new[] { (byte)(shift ? ")!@#$%^&*("[n] : (char)('0' + n)) };
        }
        if (key >= VirtualKey.NumberPad0 && key <= VirtualKey.NumberPad9)
            return new[] { (byte)('0' + (key - VirtualKey.NumberPad0)) };

        switch (key)
        {
            case VirtualKey.Enter: return new byte[] { 0x0d };
            case VirtualKey.Back: return new byte[] { 0x7f };
            case VirtualKey.Tab: return shift ? Csi("Z") : new byte[] { 0x09 };
            case VirtualKey.Escape: return new byte[] { 0x1b };
            case VirtualKey.Space: return new byte[] { ctrl ? (byte)0x00 : (byte)0x20 };

            case VirtualKey.Up: return Csi("A");
            case VirtualKey.Down: return Csi("B");
            case VirtualKey.Right: return Csi("C");
            case VirtualKey.Left: return Csi("D");
            case VirtualKey.Home: return Csi("H");
            case VirtualKey.End: return Csi("F");
            case VirtualKey.Insert: return Csi("2~");
            case VirtualKey.Delete: return Csi("3~");
            case VirtualKey.PageUp: return Csi("5~");
            case VirtualKey.PageDown: return Csi("6~");
        }

        // OEM punctuation carries the raw Win32 VK code (US layout).
        (char plain, char shifted)? oem = (int)key switch
        {
            186 => (';', ':'), 187 => ('=', '+'), 188 => (',', '<'), 189 => ('-', '_'),
            190 => ('.', '>'), 191 => ('/', '?'), 192 => ('`', '~'), 219 => ('[', '{'),
            220 => ('\\', '|'), 221 => (']', '}'), 222 => ('\'', '"'),
            _ => null,
        };
        if (oem is { } o)
        {
            char ch = shift ? o.shifted : o.plain;
            if (ctrl && ch == '\\') return new byte[] { 0x1c };
            return new[] { (byte)ch };
        }
        return null;
    }

    private static byte[] Csi(string tail) => Encoding.ASCII.GetBytes("\x1b[" + tail);
}

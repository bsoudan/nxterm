using System.Buffers.Binary;
using System.IO;

namespace Nx2Gui.Protocol;

// nx2 host<->broker framing: [type:1][u32 LE len][payload]. Mirrors Go
// nx2/internal/wire. Control frames carry JSON; Data frames are opaque bytes
// the broker relays blind between the guest and its companion.
public enum FrameType : byte { Control = 0, Data = 1 }

public static class Wire
{
    public const int MaxFrameLen = 16 << 20;

    public static async Task WriteFrameAsync(Stream s, FrameType t, byte[] payload, CancellationToken ct)
    {
        var hdr = new byte[5];
        hdr[0] = (byte)t;
        BinaryPrimitives.WriteUInt32LittleEndian(hdr.AsSpan(1), (uint)payload.Length);
        await s.WriteAsync(hdr, ct);
        if (payload.Length > 0) await s.WriteAsync(payload, ct);
        await s.FlushAsync(ct);
    }

    public static async Task<(FrameType, byte[])> ReadFrameAsync(Stream s, CancellationToken ct)
    {
        var hdr = new byte[5];
        await ReadFullAsync(s, hdr, ct);
        uint n = BinaryPrimitives.ReadUInt32LittleEndian(hdr.AsSpan(1));
        if (n > MaxFrameLen) throw new IOException($"frame too large: {n}");
        var buf = new byte[n];
        if (n > 0) await ReadFullAsync(s, buf, ct);
        return ((FrameType)hdr[0], buf);
    }

    private static async Task ReadFullAsync(Stream s, byte[] buf, CancellationToken ct)
    {
        int off = 0;
        while (off < buf.Length)
        {
            int n = await s.ReadAsync(buf.AsMemory(off), ct);
            if (n == 0) throw new EndOfStreamException();
            off += n;
        }
    }
}

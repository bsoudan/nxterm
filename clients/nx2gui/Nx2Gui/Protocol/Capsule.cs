using System.IO;
using System.Security.Cryptography;

namespace Nx2Gui.Protocol;

// Content-addressed app cache (mirrors Go nx2/internal/capsule client side):
// modules are keyed by "sha256:<hex>" and verified on read/write so a corrupt
// entry is treated as a miss.
public sealed class Capsule
{
    private readonly string _dir;

    public Capsule(string dir)
    {
        _dir = dir;
        Directory.CreateDirectory(_dir);
    }

    public static string Hash(byte[] b) => "sha256:" + Convert.ToHexString(SHA256.HashData(b)).ToLowerInvariant();

    public static bool Verify(string hash, byte[] b) => Hash(b) == hash;

    private string PathFor(string hash) => Path.Combine(_dir, hash.Replace(":", "_"));

    public byte[]? Get(string hash)
    {
        var p = PathFor(hash);
        if (!File.Exists(p)) return null;
        var b = File.ReadAllBytes(p);
        if (!Verify(hash, b)) { try { File.Delete(p); } catch { } return null; }
        return b;
    }

    public void Put(string hash, byte[] b)
    {
        if (!Verify(hash, b)) throw new IOException("capsule: content does not match hash");
        var final = PathFor(hash);
        var tmp = final + ".tmp-" + Guid.NewGuid().ToString("N");
        File.WriteAllBytes(tmp, b);
        File.Move(tmp, final, overwrite: true);
    }
}

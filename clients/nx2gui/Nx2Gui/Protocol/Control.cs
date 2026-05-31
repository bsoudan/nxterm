using System.Text.Json;
using System.Text.Json.Serialization;

namespace Nx2Gui.Protocol;

// nx2 control-plane messages (the JSON inside wire.Control frames). Mirrors Go
// nx2/internal/control: a typed envelope plus the resolve/fetch/select payloads.
public static class Control
{
    public const string TypeResolve = "resolve";
    public const string TypeResolved = "resolved";
    public const string TypeFetch = "fetch";
    public const string TypeChunk = "chunk";
    public const string TypeSelectApp = "select_app";
    public const string TypeSelected = "selected";

    private static readonly JsonSerializerOptions Opts = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    };

    public static byte[] Marshal(string type, object payload)
    {
        var env = new Envelope { Type = type, Payload = JsonSerializer.SerializeToElement(payload, Opts) };
        return JsonSerializer.SerializeToUtf8Bytes(env, Opts);
    }

    public static (string Type, JsonElement Payload) Parse(byte[] b)
    {
        var env = JsonSerializer.Deserialize<Envelope>(b, Opts)!;
        return (env.Type, env.Payload);
    }

    public static T PayloadAs<T>(JsonElement e) => e.Deserialize<T>(Opts)!;

    public sealed class Envelope
    {
        [JsonPropertyName("type")] public string Type { get; set; } = "";
        [JsonPropertyName("payload")] public JsonElement Payload { get; set; }
    }

    public sealed class Resolve
    {
        [JsonPropertyName("app")] public string App { get; set; } = "";
    }

    public sealed class Resolved
    {
        [JsonPropertyName("app")] public string App { get; set; } = "";
        [JsonPropertyName("hash")] public string Hash { get; set; } = "";
        [JsonPropertyName("error")] public bool Error { get; set; }
        [JsonPropertyName("message")] public string? Message { get; set; }
    }

    public sealed class Fetch
    {
        [JsonPropertyName("hash")] public string Hash { get; set; } = "";
    }

    public sealed class Chunk
    {
        [JsonPropertyName("hash")] public string Hash { get; set; } = "";
        [JsonPropertyName("data")] public byte[]? Data { get; set; } // JSON base64
        [JsonPropertyName("done")] public bool Done { get; set; }
        [JsonPropertyName("error")] public bool Error { get; set; }
        [JsonPropertyName("message")] public string? Message { get; set; }
    }

    public sealed class SelectApp
    {
        [JsonPropertyName("surface")] public uint Surface { get; set; }
        [JsonPropertyName("app")] public string App { get; set; } = "";
        [JsonPropertyName("session")] public string? Session { get; set; }
    }

    public sealed class Selected
    {
        [JsonPropertyName("surface")] public uint Surface { get; set; }
        [JsonPropertyName("error")] public bool Error { get; set; }
        [JsonPropertyName("message")] public string? Message { get; set; }
    }
}

const std = @import("std");
const Server = @import("server.zig").Server;

// Compile in all log levels; runtime filtering in customLog.
pub const std_options: std.Options = .{
    .log_level = .debug,
    .logFn = customLog,
};

var debug_enabled: bool = false;

fn customLog(
    comptime level: std.log.Level,
    comptime scope: @TypeOf(.enum_literal),
    comptime format: []const u8,
    args: anytype,
) void {
    if (!debug_enabled and level == .debug) return;

    const scope_prefix = if (scope != .default) @tagName(scope) ++ ": " else "";
    const prefix = comptime @tagName(level) ++ "(" ++ scope_prefix ++ "): ";

    var buf: [4096]u8 = undefined;
    const msg = std.fmt.bufPrint(&buf, prefix ++ format ++ "\n", args) catch return;
    _ = std.posix.write(2, msg) catch {};
}

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const alloc = gpa.allocator();

    const args = try std.process.argsAlloc(alloc);
    defer std.process.argsFree(alloc, args);

    // Parse flags: [-debug] [socket_path]
    var socket_path: [:0]const u8 = "/tmp/termd.sock";
    for (args[1..]) |arg| {
        if (std.mem.eql(u8, arg, "-debug")) {
            debug_enabled = true;
        } else {
            socket_path = arg;
        }
    }

    // Check TERMD_DEBUG=1 env var as fallback
    if (!debug_enabled) {
        if (std.posix.getenv("TERMD_DEBUG")) |val| {
            if (std.mem.eql(u8, val, "1")) debug_enabled = true;
        }
    }

    var server = try Server.init(alloc, socket_path);
    defer server.deinit();
    try server.run();
}

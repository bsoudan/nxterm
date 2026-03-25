const std = @import("std");
const Server = @import("server.zig").Server;

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const alloc = gpa.allocator();

    const args = try std.process.argsAlloc(alloc);
    defer std.process.argsFree(alloc, args);

    const socket_path: [:0]const u8 = if (args.len > 1) args[1] else "/tmp/termd.sock";

    var server = try Server.init(alloc, socket_path);
    defer server.deinit();
    try server.run();
}

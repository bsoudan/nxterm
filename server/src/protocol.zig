const std = @import("std");
const log = std.log.scoped(.protocol);

// ── Outbound messages (server → frontend/termctl) ───────────────────────────

pub const SpawnResponse = struct {
    region_id: []const u8,
    name: []const u8,
    @"error": bool,
    message: []const u8,
};

pub const SubscribeResponse = struct {
    region_id: []const u8,
    @"error": bool,
    message: []const u8,
};

pub const ResizeResponse = struct {
    region_id: []const u8,
    @"error": bool,
    message: []const u8,
};

pub const RegionCreated = struct {
    region_id: []const u8,
    name: []const u8,
};

pub const ScreenUpdate = struct {
    region_id: []const u8,
    cursor_row: u16,
    cursor_col: u16,
    lines: []const []const u8,
};

pub const RegionDestroyed = struct {
    region_id: []const u8,
};

pub const RegionInfo = struct {
    region_id: []const u8,
    name: []const u8,
    cmd: []const u8,
    pid: i32,
};

pub const ListRegionsResponse = struct {
    regions: []const RegionInfo,
    @"error": bool,
    message: []const u8,
};

pub const StatusResponse = struct {
    pid: i32,
    uptime_seconds: i64,
    socket_path: []const u8,
    num_clients: u32,
    num_regions: u32,
    @"error": bool,
    message: []const u8,
};

pub const GetScreenResponse = struct {
    region_id: []const u8,
    cursor_row: u16,
    cursor_col: u16,
    lines: []const []const u8,
    @"error": bool,
    message: []const u8,
};

pub const KillRegionResponse = struct {
    region_id: []const u8,
    @"error": bool,
    message: []const u8,
};

pub const ClientInfo = struct {
    client_id: u32,
    hostname: []const u8,
    username: []const u8,
    pid: i32,
    process: []const u8,
    subscribed_region_id: []const u8,
};

pub const ListClientsResponse = struct {
    clients: []const ClientInfo,
    @"error": bool,
    message: []const u8,
};

pub const KillClientResponse = struct {
    client_id: u32,
    @"error": bool,
    message: []const u8,
};

pub const OutboundMessage = union(enum) {
    spawn_response: SpawnResponse,
    subscribe_response: SubscribeResponse,
    resize_response: ResizeResponse,
    region_created: RegionCreated,
    screen_update: ScreenUpdate,
    region_destroyed: RegionDestroyed,
    list_regions_response: ListRegionsResponse,
    status_response: StatusResponse,
    get_screen_response: GetScreenResponse,
    kill_region_response: KillRegionResponse,
    list_clients_response: ListClientsResponse,
    kill_client_response: KillClientResponse,
};

// ── Inbound messages (frontend/termctl → server) ────────────────────────────

pub const Identify = struct {
    hostname: []const u8,
    username: []const u8,
    pid: i32,
    process: []const u8,
};

pub const SpawnRequest = struct {
    cmd: []const u8,
    args: []const []const u8,
};

pub const SubscribeRequest = struct {
    region_id: []const u8,
};

pub const InputMsg = struct {
    region_id: []const u8,
    data: []const u8,
};

pub const ResizeRequest = struct {
    region_id: []const u8,
    width: u16,
    height: u16,
};

pub const ListRegionsRequest = struct {};
pub const StatusRequest = struct {};
pub const ListClientsRequest = struct {};

pub const GetScreenRequest = struct {
    region_id: []const u8,
};

pub const KillRegionRequest = struct {
    region_id: []const u8,
};

pub const KillClientRequest = struct {
    client_id: u32,
};

pub const InboundMessage = union(enum) {
    identify: Identify,
    spawn_request: SpawnRequest,
    subscribe_request: SubscribeRequest,
    input: InputMsg,
    resize_request: ResizeRequest,
    list_regions_request: ListRegionsRequest,
    status_request: StatusRequest,
    get_screen_request: GetScreenRequest,
    kill_region_request: KillRegionRequest,
    list_clients_request: ListClientsRequest,
    kill_client_request: KillClientRequest,
};

// ── Serialization ────────────────────────────────────────────────────────────

const TypeTag = struct { @"type": []const u8 };
const json_opts: std.json.ParseOptions = .{ .ignore_unknown_fields = true };

pub fn parseInbound(alloc: std.mem.Allocator, line: []const u8) !InboundMessage {
    const tag = try std.json.parseFromSliceLeaky(TypeTag, alloc, line, json_opts);
    log.debug("recv type={s}", .{tag.@"type"});

    const t = tag.@"type";
    if (std.mem.eql(u8, t, "identify")) {
        return .{ .identify = try std.json.parseFromSliceLeaky(Identify, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "spawn_request")) {
        return .{ .spawn_request = try std.json.parseFromSliceLeaky(SpawnRequest, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "subscribe_request")) {
        return .{ .subscribe_request = try std.json.parseFromSliceLeaky(SubscribeRequest, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "input")) {
        return .{ .input = try std.json.parseFromSliceLeaky(InputMsg, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "resize_request")) {
        return .{ .resize_request = try std.json.parseFromSliceLeaky(ResizeRequest, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "list_regions_request")) {
        return .{ .list_regions_request = .{} };
    } else if (std.mem.eql(u8, t, "status_request")) {
        return .{ .status_request = .{} };
    } else if (std.mem.eql(u8, t, "get_screen_request")) {
        return .{ .get_screen_request = try std.json.parseFromSliceLeaky(GetScreenRequest, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "kill_region_request")) {
        return .{ .kill_region_request = try std.json.parseFromSliceLeaky(KillRegionRequest, alloc, line, json_opts) };
    } else if (std.mem.eql(u8, t, "list_clients_request")) {
        return .{ .list_clients_request = .{} };
    } else if (std.mem.eql(u8, t, "kill_client_request")) {
        return .{ .kill_client_request = try std.json.parseFromSliceLeaky(KillClientRequest, alloc, line, json_opts) };
    }

    return error.UnknownMessageType;
}

fn fmtWrite(writer: *std.io.Writer, value: anytype) !void {
    try writer.print("{f}", .{std.json.fmt(value, .{})});
}

pub fn writeOutbound(writer: *std.io.Writer, msg: OutboundMessage) !void {
    switch (msg) {
        .spawn_response => |r| try fmtWrite(writer, .{
            .@"type" = "spawn_response", .region_id = r.region_id,
            .name = r.name, .@"error" = r.@"error", .message = r.message,
        }),
        .subscribe_response => |r| try fmtWrite(writer, .{
            .@"type" = "subscribe_response", .region_id = r.region_id,
            .@"error" = r.@"error", .message = r.message,
        }),
        .resize_response => |r| try fmtWrite(writer, .{
            .@"type" = "resize_response", .region_id = r.region_id,
            .@"error" = r.@"error", .message = r.message,
        }),
        .region_created => |r| try fmtWrite(writer, .{
            .@"type" = "region_created", .region_id = r.region_id, .name = r.name,
        }),
        .screen_update => |r| try fmtWrite(writer, .{
            .@"type" = "screen_update", .region_id = r.region_id,
            .cursor_row = r.cursor_row, .cursor_col = r.cursor_col, .lines = r.lines,
        }),
        .region_destroyed => |r| try fmtWrite(writer, .{
            .@"type" = "region_destroyed", .region_id = r.region_id,
        }),
        .list_regions_response => |r| try fmtWrite(writer, .{
            .@"type" = "list_regions_response", .regions = r.regions,
            .@"error" = r.@"error", .message = r.message,
        }),
        .status_response => |r| try fmtWrite(writer, .{
            .@"type" = "status_response", .pid = r.pid,
            .uptime_seconds = r.uptime_seconds, .socket_path = r.socket_path,
            .num_clients = r.num_clients, .num_regions = r.num_regions,
            .@"error" = r.@"error", .message = r.message,
        }),
        .get_screen_response => |r| try fmtWrite(writer, .{
            .@"type" = "get_screen_response", .region_id = r.region_id,
            .cursor_row = r.cursor_row, .cursor_col = r.cursor_col, .lines = r.lines,
            .@"error" = r.@"error", .message = r.message,
        }),
        .kill_region_response => |r| try fmtWrite(writer, .{
            .@"type" = "kill_region_response", .region_id = r.region_id,
            .@"error" = r.@"error", .message = r.message,
        }),
        .list_clients_response => |r| try fmtWrite(writer, .{
            .@"type" = "list_clients_response", .clients = r.clients,
            .@"error" = r.@"error", .message = r.message,
        }),
        .kill_client_response => |r| try fmtWrite(writer, .{
            .@"type" = "kill_client_response", .client_id = r.client_id,
            .@"error" = r.@"error", .message = r.message,
        }),
    }
    try writer.writeByte('\n');
    switch (msg) {
        .screen_update => |r| log.debug("send type=screen_update cursor=({d},{d})", .{ r.cursor_row, r.cursor_col }),
        else => log.debug("send type={s}", .{@tagName(msg)}),
    }
}

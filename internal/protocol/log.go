package protocol

import (
	"encoding/base64"
	"fmt"
	"log/slog"
)

// LogProtocolMsg logs a protocol message with structured key=value fields.
func LogProtocolMsg(direction string, msg any) {
	// Unwrap protocol.Message and tagged wrappers so the inner
	// payload hits the specific cases instead of the default.
	if m, ok := msg.(Message); ok {
		LogProtocolMsg(direction, m.Payload)
		return
	}
	if inner := UnwrapTagged(msg); inner != nil {
		LogProtocolMsg(direction, inner)
		return
	}

	switch m := msg.(type) {
	case Identify:
		slog.Debug(direction, "type", "identify", "hostname", m.Hostname, "username", m.Username, "pid", m.Pid, "process", m.Process)
	case SessionConnectRequest:
		slog.Debug(direction, "type", "session_connect_request", "session", m.Session, "width", m.Width, "height", m.Height)
	case SessionConnectResponse:
		slog.Debug(direction, "type", "session_connect_response", "session", m.Session, "regions", len(m.Regions), "programs", len(m.Programs), "error", m.Error, "message", m.Message)
	case SpawnRequest:
		slog.Debug(direction, "type", "spawn_request", "program", m.Program, "session", m.Session)
	case SpawnResponse:
		slog.Debug(direction, "type", "spawn_response", "region_id", m.RegionID, "name", m.Name, "error", m.Error, "message", m.Message)
	case SubscribeRequest:
		slog.Debug(direction, "type", "subscribe_request", "region_id", m.RegionID)
	case SubscribeResponse:
		slog.Debug(direction, "type", "subscribe_response", "region_id", m.RegionID, "error", m.Error, "message", m.Message)
	case UnsubscribeRequest:
		slog.Debug(direction, "type", "unsubscribe_request", "region_id", m.RegionID)
	case InputMsg:
		decoded, _ := base64.StdEncoding.DecodeString(m.Data)
		slog.Debug(direction, "type", "input", "region_id", m.RegionID, "data", fmt.Sprintf("[%d bytes]", len(decoded)))
	case ResizeRequest:
		slog.Debug(direction, "type", "resize_request", "region_id", m.RegionID, "width", m.Width, "height", m.Height)
	case ResizeResponse:
		slog.Debug(direction, "type", "resize_response", "region_id", m.RegionID, "error", m.Error)
	case ListRegionsRequest:
		slog.Debug(direction, "type", "list_regions_request")
	case ListRegionsResponse:
		slog.Debug(direction, "type", "list_regions_response", "regions", len(m.Regions), "error", m.Error)
	case StatusRequest:
		slog.Debug(direction, "type", "status_request")
	case StatusResponse:
		slog.Debug(direction, "type", "status_response", "pid", m.Pid, "uptime", m.UptimeSeconds, "clients", m.NumClients, "regions", m.NumRegions)
	case GetScreenRequest:
		slog.Debug(direction, "type", "get_screen_request", "region_id", m.RegionID)
	case GetScreenResponse:
		slog.Debug(direction, "type", "get_screen_response", "region_id", m.RegionID, "cursor", fmt.Sprintf("(%d,%d)", m.CursorRow, m.CursorCol), "lines", fmt.Sprintf("[%d lines]", len(m.Lines)), "error", m.Error)
	case ScreenUpdate:
		slog.Debug(direction, "type", "screen_update", "region_id", m.RegionID, "cursor", fmt.Sprintf("(%d,%d)", m.CursorRow, m.CursorCol), "lines", fmt.Sprintf("[%d lines]", len(m.Lines)), "title", m.Title)
	case RegionCreated:
		slog.Debug(direction, "type", "region_created", "region_id", m.RegionID, "name", m.Name)
	case RegionDestroyed:
		slog.Debug(direction, "type", "region_destroyed", "region_id", m.RegionID)
	case KillRegionRequest:
		slog.Debug(direction, "type", "kill_region_request", "region_id", m.RegionID)
	case KillRegionResponse:
		slog.Debug(direction, "type", "kill_region_response", "region_id", m.RegionID, "error", m.Error)
	case ListClientsRequest:
		slog.Debug(direction, "type", "list_clients_request")
	case ListClientsResponse:
		slog.Debug(direction, "type", "list_clients_response", "clients", len(m.Clients), "error", m.Error)
	case KillClientRequest:
		slog.Debug(direction, "type", "kill_client_request", "client_id", m.ClientID)
	case KillClientResponse:
		slog.Debug(direction, "type", "kill_client_response", "client_id", m.ClientID, "error", m.Error)
	case TerminalEvents:
		slog.Debug(direction, "type", "terminal_events", "region_id", m.RegionID, "events", len(m.Events))
	case Disconnect:
		slog.Debug(direction, "type", "disconnect")
	default:
		slog.Debug(direction, "type", fmt.Sprintf("%T", msg))
	}
}

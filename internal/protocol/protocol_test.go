package protocol

import (
	"strings"
	"testing"
)

func TestParseInboundSpawnResponse(t *testing.T) {
	input := []byte(`{"type":"spawn_response","region_id":"r1","name":"bash","error":false,"message":""}`)
	msg, err := ParseInbound(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sr, ok := msg.Payload.(SpawnResponse)
	if !ok {
		t.Fatalf("expected SpawnResponse, got %T", msg)
	}
	if sr.RegionID != "r1" {
		t.Errorf("expected region_id r1, got %s", sr.RegionID)
	}
	if sr.Name != "bash" {
		t.Errorf("expected name bash, got %s", sr.Name)
	}
}

func TestParseInboundTerminalEvents(t *testing.T) {
	input := []byte(`{"type":"terminal_events","region_id":"r2","events":[{"op":"draw","data":"hi"}]}`)
	msg, err := ParseInbound(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	te, ok := msg.Payload.(TerminalEvents)
	if !ok {
		t.Fatalf("expected TerminalEvents, got %T", msg)
	}
	if te.RegionID != "r2" {
		t.Errorf("expected region_id r2, got %s", te.RegionID)
	}
	if len(te.Events) != 1 || te.Events[0].Op != "draw" {
		t.Errorf("unexpected events: %+v", te.Events)
	}
}

func TestParseInboundEmptyJSON(t *testing.T) {
	_, err := ParseInbound([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for empty JSON object, got nil")
	}
}

func TestParseInboundMissingType(t *testing.T) {
	_, err := ParseInbound([]byte(`{"region_id":"abc"}`))
	if err == nil {
		t.Fatal("expected error for missing type field, got nil")
	}
}

func TestParseInboundUnknownType(t *testing.T) {
	_, err := ParseInbound([]byte(`{"type":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown message type") {
		t.Errorf("expected error to contain 'unknown message type', got: %v", err)
	}
}

func TestParseInboundTruncatedJSON(t *testing.T) {
	_, err := ParseInbound([]byte(`{broken`))
	if err == nil {
		t.Fatal("expected error for truncated JSON, got nil")
	}
}

func TestParseInboundWrongFieldTypes(t *testing.T) {
	_, err := ParseInbound([]byte(`{"type":"spawn_response","error":"notabool"}`))
	if err == nil {
		t.Fatal("expected error for wrong field types, got nil")
	}
}

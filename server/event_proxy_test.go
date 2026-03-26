package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	te "github.com/rcarmo/go-te/pkg/te"
	"termd/frontend/protocol"
	"termd/frontend/ui"
)

// roundTripEvents serializes events to JSON and back, simulating the network.
func roundTripEvents(events []protocol.TerminalEvent) []protocol.TerminalEvent {
	msg := protocol.TerminalEvents{
		Type:     "terminal_events",
		RegionID: "test",
		Events:   events,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	var out protocol.TerminalEvents
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out.Events
}

func TestEventProxyReplay(t *testing.T) {
	// Simulate top-like behavior: home, erase, draw lines, repeat
	const cols, rows = 80, 24

	// "Server" screen with proxy
	serverScreen := te.NewScreen(cols, rows)
	proxy := NewEventProxy(serverScreen)
	stream := te.NewStream(proxy, false)

	// Simulate: prompt, then top-like redraw cycle
	input := ""
	input += "termd$ " // Draw prompt
	input += "top\r\n" // Type "top" + enter

	// Top startup: home, clear, draw header
	input += "\x1b[?1h"   // DECCKM
	input += "\x1b[?25l"  // Hide cursor
	input += "\x1b[H"     // Cursor home
	input += "\x1b[2J"    // Erase display

	// Draw top's display
	for row := range rows {
		input += fmt.Sprintf("\x1b[%d;1H", row+1) // Position cursor
		input += fmt.Sprintf("top line %02d", row) // Draw content
		input += "\x1b[K"                          // Erase to end of line
	}

	// Simulate a second refresh cycle (top refreshes)
	input += "\x1b[H" // Home
	for row := range rows {
		input += fmt.Sprintf("\x1b[%d;1H", row+1)
		input += fmt.Sprintf("top refresh %02d", row)
		input += "\x1b[K"
	}

	// Top exits: move to bottom, show cursor, reset DECCKM
	input += fmt.Sprintf("\x1b[%d;1H", rows) // Move to last row
	input += "\x1b[K"                         // Clear last line
	input += "\x1b[?25h"                      // Show cursor
	input += "\x1b[?1l"                       // Reset DECCKM

	// Bash prompt after top exits
	input += "\r\n"
	input += "termd$ "

	stream.FeedBytes([]byte(input))
	allEvents := proxy.Flush()

	t.Logf("total events captured: %d", len(allEvents))

	// Round-trip through JSON (simulates network)
	allEvents = roundTripEvents(allEvents)

	// "Frontend" screen: replay events
	frontendScreen := te.NewScreen(cols, rows)
	ui.ReplayEvents(frontendScreen, allEvents)

	// Compare
	serverDisplay := serverScreen.Display()
	frontendDisplay := frontendScreen.Display()

	mismatches := 0
	for i := range serverDisplay {
		if i >= len(frontendDisplay) {
			break
		}
		sLine := strings.TrimRight(serverDisplay[i], " ")
		fLine := strings.TrimRight(frontendDisplay[i], " ")
		if sLine != fLine {
			t.Logf("row %d mismatch:\n  server:   %q\n  frontend: %q", i, sLine, fLine)
			mismatches++
		}
	}

	t.Logf("server cursor: (%d,%d), frontend cursor: (%d,%d)",
		serverScreen.Cursor.Row, serverScreen.Cursor.Col,
		frontendScreen.Cursor.Row, frontendScreen.Cursor.Col)

	if serverScreen.Cursor.Row != frontendScreen.Cursor.Row ||
		serverScreen.Cursor.Col != frontendScreen.Cursor.Col {
		t.Errorf("cursor mismatch: server=(%d,%d) frontend=(%d,%d)",
			serverScreen.Cursor.Row, serverScreen.Cursor.Col,
			frontendScreen.Cursor.Row, frontendScreen.Cursor.Col)
	}

	if mismatches > 0 {
		t.Errorf("%d rows differ", mismatches)
	}
}

func TestEventProxyReplayWithAltScreen(t *testing.T) {
	const cols, rows = 80, 24

	serverScreen := te.NewScreen(cols, rows)
	proxy := NewEventProxy(serverScreen)
	stream := te.NewStream(proxy, false)

	input := ""
	input += "termd$ " // Initial prompt

	// Enter alt screen
	input += "\x1b[?1049h"
	input += "\x1b[H\x1b[2J" // Home + clear

	// Draw on alt screen
	for row := 0; row < rows; row++ {
		input += fmt.Sprintf("\x1b[%d;1H", row+1)
		input += fmt.Sprintf("alt line %02d", row)
		input += "\x1b[K"
	}

	// Exit alt screen
	input += "\x1b[?1049l"

	// New prompt
	input += "termd$ "

	stream.FeedBytes([]byte(input))
	allEvents := proxy.Flush()

	t.Logf("total events: %d", len(allEvents))

	allEvents = roundTripEvents(allEvents)

	frontendScreen := te.NewScreen(cols, rows)
	ui.ReplayEvents(frontendScreen, allEvents)

	serverDisplay := serverScreen.Display()
	frontendDisplay := frontendScreen.Display()

	mismatches := 0
	for i := 0; i < len(serverDisplay) && i < len(frontendDisplay); i++ {
		sLine := strings.TrimRight(serverDisplay[i], " ")
		fLine := strings.TrimRight(frontendDisplay[i], " ")
		if sLine != fLine {
			t.Logf("row %d mismatch:\n  server:   %q\n  frontend: %q", i, sLine, fLine)
			mismatches++
		}
	}

	t.Logf("server cursor: (%d,%d), frontend cursor: (%d,%d)",
		serverScreen.Cursor.Row, serverScreen.Cursor.Col,
		frontendScreen.Cursor.Row, frontendScreen.Cursor.Col)

	if serverScreen.Cursor.Row != frontendScreen.Cursor.Row ||
		serverScreen.Cursor.Col != frontendScreen.Cursor.Col {
		t.Errorf("cursor mismatch: server=(%d,%d) frontend=(%d,%d)",
			serverScreen.Cursor.Row, serverScreen.Cursor.Col,
			frontendScreen.Cursor.Row, frontendScreen.Cursor.Col)
	}

	if mismatches > 0 {
		t.Errorf("%d rows differ", mismatches)
	}
}

func TestEventProxyReplayColors(t *testing.T) {
	const cols, rows = 80, 24

	serverScreen := te.NewScreen(cols, rows)
	proxy := NewEventProxy(serverScreen)
	stream := te.NewStream(proxy, false)

	// ANSI16 red, green, bold, 256-color orange (208), true-color
	input := "\x1b[31mRED\x1b[0m \x1b[32mGRN\x1b[0m \x1b[1mBLD\x1b[0m \x1b[38;5;208mIDX\x1b[0m \x1b[38;2;255;128;0mRGB\x1b[0m"
	stream.FeedBytes([]byte(input))

	allEvents := proxy.Flush()
	allEvents = roundTripEvents(allEvents)

	frontendScreen := te.NewScreen(cols, rows)
	ui.ReplayEvents(frontendScreen, allEvents)

	fc := frontendScreen.LinesCells()[0]

	// RED at col 0: ANSI16 red
	if fc[0].Attr.Fg.Name != "red" {
		t.Errorf("'R' expected red fg, got %+v", fc[0].Attr.Fg)
	}
	// GRN at col 4: ANSI16 green
	if fc[4].Attr.Fg.Name != "green" {
		t.Errorf("'G' expected green fg, got %+v", fc[4].Attr.Fg)
	}
	// BLD at col 8: bold
	if !fc[8].Attr.Bold {
		t.Errorf("'B' expected bold, got %+v", fc[8].Attr)
	}
	// IDX at col 12: 256-color index 208
	if fc[12].Attr.Fg.Mode != te.ColorANSI256 || fc[12].Attr.Fg.Index != 208 {
		t.Errorf("'I' expected ANSI256 index 208, got %+v", fc[12].Attr.Fg)
	}
	// RGB at col 16: true color
	if fc[16].Attr.Fg.Mode != te.ColorTrueColor {
		t.Errorf("'R' expected TrueColor, got %+v", fc[16].Attr.Fg)
	}
}



package tui

import (
	"strings"
	"testing"

	"nxtermd/internal/protocol"
	"nxtermd/pkg/te"
)

// TestClientEventReplayRendersOverline isolates the client side of the
// attr-plumbing chain: replay the SGR events the server emits (curly underline,
// underline color, overline), then render the line and parse it back.
func TestClientEventReplayRendersOverline(t *testing.T) {
	s := te.NewScreen(6, 1)
	events := []protocol.TerminalEvent{
		{Op: "sgr", Attrs: []int{10003}},   // 4:3 curly (marker)
		{Op: "sgr", Attrs: []int{58, 5, 9}}, // underline color
		{Op: "sgr", Attrs: []int{53}},       // overline
		{Op: "draw", Data: "D"},
	}
	ReplayEvents(s, events)
	cell := s.Buffer[0][0]
	if !cell.Attr.Overline {
		t.Fatalf("replay: overline not set on cell: %+v", cell.Attr)
	}
	if cell.Attr.UnderlineStyle != 3 {
		t.Fatalf("replay: underline style = %d, want 3", cell.Attr.UnderlineStyle)
	}

	var sb strings.Builder
	renderCellLine(&sb, s.Buffer[0], 6, 0, -1, -1, false, false, false)
	out := sb.String()
	t.Logf("rendered: %q", out)

	s2 := te.NewScreen(6, 1)
	st := te.NewStream(s2, false)
	if err := st.Feed(out); err != nil {
		t.Fatal(err)
	}
	got := s2.Buffer[0][0].Attr
	if !got.Overline {
		t.Fatalf("round-trip: overline lost. parsed=%+v", got)
	}
	if got.UnderlineStyle != 3 {
		t.Fatalf("round-trip: underline style = %d, want 3", got.UnderlineStyle)
	}
}

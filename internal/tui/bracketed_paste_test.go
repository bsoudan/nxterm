package tui

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"nxtermd/pkg/layer"
	"nxtermd/pkg/te"
)

// recordingLayer is a minimal base layer that records every message the model
// dispatches into the stack. It is deliberately NOT a TermdLayer, so
// needsFocusRouting skips it and the normal raw-input path runs.
type recordingLayer struct {
	msgs []tea.Msg
}

func (r *recordingLayer) Activate() tea.Cmd { return nil }
func (r *recordingLayer) Deactivate()       {}
func (r *recordingLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	r.msgs = append(r.msgs, msg)
	return nil, nil, true
}
func (r *recordingLayer) View(width, height int, rs *RenderState) []*lipgloss.Layer {
	return nil
}

func newPasteTestModel() (*NxtermModel, *recordingLayer) {
	rec := &recordingLayer{}
	m := &NxtermModel{
		registry: &Registry{PrefixKey: 0x02}, // ctrl+b
		stack:    layer.NewStack[RenderState](rec),
	}
	return m, rec
}

// raws returns the concatenation of every RawInputMsg the model dispatched.
func (r *recordingLayer) raws() []byte {
	var out []byte
	for _, m := range r.msgs {
		if ri, ok := m.(RawInputMsg); ok {
			out = append(out, ri...)
		}
	}
	return out
}

// pastes returns the concatenation of every PasteInputMsg's content.
func (r *recordingLayer) pastes() []byte {
	var out []byte
	for _, m := range r.msgs {
		if p, ok := m.(PasteInputMsg); ok {
			out = append(out, p.Data...)
		}
	}
	return out
}

// TestBracketedPastePrefixByteNotInterpreted verifies a prefix byte inside a
// paste is delivered as literal paste content rather than entering command
// mode, and that the paste markers themselves are not dispatched as raw input.
func TestBracketedPastePrefixByteNotInterpreted(t *testing.T) {
	m, rec := newPasteTestModel()

	// Paste containing the prefix byte (0x02) followed by 'c' — which in
	// command mode would open a new tab.
	in := append([]byte("\x1b[200~hi\x02cthere"), []byte("\x1b[201~")...)
	if _, err := m.processRawInput(RawInputMsg(in)); err != nil {
		t.Fatalf("processRawInput: %v", err)
	}

	if m.commandMode {
		t.Fatal("paste containing the prefix byte entered command mode")
	}
	if got, want := rec.pastes(), []byte("hi\x02cthere"); !bytes.Equal(got, want) {
		t.Fatalf("paste content = %q, want %q", got, want)
	}
	if got := rec.raws(); len(got) != 0 {
		t.Fatalf("paste content leaked into raw-input path: %q", got)
	}
}

// TestBracketedPasteSplitAcrossReads verifies a paste whose content spans
// multiple RawInputMsg reads is reassembled as literal content, with the
// prefix byte in a later read still not interpreted.
func TestBracketedPasteSplitAcrossReads(t *testing.T) {
	m, rec := newPasteTestModel()

	chunks := [][]byte{
		[]byte("\x1b[200~aaa"),
		[]byte("\x02bbb"),
		[]byte("ccc\x1b[201~"),
	}
	for _, c := range chunks {
		if _, err := m.processRawInput(RawInputMsg(c)); err != nil {
			t.Fatalf("processRawInput: %v", err)
		}
	}

	if m.commandMode {
		t.Fatal("split paste entered command mode")
	}
	if m.pasting {
		t.Fatal("paste state not cleared after the end marker")
	}
	if got, want := rec.pastes(), []byte("aaa\x02bbbccc"); !bytes.Equal(got, want) {
		t.Fatalf("reassembled paste content = %q, want %q", got, want)
	}
}

// TestBracketedPasteInterleavedWithNormalInput verifies bytes before the start
// marker and after the end marker still take the normal interpreted path
// (prefix scan / raw forwarding), while only the bracketed span is literal.
func TestBracketedPasteInterleavedWithNormalInput(t *testing.T) {
	m, rec := newPasteTestModel()

	in := []byte("AB\x1b[200~PASTE\x1b[201~CD")
	if _, err := m.processRawInput(RawInputMsg(in)); err != nil {
		t.Fatalf("processRawInput: %v", err)
	}

	if m.commandMode {
		t.Fatal("unexpected command mode")
	}
	if got, want := rec.pastes(), []byte("PASTE"); !bytes.Equal(got, want) {
		t.Fatalf("paste content = %q, want %q", got, want)
	}
	if got, want := rec.raws(), []byte("ABCD"); !bytes.Equal(got, want) {
		t.Fatalf("interpreted (raw) input = %q, want %q", got, want)
	}
}

func sessionWithTerm(mode2004 bool) (*SessionLayer, *Server) {
	srv := NewServer(8, "test")
	hs := te.NewHistoryScreen(80, 24, 100)
	hs.Mode = map[int]struct{}{}
	if mode2004 {
		hs.Mode[privateModeKey(2004)] = struct{}{}
	}
	term := &TerminalLayer{hscreen: hs}
	s := &SessionLayer{server: srv, tabs: []tab{{regionID: "r1", term: term}}}
	return s, srv
}

// TestPasteMarkersStrippedWhenChildLacksMode2004 verifies a child that never
// enabled bracketed paste receives only the content, not the ESC[200~/ESC[201~
// markers (which it would otherwise render as garbage text).
func TestPasteMarkersStrippedWhenChildLacksMode2004(t *testing.T) {
	s, srv := sessionWithTerm(false)
	s.sendPasteToServer(PasteInputMsg{Data: []byte("hello"), Start: true, End: true})

	im := (<-srv.ch).(InputMsg)
	if got, want := im.Data, []byte("hello"); !bytes.Equal(got, want) {
		t.Fatalf("forwarded %q, want %q (markers must be stripped)", got, want)
	}
}

// TestPasteMarkersForwardedWhenChildEnabledMode2004 verifies a child that opted
// into bracketed paste receives the markers around the content.
func TestPasteMarkersForwardedWhenChildEnabledMode2004(t *testing.T) {
	s, srv := sessionWithTerm(true)
	s.sendPasteToServer(PasteInputMsg{Data: []byte("hello"), Start: true, End: true})

	im := (<-srv.ch).(InputMsg)
	want := []byte("\x1b[200~hello\x1b[201~")
	if !bytes.Equal(im.Data, want) {
		t.Fatalf("forwarded %q, want %q", im.Data, want)
	}
}

// TestPrefixByteOutsidePasteStillCommands is the control: the same prefix byte
// outside a paste must still enter command mode.
func TestPrefixByteOutsidePasteStillCommands(t *testing.T) {
	m, _ := newPasteTestModel()
	if _, err := m.processRawInput(RawInputMsg([]byte("\x02"))); err != nil {
		t.Fatalf("processRawInput: %v", err)
	}
	if !m.commandMode {
		t.Fatal("prefix byte outside a paste did not enter command mode")
	}
}

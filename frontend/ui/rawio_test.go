package ui

import (
	"bytes"
	"testing"
	"time"
)

// newTestParser creates an InputParser with a short timeout and channels
// for feeding input and collecting output. Call close(inputCh) to stop.
func newTestParser() (inputCh chan []byte, outputCh chan []byte, parser *InputParser) {
	inputCh = make(chan []byte, 16)
	outputCh = make(chan []byte, 16)
	parser = &InputParser{
		Input:      inputCh,
		Send:       func(msg RawInputMsg) { outputCh <- []byte(msg) },
		EscTimeout: 10 * time.Millisecond,
	}
	return
}

// recvOne reads one message from outputCh with a timeout.
func recvOne(t *testing.T, outputCh <-chan []byte, timeout time.Duration) []byte {
	t.Helper()
	select {
	case msg := <-outputCh:
		return msg
	case <-time.After(timeout):
		t.Fatal("timeout waiting for output")
		return nil
	}
}

// expectNoOutput verifies nothing is emitted within the given duration.
func expectNoOutput(t *testing.T, outputCh <-chan []byte, dur time.Duration) {
	t.Helper()
	select {
	case msg := <-outputCh:
		t.Fatalf("expected no output, got %q", msg)
	case <-time.After(dur):
	}
}

// ── Complete sequences (emitted immediately) ────────────────────────────────

func TestInputParserPlainText(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("hello")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestInputParserControlChar(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte{0x02} // ctrl+b
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte{0x02}) {
		t.Fatalf("got %q, want %q", got, []byte{0x02})
	}
}

func TestInputParserCompleteCSI(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("\x1b[A") // arrow up
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[A")) {
		t.Fatalf("got %q, want %q", got, "\x1b[A")
	}
}

func TestInputParserCompleteSGRMouse(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("\x1b[<0;10;5M")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[<0;10;5M")) {
		t.Fatalf("got %q, want %q", got, "\x1b[<0;10;5M")
	}
}

func TestInputParserMultipleSequences(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	// Two CSI sequences in one read — emitted together as one batch.
	inputCh <- []byte("\x1b[A\x1b[B")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[A\x1b[B")) {
		t.Fatalf("got %q, want %q", got, "\x1b[A\x1b[B")
	}
}

func TestInputParserMixedTextAndCSI(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("hello\x1b[Aworld")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("hello\x1b[Aworld")) {
		t.Fatalf("got %q, want %q", got, "hello\x1b[Aworld")
	}
}

// ── Incomplete sequences (buffered, then completed) ─────────────────────────

func TestInputParserSplitCSI(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	// First read: ESC alone — held back (might be start of sequence).
	inputCh <- []byte{0x1b}
	expectNoOutput(t, outputCh, 5*time.Millisecond)

	// Second read: completes the CSI sequence.
	inputCh <- []byte("[A")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[A")) {
		t.Fatalf("got %q, want %q", got, "\x1b[A")
	}
}

func TestInputParserSplitSGRMouse(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("\x1b[<0;10")
	expectNoOutput(t, outputCh, 5*time.Millisecond)

	inputCh <- []byte(";5M")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[<0;10;5M")) {
		t.Fatalf("got %q, want %q", got, "\x1b[<0;10;5M")
	}
}

func TestInputParserSplitOSC(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("\x1b]0;tit")
	expectNoOutput(t, outputCh, 5*time.Millisecond)

	inputCh <- []byte("le\x07")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b]0;title\x07")) {
		t.Fatalf("got %q, want %q", got, "\x1b]0;title\x07")
	}
}

// ── Mixed complete + incomplete ─────────────────────────────────────────────

func TestInputParserTextThenIncompleteESC(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	// "hello" is complete, ESC is held back.
	inputCh <- []byte("hello\x1b")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("got %q, want %q", got, "hello")
	}

	// Complete the sequence.
	inputCh <- []byte("[A")
	got = recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[A")) {
		t.Fatalf("got %q, want %q", got, "\x1b[A")
	}
}

func TestInputParserCompleteCSIThenIncompleteESC(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	inputCh <- []byte("\x1b[A\x1b")
	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[A")) {
		t.Fatalf("got %q, want %q", got, "\x1b[A")
	}

	inputCh <- []byte("[B")
	got = recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[B")) {
		t.Fatalf("got %q, want %q", got, "\x1b[B")
	}
}

// ── Timeout flush (standalone ESC) ──────────────────────────────────────────

func TestInputParserTimeoutFlush(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()
	defer close(inputCh)

	// Send lone ESC — no followup data.
	inputCh <- []byte{0x1b}

	// Should flush after the timeout (10ms in test).
	got := recvOne(t, outputCh, 100*time.Millisecond)
	if !bytes.Equal(got, []byte{0x1b}) {
		t.Fatalf("got %q, want %q", got, []byte{0x1b})
	}
}

// ── Channel close flushes remaining ─────────────────────────────────────────

func TestInputParserCloseFlushes(t *testing.T) {
	inputCh, outputCh, parser := newTestParser()
	go parser.Run()

	// Send incomplete sequence then close.
	inputCh <- []byte("\x1b[")
	close(inputCh)

	got := recvOne(t, outputCh, time.Second)
	if !bytes.Equal(got, []byte("\x1b[")) {
		t.Fatalf("got %q, want %q", got, "\x1b[")
	}
}

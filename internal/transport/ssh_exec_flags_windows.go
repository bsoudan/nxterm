//go:build windows

package transport

import (
	"bufio"
	"encoding/base64"
	"io"
)

func proxyFlags() string { return "--base64" }

// base64ChunkSize is the maximum number of base64 characters per
// output line. 4096 stays well under the 16384 ConPTY width.
const base64ChunkSize = 4096

// wrapDataPhase wraps the post-auth reader and writer with base64
// encoding/decoding. On Windows, the remote proxy sends base64-
// encoded data to survive ConPTY byte-mangling.
func wrapDataPhase(r io.Reader, w io.Writer) (io.Reader, io.Writer) {
	return &base64Reader{scanner: newLineScanner(r)}, &base64Writer{w: w}
}

// --- base64 reader (ConPTY output → decoded JSON) ---

type base64Reader struct {
	scanner *bufio.Scanner
	buf     []byte // decoded bytes not yet delivered
}

func newLineScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1<<20), 16<<20)
	return s
}

func (r *base64Reader) Read(b []byte) (int, error) {
	// Deliver buffered decoded bytes first.
	if len(r.buf) > 0 {
		n := copy(b, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	// Accumulate base64 chunks until "." delimiter.
	var accum []byte
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if line == "" || line == "\r" {
			continue
		}
		if line == "." || line == ".\r" {
			if len(accum) == 0 {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(string(accum))
			if err != nil {
				accum = accum[:0]
				continue
			}
			// Re-add the \n the proxy stripped when reading lines.
			decoded = append(decoded, '\n')
			n := copy(b, decoded)
			if n < len(decoded) {
				r.buf = append(r.buf, decoded[n:]...)
			}
			return n, nil
		}
		// Strip trailing \r that ConPTY adds.
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		accum = append(accum, line...)
	}
	if err := r.scanner.Err(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

// --- base64 writer (JSON → ConPTY input encoded) ---

type base64Writer struct {
	w io.Writer
}

func (w *base64Writer) Write(b []byte) (int, error) {
	encoded := base64.StdEncoding.EncodeToString(b)
	for len(encoded) > 0 {
		chunk := encoded
		if len(chunk) > base64ChunkSize {
			chunk = encoded[:base64ChunkSize]
		}
		encoded = encoded[len(chunk):]
		// Use \r as line terminator for ENABLE_LINE_INPUT.
		if _, err := w.w.Write([]byte(chunk + "\r")); err != nil {
			return 0, err
		}
	}
	if _, err := w.w.Write([]byte(".\r")); err != nil {
		return 0, err
	}
	return len(b), nil
}

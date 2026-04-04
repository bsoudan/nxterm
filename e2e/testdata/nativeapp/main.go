// nativeapp is a test program that speaks the termd native protocol.
// It negotiates native mode via fd 3, then renders cell grids and
// responds to input and resize messages.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type screenCell struct {
	Char string `json:"c,omitempty"`
	Fg   string `json:"fg,omitempty"`
	Bg   string `json:"bg,omitempty"`
	A    uint8  `json:"a,omitempty"`
}

type renderMsg struct {
	Type      string       `json:"type"`
	Cells     [][]screenCell `json:"cells"`
	CursorRow uint16       `json:"cursor_row"`
	CursorCol uint16       `json:"cursor_col"`
}

type inputMsg struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type resizeMsg struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

var (
	width  = 80
	height = 24
	input  string
)

func main() {
	// Negotiate native mode via fd 3.
	fdStr := os.Getenv("TERMD_FD")
	if fdStr == "" {
		fmt.Fprintf(os.Stderr, "TERMD_FD not set\n")
		os.Exit(1)
	}
	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad TERMD_FD: %v\n", err)
		os.Exit(1)
	}
	nego := os.NewFile(uintptr(fd), "termd-negotiate")
	nego.Write([]byte("{\"mode\":\"native\"}\n"))
	nego.Close()

	// Render initial frame.
	render()

	// Read messages from stdin.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}

		switch env.Type {
		case "input":
			var msg inputMsg
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			input += string(decoded)
			render()
		case "resize":
			var msg resizeMsg
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			width = msg.Width
			height = msg.Height
			render()
		}
	}
}

func render() {
	cells := make([][]screenCell, height)
	for i := range cells {
		cells[i] = make([]screenCell, width)
		for j := range cells[i] {
			cells[i][j] = screenCell{Char: " "}
		}
	}

	// Row 0: "NATIVE" in green bold
	putString(cells, 0, 0, "NATIVE", "green", 1)

	// Row 1: dimensions
	dims := fmt.Sprintf("%dx%d", width, height)
	putString(cells, 1, 0, dims, "", 0)

	// Row 2: input echo
	if input != "" {
		putString(cells, 2, 0, "INPUT:"+input, "", 0)
	}

	msg := renderMsg{
		Type:      "render",
		Cells:     cells,
		CursorRow: 0,
		CursorCol: 0,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b = append(b, '\n')
	os.Stdout.Write(b)
}

func putString(cells [][]screenCell, row, col int, s, fg string, attrs uint8) {
	if row >= len(cells) {
		return
	}
	for i, ch := range s {
		c := col + i
		if c >= len(cells[row]) {
			break
		}
		cells[row][c] = screenCell{
			Char: string(ch),
			Fg:   fg,
			A:    attrs,
		}
	}
}

// native_scratchpad is a termd native app that provides a drawable canvas.
//
// Features:
//   - Move cursor with arrow keys
//   - Type characters at the cursor position in the selected color
//   - Click/drag with the mouse to paint cells
//   - Color palette on the right side — click to select a color
//
// Build:
//
//	go build -o native_scratchpad .
//
// Configure in server.toml:
//
//	[[programs]]
//	name = "scratchpad"
//	cmd = "/path/to/native_scratchpad"
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Protocol types matching termd's native protocol.

type screenCell struct {
	Char string `json:"c,omitempty"`
	Fg   string `json:"fg,omitempty"`
	Bg   string `json:"bg,omitempty"`
	A    uint8  `json:"a,omitempty"`
}

type renderMsg struct {
	Type      string         `json:"type"`
	Cells     [][]screenCell `json:"cells"`
	CursorRow uint16         `json:"cursor_row"`
	CursorCol uint16         `json:"cursor_col"`
	Modes     map[int]bool   `json:"modes,omitempty"`
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

// Palette colors.
var palette = []struct {
	name string // 3-char label
	spec string // termd color spec for fg/bg
}{
	{"WHT", "white"},
	{"RED", "red"},
	{"GRN", "green"},
	{"YEL", "yellow"},
	{"BLU", "blue"},
	{"MAG", "magenta"},
	{"CYN", "cyan"},
	{"BRD", "brightred"},
	{"BGN", "brightgreen"},
	{"BBL", "brightblue"},
	{"BMG", "brightmagenta"},
	{"BCN", "brightcyan"},
	{"GRY", "brightblack"},
	{"BLK", "black"},
}

const paletteWidth = 5 // columns reserved for palette + separator

// privateModeKey mirrors the server's mode key encoding (mode << 5).
func privateModeKey(mode int) int { return mode << 5 }

var (
	width     = 80
	height    = 24
	cursorRow = 0
	cursorCol = 0
	selColor  = 0 // index into palette
	canvas    [][]screenCell
)

func main() {
	fdStr := os.Getenv("TERMD_FD")
	if fdStr == "" {
		fmt.Fprintf(os.Stderr, "native_scratchpad: TERMD_FD not set (must be spawned by termd)\n")
		os.Exit(1)
	}
	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "native_scratchpad: bad TERMD_FD: %v\n", err)
		os.Exit(1)
	}
	nego := os.NewFile(uintptr(fd), "termd-negotiate")
	nego.Write([]byte("{\"mode\":\"native\"}\n"))
	nego.Close()

	initCanvas()
	render()

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
			handleInput(decoded)
		case "resize":
			var msg resizeMsg
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			width = msg.Width
			height = msg.Height
			growCanvas()
			render()
		}
	}
}

func initCanvas() {
	canvas = make([][]screenCell, height)
	for r := range canvas {
		canvas[r] = make([]screenCell, width)
		for c := range canvas[r] {
			canvas[r][c] = screenCell{Char: " "}
		}
	}
}

func growCanvas() {
	for len(canvas) < height {
		row := make([]screenCell, width)
		for c := range row {
			row[c] = screenCell{Char: " "}
		}
		canvas = append(canvas, row)
	}
	for r := range canvas {
		for len(canvas[r]) < width {
			canvas[r] = append(canvas[r], screenCell{Char: " "})
		}
	}
}

func handleInput(data []byte) {
	i := 0
	for i < len(data) {
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == '[' {
			if i+2 < len(data) && data[i+2] == '<' {
				// SGR mouse: ESC [ < btn ; col ; row M/m
				n := handleSGRMouse(data[i:])
				if n > 0 {
					i += n
					continue
				}
			}
			// Arrow keys: ESC [ A/B/C/D
			if i+2 < len(data) {
				switch data[i+2] {
				case 'A': // up
					if cursorRow > 0 {
						cursorRow--
					}
					render()
					i += 3
					continue
				case 'B': // down
					if cursorRow < height-1 {
						cursorRow++
					}
					render()
					i += 3
					continue
				case 'C': // right
					if cursorCol < cw()-1 {
						cursorCol++
					}
					render()
					i += 3
					continue
				case 'D': // left
					if cursorCol > 0 {
						cursorCol--
					}
					render()
					i += 3
					continue
				}
			}
			i += 2
			continue
		}

		if data[i] == 0x1b {
			i++
			continue
		}

		// Printable character — type at cursor.
		ch := data[i]
		if ch >= 0x20 && ch < 0x7f {
			if cursorRow < len(canvas) && cursorCol < cw() {
				canvas[cursorRow][cursorCol] = screenCell{
					Char: string(ch),
					Fg:   palette[selColor].spec,
				}
				cursorCol++
				if cursorCol >= cw() {
					cursorCol = cw() - 1
				}
			}
			render()
		}
		i++
	}
}

// handleSGRMouse parses ESC [ < btn ; col ; row M/m and handles paint/palette.
// Returns bytes consumed, or 0 on failure.
func handleSGRMouse(data []byte) int {
	if len(data) < 9 {
		return 0
	}
	end := -1
	for j := 3; j < len(data); j++ {
		if data[j] == 'M' || data[j] == 'm' {
			end = j + 1
			break
		}
		if data[j] != ';' && (data[j] < '0' || data[j] > '9') {
			return 0
		}
	}
	if end < 0 {
		return 0
	}

	terminator := data[end-1]
	parts := strings.Split(string(data[3:end-1]), ";")
	if len(parts) != 3 {
		return 0
	}
	btn, e1 := strconv.Atoi(parts[0])
	col, e2 := strconv.Atoi(parts[1])
	row, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return 0
	}
	col-- // 1-based → 0-based
	row--

	isPress := terminator == 'M'
	isDrag := btn&32 != 0
	leftButton := (btn & 3) == 0

	if leftButton && (isPress || isDrag) {
		paint(row, col)
	}

	return end
}

func paint(row, col int) {
	// Palette click.
	palStart := width - paletteWidth + 1 // after separator
	if col >= palStart {
		if row >= 0 && row < len(palette) {
			selColor = row
		}
		render()
		return
	}

	// Canvas paint.
	if row >= 0 && row < len(canvas) && col >= 0 && col < cw() {
		canvas[row][col] = screenCell{
			Char: "█",
			Fg:   palette[selColor].spec,
		}
		cursorRow = row
		cursorCol = col
	}
	render()
}

// cw returns the usable canvas width (total width minus palette).
func cw() int {
	w := width - paletteWidth
	if w < 1 {
		return 1
	}
	return w
}

func render() {
	cells := make([][]screenCell, height)
	canvasW := cw()

	for r := 0; r < height; r++ {
		cells[r] = make([]screenCell, width)

		// Canvas area.
		for c := 0; c < canvasW && c < width; c++ {
			if r < len(canvas) && c < len(canvas[r]) {
				cells[r][c] = canvas[r][c]
			} else {
				cells[r][c] = screenCell{Char: " "}
			}
		}

		// Separator column.
		sepCol := canvasW
		if sepCol < width {
			cells[r][sepCol] = screenCell{Char: "│", Fg: "brightblack"}
		}

		// Palette entries.
		palStart := canvasW + 1
		if r < len(palette) {
			label := palette[r].name
			bg := palette[r].spec
			fg := "white"
			if r == selColor {
				fg = "black"
			}

			for c := 0; c < paletteWidth-1 && c < len(label); c++ {
				if palStart+c < width {
					cells[r][palStart+c] = screenCell{
						Char: string(label[c]),
						Fg:   fg,
						Bg:   bg,
					}
				}
			}
			for c := len(label); c < paletteWidth-1; c++ {
				if palStart+c < width {
					cells[r][palStart+c] = screenCell{Char: " ", Bg: bg}
				}
			}
			// Selection indicator.
			if r == selColor {
				if palStart+paletteWidth-1 < width {
					cells[r][palStart+paletteWidth-1] = screenCell{
						Char: "◄",
						Fg:   palette[r].spec,
					}
				}
			}
		} else {
			for c := palStart; c < width; c++ {
				cells[r][c] = screenCell{Char: " "}
			}
		}
	}

	msg := renderMsg{
		Type:      "render",
		Cells:     cells,
		CursorRow: uint16(cursorRow),
		CursorCol: uint16(cursorCol),
		Modes: map[int]bool{
			privateModeKey(1003): true, // mouse any-event tracking
			privateModeKey(1006): true, // SGR mouse encoding
		},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b = append(b, '\n')
	os.Stdout.Write(b)
}

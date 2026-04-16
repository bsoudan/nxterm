# pkg/te

Full VT100/xterm terminal emulator library. Parses escape sequences, maintains screen state with colors/attributes, provides scrollback history.

## Architecture

Three-layer design:
1. **Stream** — parses raw bytes into terminal commands
2. **Screen** — maintains cell grid, cursor, modes, attributes
3. **HistoryScreen** — wraps Screen with scrollback buffer and paging

## Stream (`stream.go`)

State machine parser with 11 states: `ground`, `escape`, `CSI`, `OSC`, `DCS`, `APC`, `PM`, `SOS`, `charset`, `escape_percent`, `escape_space`.

- Input: `Feed(string)` or `FeedBytes([]byte)` (via `ByteStream`)
- Output: dispatches to `EventHandler` interface (98 methods)
- Handles UTF-8 multibyte, 7-bit and 8-bit control forms

The `EventHandler` interface decouples parsing from state management. Screen implements it.

## Screen (`screen.go`, ~2900 lines)

Core terminal state:
- `Buffer [][]Cell` — grid of cells
- `Cursor` — position + current attributes
- `Mode map[int]struct{}` — active terminal modes
- `Margins` — scroll region (top/bottom, left/right)
- `Dirty map[int]struct{}` — modified lines for efficient re-rendering

### Cell Model
- `Cell{Data string, Attr Attr}` — Data is a grapheme cluster
- Wide characters: stored in cell[col], cell[col+1] is empty marker
- Combining characters: normalized via `norm.NFC` into preceding cell
- Uses `uniseg` for correct grapheme cluster handling

### Attributes
```go
type Attr struct {
    Fg, Bg  Color           // ColorDefault, ANSI16, ANSI256, TrueColor
    Bold, Faint, Italics, Underline, Strikethrough,
    Reverse, Blink, Conceal, Protected bool
}
```

### Key Operations
- **Drawing**: `Draw(data)` — renders text respecting wide chars, combining marks, autowrap, insert mode
- **Cursor**: `CursorPosition()`, `CursorUp/Down/Forward/Back()`, save/restore (ANSI + DEC)
- **Erasing**: `EraseInDisplay()`, `EraseInLine()`, `EraseCharacters()`
- **Scrolling**: `Index()`, `ReverseIndex()`, `ScrollUp()`, `ScrollDown()`
- **Modes**: `SetMode()`, `ResetMode()` — handles ANSI + DEC private modes
- **SGR**: `SelectGraphicRendition()` — full color and attribute support
- **Serialization**: `MarshalState()` / `UnmarshalState()` for live upgrade state transfer

### Alternate Screen Buffer
`altBuffer` with separate savepoints. Toggle via `SetMode(ModeAltBufCursor)`.

## HistoryScreen (`history_screen.go`)

Wraps Screen with scrollback:
- `history.Top` (historyDeque) — lines scrolled off top
- `history.Bottom` (historyDeque) — lines from paging back up
- `Position` / `Size` — paging state (Position == Size means live view)
- Ratio-based paging: `PrevPage()` / `NextPage()` scroll by `ceil(Lines * Ratio)` lines
- `PrependHistory()` — sync older history from server on client join

`beforeEvent()` auto-scrolls to live view for non-paging terminal events.

## Supporting Files

| File | Purpose |
|------|---------|
| `types.go` | Cell, Cursor, Color, Attr, ColorMode |
| `modes.go` | Terminal mode constants (DECTCEM, DECAWM, etc.) |
| `byte_stream.go` | UTF-8 byte buffering wrapper for Stream |
| `state.go` | Serializable ScreenState snapshot |
| `charsets.go` | VT100, Latin-1, IBM PC character set translations |
| `graphics.go` | ANSI/Aixterm color name maps, 256-color palette |
| `control.go` | ASCII control code constants |
| `escape.go` | Escape sequence name constants |
| `diff_screen.go` | Dirty-tracking screen wrapper |
| `svg_exporter.go` | SVG rendering export |
| `debug_screen.go` | Debugging/inspection utilities |

## Testing

Extensively tested with esctest2 test suite ports (~90 test files) and pyte compatibility tests.

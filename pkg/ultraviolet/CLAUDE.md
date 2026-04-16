# pkg/ultraviolet

Low-level terminal emulation and UI rendering library. Vendored/forked from `charmbracelet/x/term`.

## Purpose

Provides raw terminal I/O primitives used by the bubbletea fork:
- Terminal input event decoding (keyboard, mouse)
- Screen buffer management (cell-based rendering)
- Platform-specific terminal control (Unix, Windows, BSD)

## Key Concepts

- **Cell** — individual terminal cell with character, colors, and attributes
- **Screen/Buffer** — manages grid of cells for rendering
- **Decoder** — parses raw terminal input into typed events (key presses, mouse events)
- **Renderer** — optimized screen update output (hashmap diffing, hardware scroll)

## Platform Support

Platform-specific files for Unix, Windows, BSD, Darwin covering terminal mode setup, I/O polling, and console control.

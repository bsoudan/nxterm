# internal/nxlog

Custom `slog.Handler` for formatted log output with UI integration.

## Key Type

`Handler` implements `slog.Handler`:
- Formats entries as `HH:MM:SS.mmm level message key=value ...`
- Writes to `io.Writer` (stderr)
- Optionally appends to a `LogRingBuffer` via callback
- Throttled (100ms) notification function triggers UI re-renders on new log entries

Used by both server and frontend processes.

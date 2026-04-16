# e2e

End-to-end test suite for the full nxterm system.

## Pattern

Tests use `nxtest.T` wrapper: start server → start frontend in PTY → exercise scenario → assert on virtual screen.

All tests run in parallel (`t.Parallel()`).

## Test Files

| File | Coverage |
|------|----------|
| `session_test.go` | Session management |
| `stress_test.go` | Load testing (30s/120s) |
| `upgrade_test.go` / `client_upgrade_test.go` | Live upgrade |
| `render_test.go` | Rendering correctness |
| `scrollback_test.go` | Terminal history |
| `input_test.go` | Input handling |
| `keybind_test.go` | Keybinding |
| `tab_test.go` | Tab management |
| `program_test.go` | Program spawning |
| `overlay_test.go` | UI overlays |
| `transport_test.go` | Transport mechanisms |
| `connect_test.go` / `reconnect_test.go` | Connection lifecycle |
| `multisession_test.go` | Multiple sessions |
| `termctl_test.go` | nxtermctl admin tool |
| `native_test.go` | Native apps |

## Running

```bash
make test                    # all e2e tests
go test -v -timeout 120s -run TestName ./e2e  # single test
```

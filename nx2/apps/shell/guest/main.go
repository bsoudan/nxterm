// Command shell-guest is the nx2 shell app's client-side WASM half: the terminal
// multiplexer UI. It mirrors one or more child terminals (each a pkg/te
// HistoryScreen seeded by the shell companion) and renders the active one into the
// host's cell grid. It is a wasip1 reactor built -buildmode=c-shared; the real
// entry points are the //go:wasmexport functions in shell_wasm.go.
package main

func main() {}

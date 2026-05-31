// Command guest is the nx2 default terminal app's client-side WASM module.
//
// It is built as a wasip1 reactor (-buildmode=c-shared): the host instantiates
// it and calls its exported ABI (configure/feed/render/resize); the guest calls
// back into the host (submit_cells/read_input). The real logic lives in the
// wasip1-tagged file; this stub keeps the package building for the host
// toolchain too. See nx2/wit/host-surface.wit for the contract.
package main

func main() {}

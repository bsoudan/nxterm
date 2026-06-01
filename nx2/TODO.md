# nx2 TODO

## M4: Component Model promotion

**Status: BLOCKED** on ecosystem maturity.

The north-star ABI is described in WIT (`nx2/wit/`), but the current guest/host
use core WebAssembly (`(ptr,len)` batched buffers) because Component Model
hosting is immature outside Rust/JS:

- **wazero** (our Go host runtime): no Component Model support at all.
- **wasmtime-go**: early preview.
- **componentize-dotnet** (our C# host): early preview.

Migrate to real Component Model when at least one of these runtimes ships stable
CM support. Until then there is nothing meaningful or testable to do here.

**Re-check periodically** (last checked May 2026).

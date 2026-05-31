// Package wasmhost runs an nx2 client-side WASM app over wazero and bridges the
// core-wasm (ptr,len) ABI that realizes nx2/wit/host-surface.wit.
//
// The guest is a wasip1 reactor (-buildmode=c-shared). The host provides module
// "nx2" with submit_cells/read_input; the guest exports alloc/configure/feed/
// render/resize. Byte payloads cross the boundary through the guest's linear
// memory: host->guest via the guest's alloc + Memory().Write, guest->host by the
// guest passing a pointer the host reads during the synchronous call.
package wasmhost

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"nxtermd/nx2/internal/cellgrid"
)

// Surface is the host capability set a guest may call (host-surface.wit `host`).
type Surface interface {
	// SubmitCells receives one decoded cell-grid frame per render().
	SubmitCells(*cellgrid.Frame)
	// ReadInput fills dst with pending user input and returns the count.
	ReadInput(dst []byte) int
}

// Instance is a running guest bound to a Surface.
type Instance struct {
	ctx context.Context
	rt  wazero.Runtime
	mod api.Module

	alloc, configure, feed, render, resize api.Function
}

// New instantiates the guest WASM module and wires the host functions.
func New(ctx context.Context, wasm []byte, surf Surface) (*Instance, error) {
	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	_, err := rt.NewHostModuleBuilder("nx2").
		NewFunctionBuilder().
		WithFunc(func(_ context.Context, m api.Module, ptr, n int32) {
			if n <= 0 {
				return
			}
			buf, ok := m.Memory().Read(uint32(ptr), uint32(n))
			if !ok {
				return
			}
			f, err := cellgrid.Decode(buf)
			if err != nil {
				return
			}
			surf.SubmitCells(f)
		}).Export("submit_cells").
		NewFunctionBuilder().
		WithFunc(func(_ context.Context, m api.Module, ptr, capacity int32) int32 {
			if capacity <= 0 {
				return 0
			}
			tmp := make([]byte, capacity)
			n := surf.ReadInput(tmp)
			if n <= 0 {
				return 0
			}
			if !m.Memory().Write(uint32(ptr), tmp[:n]) {
				return 0
			}
			return int32(n)
		}).Export("read_input").
		Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("host module: %w", err)
	}

	// Go reactor modules (-buildmode=c-shared) init via "_initialize", not the
	// default "_start" wazero would otherwise require.
	cfg := wazero.NewModuleConfig().WithStartFunctions("_initialize")
	mod, err := rt.InstantiateWithConfig(ctx, wasm, cfg)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("instantiate guest: %w", err)
	}

	inst := &Instance{
		ctx:       ctx,
		rt:        rt,
		mod:       mod,
		alloc:     mod.ExportedFunction("alloc"),
		configure: mod.ExportedFunction("configure"),
		feed:      mod.ExportedFunction("feed"),
		render:    mod.ExportedFunction("render"),
		resize:    mod.ExportedFunction("resize"),
	}
	for name, f := range map[string]api.Function{
		"alloc": inst.alloc, "configure": inst.configure, "feed": inst.feed,
		"render": inst.render, "resize": inst.resize,
	} {
		if f == nil {
			rt.Close(ctx)
			return nil, fmt.Errorf("guest missing export %q", name)
		}
	}
	return inst, nil
}

// Configure (re)initializes the surface dimensions.
func (i *Instance) Configure(cols, rows int) error {
	_, err := i.configure.Call(i.ctx, api.EncodeI32(int32(cols)), api.EncodeI32(int32(rows)))
	return err
}

// Resize informs the guest of a new surface size.
func (i *Instance) Resize(cols, rows int) error {
	_, err := i.resize.Call(i.ctx, api.EncodeI32(int32(cols)), api.EncodeI32(int32(rows)))
	return err
}

// Feed delivers opaque companion bytes (PTY output) to the guest.
func (i *Instance) Feed(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	res, err := i.alloc.Call(i.ctx, api.EncodeI32(int32(len(data))))
	if err != nil {
		return fmt.Errorf("alloc: %w", err)
	}
	ptr := api.DecodeI32(res[0])
	if !i.mod.Memory().Write(uint32(ptr), data) {
		return fmt.Errorf("feed: memory write out of range")
	}
	_, err = i.feed.Call(i.ctx, api.EncodeI32(ptr), api.EncodeI32(int32(len(data))))
	return err
}

// Render asks the guest to produce one frame; it calls Surface.SubmitCells once.
func (i *Instance) Render() error {
	_, err := i.render.Call(i.ctx)
	return err
}

// Close tears down the runtime.
func (i *Instance) Close() error { return i.rt.Close(i.ctx) }

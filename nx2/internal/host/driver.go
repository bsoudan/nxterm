package host

import (
	"context"

	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/wasmhost"
	"nxtermd/nx2/internal/wire"
)

// Display paints frames produced by the guest. A terminal host writes ANSI
// (see RenderANSI); a GUI host draws the cell grid with a glyph atlas.
//
// Render is always invoked from inside a guest call (feed/render/input), which
// the wasmhost serializes, so a Display never sees concurrent Render calls and
// needs no locking of its own.
type Display interface {
	Render(*cellgrid.Frame)
}

// Driver is the reusable nx2 host core: it runs one app on one connection and
// bridges the guest to a Display (output) and to user input. Both nx2-host-tui
// and a native GUI host are thin wrappers over it. The connection's read side is
// owned by the driver (Open's setup reads, then Run reads); callers must not read
// conn concurrently.
type Driver struct {
	conn   *wire.Conn
	inst   *wasmhost.Instance
	sendCh chan []byte
	closed chan struct{}
}

type driverSurface struct {
	d       *Driver
	display Display
}

func (s *driverSurface) SubmitCells(f *cellgrid.Frame) { s.display.Render(f) }

// ClipboardSet forwards an app's OSC 52 clipboard copy to the Display if it
// implements the optional ClipboardSet capability; otherwise it is dropped.
func (s *driverSurface) ClipboardSet(b []byte) {
	if cs, ok := s.display.(interface{ ClipboardSet([]byte) }); ok {
		cs.ClipboardSet(b)
	}
}

func (s *driverSurface) ChannelSend(b []byte) {
	select {
	case s.d.sendCh <- b:
	case <-s.d.closed:
	}
}

// Open performs the connect flow — resolve app name -> hash, fetch+cache the
// guest module, instantiate it, configure the surface, and select the app — and
// returns a Driver ready to Run.
func Open(ctx context.Context, conn *wire.Conn, cacheDir, app, session string, cols, rows int, display Display) (*Driver, error) {
	hash, err := Resolve(conn, app)
	if err != nil {
		return nil, err
	}
	cache, err := capsule.NewCache(cacheDir)
	if err != nil {
		return nil, err
	}
	wasmBytes, err := Fetch(conn, cache, hash)
	if err != nil {
		return nil, err
	}

	d := &Driver{conn: conn, sendCh: make(chan []byte, 256), closed: make(chan struct{})}
	inst, err := wasmhost.New(ctx, wasmBytes, &driverSurface{d: d, display: display})
	if err != nil {
		return nil, err
	}
	d.inst = inst
	if err := inst.Configure(cols, rows); err != nil {
		inst.Close()
		return nil, err
	}

	// Relay guest data-plane output (e.g. wrapped input) to the broker, off the
	// wasm call path so guest calls never block on a network write.
	go func() {
		for {
			select {
			case b := <-d.sendCh:
				if err := conn.Write(wire.Data, b); err != nil {
					return
				}
			case <-d.closed:
				return
			}
		}
	}()

	sel, err := control.Marshal(control.TypeSelectApp, control.SelectApp{App: app, Session: session})
	if err != nil {
		d.Close()
		return nil, err
	}
	if err := conn.Write(wire.Control, sel); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Input delivers user-input bytes to the guest (which relays them to the companion).
func (d *Driver) Input(b []byte) error { return d.inst.Input(b) }

// Resize informs the guest of a new surface size.
func (d *Driver) Resize(cols, rows int) error { return d.inst.Resize(cols, rows) }

// Run reads the data plane, feeds the guest, and renders until the connection
// closes. It blocks; call it on its own goroutine or as the host's main loop.
func (d *Driver) Run() error {
	for {
		t, payload, err := d.conn.Read()
		if err != nil {
			return err
		}
		if t == wire.Data {
			if err := d.inst.Feed(payload); err != nil {
				return err
			}
			if err := d.inst.Render(); err != nil {
				return err
			}
		}
	}
}

// Close tears down the driver.
func (d *Driver) Close() error {
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	if d.inst != nil {
		return d.inst.Close()
	}
	return nil
}

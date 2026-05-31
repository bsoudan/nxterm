package broker

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/wire"
)

func echoBin(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", ".local", "bin", "nx2-echo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("nx2-echo not built (%v); run: make build-nx2-echo", err)
	}
	return p
}

// TestRelayEchoesOpaqueBytes is the S2 validator: select an app, then push random
// binary (including bytes that resemble frame headers) and require it back
// byte-exact — proving the broker relays the data plane blind.
func TestRelayEchoesOpaqueBytes(t *testing.T) {
	b := New()
	b.Register(App{Name: "echo", Command: echoBin(t)})

	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	defer cli.Close()
	_ = cli.SetDeadline(time.Now().Add(10 * time.Second))
	host := wire.NewConn(cli)

	if got := selectApp(t, host, "echo"); got.Error {
		t.Fatalf("select failed: %s", got.Message)
	}

	want := make([]byte, 64*1024)
	if _, err := rand.Read(want); err != nil {
		t.Fatal(err)
	}
	if err := host.Write(wire.Data, want); err != nil {
		t.Fatal(err)
	}

	var echoed bytes.Buffer
	for echoed.Len() < len(want) {
		typ, payload, err := host.Read()
		if err != nil {
			t.Fatalf("read echo (%d/%d): %v", echoed.Len(), len(want), err)
		}
		if typ == wire.Data {
			echoed.Write(payload)
		}
	}
	if !bytes.Equal(echoed.Bytes(), want) {
		t.Fatalf("echo mismatch: got %d bytes, want %d", echoed.Len(), len(want))
	}
}

func TestSelectUnknownAppErrors(t *testing.T) {
	b := New()
	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	defer cli.Close()
	_ = cli.SetDeadline(time.Now().Add(5 * time.Second))
	host := wire.NewConn(cli)

	if got := selectApp(t, host, "does-not-exist"); !got.Error {
		t.Fatal("expected error selecting unknown app")
	}
}

func selectApp(t *testing.T, host *wire.Conn, name string) control.Selected {
	t.Helper()
	sel, err := control.Marshal(control.TypeSelectApp, control.SelectApp{App: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Write(wire.Control, sel); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := host.Read()
	if err != nil {
		t.Fatalf("read selected: %v", err)
	}
	if typ != wire.Control {
		t.Fatalf("want control frame, got %d", typ)
	}
	tt, raw, err := control.Parse(payload)
	if err != nil || tt != control.TypeSelected {
		t.Fatalf("parse selected: %v type=%s", err, tt)
	}
	var got control.Selected
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

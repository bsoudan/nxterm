package host

import (
	"bytes"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/wire"
)

func dialBroker(t *testing.T, b *broker.Broker) *wire.Conn {
	t.Helper()
	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	t.Cleanup(func() { cli.Close() })
	_ = cli.SetDeadline(time.Now().Add(10 * time.Second))
	return wire.NewConn(cli)
}

func TestFetchDownloadsAndCaches(t *testing.T) {
	blob := make([]byte, 200*1024) // spans multiple chunks
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	b := broker.New()
	app := b.Register(broker.App{Name: "term", GuestWASM: blob})

	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Fetch(dialBroker(t, b), cache, app.Hash)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("blob mismatch: got %d bytes, want %d", len(got), len(blob))
	}
	if !cache.Has(app.Hash) {
		t.Fatal("blob not cached after fetch")
	}
}

func TestFetchServedFromCacheWithoutBroker(t *testing.T) {
	blob := []byte("hello wasm")
	hash := capsule.Hash(blob)
	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(hash, blob); err != nil {
		t.Fatal(err)
	}
	// The broker has no such blob; a cache hit must not contact it.
	got, err := Fetch(dialBroker(t, broker.New()), cache, hash)
	if err != nil {
		t.Fatalf("fetch from cache: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("cache blob mismatch")
	}
}

func TestFetchUnknownHashErrors(t *testing.T) {
	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(dialBroker(t, broker.New()), cache, "sha256:deadbeef"); err == nil {
		t.Fatal("expected error for unknown hash")
	}
}

func TestCorruptCacheEntryRefetches(t *testing.T) {
	blob := make([]byte, 4096)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	b := broker.New()
	app := b.Register(broker.App{Name: "term", GuestWASM: blob})

	dir := t.TempDir()
	cache, err := capsule.NewCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Plant a corrupt entry under the correct filename (bypassing Put's verify).
	corrupt := filepath.Join(dir, strings.ReplaceAll(app.Hash, ":", "_"))
	if err := os.WriteFile(corrupt, []byte("garbage not matching hash"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Fetch(dialBroker(t, b), cache, app.Hash)
	if err != nil {
		t.Fatalf("fetch after corruption: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("did not recover correct blob after corruption")
	}
	if !cache.Has(app.Hash) {
		t.Fatal("cache not repaired")
	}
}

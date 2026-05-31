// Package host holds nx2 client-side ("native shell") logic shared by host
// binaries: fetching content-addressed app modules and (later) driving surfaces.
package host

import (
	"bytes"
	"encoding/json"
	"fmt"

	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/wire"
)

// Fetch returns the WASM module for hash. A valid cache entry is used directly;
// otherwise the module is fetched from the broker over conn, verified, and
// cached. It reads control frames from conn directly, so call it during setup
// before starting a concurrent read loop on conn.
func Fetch(conn *wire.Conn, cache *capsule.Cache, hash string) ([]byte, error) {
	if b, err := cache.Get(hash); err == nil {
		return b, nil
	}

	req, err := control.Marshal(control.TypeFetch, control.Fetch{Hash: hash})
	if err != nil {
		return nil, err
	}
	if err := conn.Write(wire.Control, req); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for {
		t, payload, err := conn.Read()
		if err != nil {
			return nil, err
		}
		if t != wire.Control {
			continue
		}
		typ, raw, err := control.Parse(payload)
		if err != nil {
			return nil, err
		}
		if typ != control.TypeChunk {
			continue
		}
		var ch control.Chunk
		if err := json.Unmarshal(raw, &ch); err != nil {
			return nil, err
		}
		if ch.Error {
			return nil, fmt.Errorf("fetch %s: %s", hash, ch.Message)
		}
		buf.Write(ch.Data)
		if ch.Done {
			break
		}
	}

	data := buf.Bytes()
	if err := cache.Put(hash, data); err != nil {
		return nil, fmt.Errorf("cache put: %w", err)
	}
	return data, nil
}

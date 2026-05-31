// Package capsule provides content-addressed storage for nx2 app modules
// (client-side WASM). An app is named by the hash of its bytes; the host fetches
// a blob once over the control side-channel and caches it by hash. Integrity is
// verified on every read and write, so a corrupt/tampered cache entry is treated
// as a miss and refetched.
package capsule

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hash returns the canonical content hash "sha256:<hex>" of b.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Verify reports whether b hashes to hash.
func Verify(hash string, b []byte) bool { return Hash(b) == hash }

// ErrMismatch is returned when bytes do not match their claimed hash.
var ErrMismatch = errors.New("capsule: content does not match hash")

// Cache is a filesystem store of blobs keyed by hash.
type Cache struct {
	dir string
}

// NewCache opens (creating if needed) a cache rooted at dir.
func NewCache(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) path(hash string) string {
	// hash is "sha256:<hex>"; ':' is unsafe in some filesystems.
	return filepath.Join(c.dir, strings.ReplaceAll(hash, ":", "_"))
}

// Has reports whether a (valid) entry for hash exists.
func (c *Cache) Has(hash string) bool {
	_, err := c.Get(hash)
	return err == nil
}

// Get returns the cached blob for hash, or an error if absent or corrupt. A
// corrupt entry is removed so the next fetch repairs it.
func (c *Cache) Get(hash string) ([]byte, error) {
	b, err := os.ReadFile(c.path(hash))
	if err != nil {
		return nil, err
	}
	if !Verify(hash, b) {
		_ = os.Remove(c.path(hash))
		return nil, ErrMismatch
	}
	return b, nil
}

// Put stores b under hash after verifying it. The write is atomic (temp+rename).
func (c *Cache) Put(hash string, b []byte) error {
	if !Verify(hash, b) {
		return fmt.Errorf("%w: refusing to cache", ErrMismatch)
	}
	final := c.path(hash)
	tmp, err := os.CreateTemp(c.dir, "tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, final)
}

// Store is an in-memory server-side blob source keyed by hash.
type Store struct {
	blobs map[string][]byte
}

// NewStore returns an empty Store.
func NewStore() *Store { return &Store{blobs: make(map[string][]byte)} }

// Add stores b and returns its hash.
func (s *Store) Add(b []byte) string {
	h := Hash(b)
	s.blobs[h] = b
	return h
}

// Get returns the blob for hash.
func (s *Store) Get(hash string) ([]byte, bool) {
	b, ok := s.blobs[hash]
	return b, ok
}

// Command files-companion is the file-browser app's server-side half: it does
// the OS work (readdir) and speaks fproto to the guest. It owns no PTY — proof
// that an nx2 companion is just "the part of the app that needs the server's
// resources", not necessarily a terminal.
//
// Usage: files-companion [root]   (defaults to the current directory)
package main

import (
	"os"
	"path/filepath"
	"sort"

	"nxtermd/nx2/apps/files/fproto"
)

func main() {
	cwd := "."
	if len(os.Args) > 1 {
		cwd = os.Args[1]
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}

	send(listing(cwd))

	var dec fproto.Decoder
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			dec.Push(buf[:n])
			for {
				m, ok, e := dec.Next()
				if e != nil || !ok {
					break
				}
				if m.Type == fproto.TypeChdir {
					cwd = filepath.Clean(filepath.Join(cwd, m.Path))
					send(listing(cwd))
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func listing(dir string) fproto.Msg {
	entries := []fproto.Entry{{Name: "..", Dir: true}}
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		entries = append(entries, fproto.Entry{Name: de.Name(), Dir: de.IsDir()})
	}
	sort.SliceStable(entries[1:], func(i, j int) bool {
		return entries[1+i].Name < entries[1+j].Name
	})
	return fproto.Msg{Type: fproto.TypeListing, Path: dir, Entries: entries}
}

var wbuf []byte

func send(m fproto.Msg) {
	var err error
	wbuf, err = fproto.Encode(m, wbuf[:0])
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(wbuf)
}

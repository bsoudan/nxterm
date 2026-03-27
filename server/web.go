package main

import (
	"embed"
	"io/fs"
	"net/http"

	"termd/transport"
)

//go:embed webstatic
var webStaticFS embed.FS

func init() {
	sub, err := fs.Sub(webStaticFS, "webstatic")
	if err != nil {
		panic(err)
	}
	transport.WSFallback = http.FileServer(http.FS(sub))
}

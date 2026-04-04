package main

import (
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
)

func startPprof(addr string) {
	// "true" or empty value means use default address.
	if addr == "true" {
		addr = "localhost:6060"
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = "localhost:" + addr
	}

	// Enable mutex and block profiling (off by default).
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(10000) // 10µs granularity

	go func() {
		slog.Info("pprof server listening", "addr", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Error("pprof server failed", "err", err)
		}
	}()
}

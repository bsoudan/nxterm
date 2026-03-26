.PHONY: all build-server build-frontend build-termctl test test-e2e clean

all: build-server build-frontend build-termctl

build-server:
	cd server && zig build

build-frontend:
	cd frontend && go build -o termd-frontend .

build-termctl:
	cd termctl && go build -o termctl .

test: test-e2e

test-e2e: build-server build-frontend build-termctl
	cd e2e && go test -v -timeout 120s

clean:
	rm -rf server/zig-out server/.zig-cache
	cd frontend && go clean ./...
	cd termctl && go clean ./...

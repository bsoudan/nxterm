.PHONY: all build-server build-frontend test test-e2e clean

all: build-server build-frontend

build-server:
	cd server && zig build

build-frontend:
	cd frontend && go build ./...

test: test-e2e

test-e2e: build-server build-frontend
	cd frontend && go test ./... -v -timeout 60s

clean:
	rm -rf server/zig-out server/.zig-cache
	cd frontend && go clean ./...

.PHONY: all build-server build-frontend test test-e2e clean

all: build-server build-frontend

build-server:
	cd server && zig build

build-frontend:
	cd frontend && go build -o termd-frontend .

test: test-e2e

test-e2e: build-server build-frontend
	cd e2e && go test -v -timeout 120s

clean:
	rm -rf server/zig-out server/.zig-cache
	cd frontend && go clean ./...

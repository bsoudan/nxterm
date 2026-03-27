.PHONY: all build-server build-frontend build-frontend-windows build-termctl check-windows test test-e2e clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

all: build-server build-frontend build-frontend-windows build-termctl

build-server:
	cd server && go build -ldflags "-X main.version=$(VERSION)" -o ../.local/bin/termd .

build-frontend:
	cd frontend && go build -ldflags "-X main.version=$(VERSION)" -o ../.local/bin/termd-frontend .

build-frontend-windows:
	cd frontend && GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o ../.local/bin/termd-frontend.exe .

build-termctl:
	cd termctl && go build -ldflags "-X main.version=$(VERSION)" -o ../.local/bin/termctl .

check-windows:
	cd frontend && GOOS=windows GOARCH=amd64 go build -o /dev/null .
	cd transport && GOOS=windows GOARCH=amd64 go build -o /dev/null .

test: test-e2e

test-e2e: all
	cd e2e && PATH="$(CURDIR)/.local/bin:$(PATH)" go test -v -timeout 120s

clean:
	rm -rf .local/bin
	cd server && go clean ./...
	cd frontend && go clean ./...
	cd termctl && go clean ./...

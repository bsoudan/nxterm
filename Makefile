.PHONY: all build-server build-frontend build-termctl check-windows test test-e2e clean

all: build-server build-frontend build-termctl

build-server:
	cd server && go build -o ../.local/bin/termd .

build-frontend:
	cd frontend && go build -o ../.local/bin/termd-frontend .

build-termctl:
	cd termctl && go build -o ../.local/bin/termctl .

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

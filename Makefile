.PHONY: all build-server changelog build-tui build-tui-windows build-wasm build-termctl check-windows check-wasm test test-e2e clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-g[0-9a-f]*//;s/-dirty/*/' || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
ifndef RELEASE
  GCFLAGS := -gcflags "all=-N -l"
endif

all: build-server build-tui build-tui-windows build-termctl

build-server: build-wasm
	cd server && go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o ../.local/bin/termd .

changelog:
	@if git diff --quiet HEAD 2>/dev/null && test -z "$$(git ls-files --others --exclude-standard)"; then \
		:; \
	else \
		printf '%18s %s\n' "$$(git describe --tags --always --dirty='*' 2>/dev/null):" "$$(git status --short | tr '\n' ' ')" > frontend/changelog.txt; \
	fi
	git log --format='%H %s' -100 | while read hash rest; do \
		ver=$$(git describe --tags --always $$hash 2>/dev/null); \
		printf '%18s %s\n' "$$ver:" "$$rest"; \
	done >> frontend/changelog.txt

build-tui: changelog
	cd frontend && go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o ../.local/bin/termd-tui .

build-tui-windows: changelog
	cd frontend && GOOS=windows GOARCH=amd64 go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o ../.local/bin/termd-tui.exe .

build-wasm: changelog
	rm -f server/webstatic/wasm_exec.js
	cp $$(go env GOROOT)/lib/wasm/wasm_exec.js server/webstatic/wasm_exec.js
	cp frontend/changelog.txt web/changelog.txt
	cd web && GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -tags osusergo \
		-ldflags "$(LDFLAGS)" -o ../server/webstatic/termd.wasm .

build-termctl:
	cd termctl && go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o ../.local/bin/termctl .

check-wasm:
	cd web && GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -tags osusergo -o /dev/null .

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

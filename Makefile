.PHONY: all build-server changelog build-tui build-termctl build-nxtest build-mousehelper build-nativeapp build-upgrade-test-binaries build-nx2-guest build-nx2-files-guest build-nx2-echo build-nx2-files build-nx2d build-nx2-term build-nx2-host test-nx2 check-wasm check-windows test test-e2e test-race test-upgrade test-stress test-stress-long test-winapp build-winui test-winui rpm version clean

# Binary names
SERVER_BIN   := nxtermd
TUI_BIN      := nxterm
CTL_BIN      := nxtermctl

# Host platform for full-name binaries (e.g. nxtermd-linux-amd64).
HOST_OS_ARCH := $(shell go env GOOS)-$(shell go env GOARCH)

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-g[0-9a-f]*//;s/-dirty/*/' || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
ifndef RELEASE
  GCFLAGS := -gcflags "all=-N -l"
endif

# Set RACE=1 (or invoke the test-race target) to build binaries and run tests
# with the Go race detector enabled. Race-instrumented binaries are ~5-10x
# slower and use ~2x memory, so timeouts need to be looser.
ifdef RACE
  RACEFLAG := -race
endif

all: build-server build-tui build-termctl build-mousehelper build-nativeapp

# Host binaries are built with their full <name>-<os>-<arch> filename so
# the same artifact serves as both the locally-runnable binary and the
# upgrade binary the server hands out to clients. Short names are
# symlinked to the full name for convenience.
build-server:
	go build $(RACEFLAG) $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/$(SERVER_BIN)-$(HOST_OS_ARCH) ./cmd/nxtermd
	ln -sf $(SERVER_BIN)-$(HOST_OS_ARCH) .local/bin/$(SERVER_BIN)

changelog:
	@tmp=$$(mktemp); \
	if ! git diff --quiet HEAD 2>/dev/null || test -n "$$(git ls-files --others --exclude-standard)"; then \
		printf '%18s %s\n' "$$(git describe --tags --always --dirty='*' 2>/dev/null):" "$$(git status --short | tr '\n' ' ')" > "$$tmp"; \
	fi; \
	git log --format='%H %s' -100 | while read hash rest; do \
		ver=$$(git describe --tags --always $$hash 2>/dev/null); \
		printf '%18s %s\n' "$$ver:" "$$rest"; \
	done >> "$$tmp"; \
	mv "$$tmp" dist/changelog.txt

build-tui: changelog
	go build $(RACEFLAG) $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/$(TUI_BIN)-$(HOST_OS_ARCH) ./cmd/nxterm
	ln -sf $(TUI_BIN)-$(HOST_OS_ARCH) .local/bin/$(TUI_BIN)
	GOOS=windows GOARCH=amd64 go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/$(TUI_BIN)-windows-amd64.exe ./cmd/nxterm
	ln -sf $(TUI_BIN)-windows-amd64.exe .local/bin/$(TUI_BIN).exe

build-mousehelper:
	cd e2e/testdata/mousehelper && go build -o ../../../.local/bin/mousehelper .

build-nativeapp:
	cd e2e/testdata/nativeapp && go build -o ../../../.local/bin/nativeapp .

build-termctl:
	go build $(RACEFLAG) $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/$(CTL_BIN) ./cmd/nxtermctl

build-nxtest:
	go build $(GCFLAGS) -o .local/bin/nxtest ./cmd/nxtest

UPGRADE_TEST_DIR := .local/upgrade-binaries
UPGRADE_TEST_VERSION := upgrade-test-v2

build-upgrade-test-binaries: changelog
	@mkdir -p $(UPGRADE_TEST_DIR)
	go build $(RACEFLAG) $(GCFLAGS) -ldflags "-X main.version=$(UPGRADE_TEST_VERSION)" -o $(UPGRADE_TEST_DIR)/$(SERVER_BIN)-$$(go env GOOS)-$$(go env GOARCH) ./cmd/nxtermd
	go build $(RACEFLAG) $(GCFLAGS) -ldflags "-X main.version=$(UPGRADE_TEST_VERSION)" -o $(UPGRADE_TEST_DIR)/$(TUI_BIN)-$$(go env GOOS)-$$(go env GOARCH) ./cmd/nxterm

check-windows:
	GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/nxterm
	GOOS=windows GOARCH=amd64 go build -o /dev/null ./internal/transport

# Build and GUI-test the WinUI 3 HelloApp inside the Windows VM. Drives the
# wfvm environment in testenv/windows (deploy -> provision -> build -> run the
# WinAppDriver UI test). Run from the dev shell (`nix develop`) so the
# wintest-* scripts are on PATH; the VM is started automatically if needed.
test-winapp:
	testenv/windows/helloapp/run-test.sh

# Build the WinUI 3 nxterm GUI client inside the Windows VM (deploy -> provision
# -> publish). Running it is a separate visual step; see clients/winui/README.md.
# Run from the dev shell (`nix develop`).
build-winui:
	clients/winui/build.sh

# WinAppDriver UI test for the GUI client (tabs + status bar). Starts an
# nxtermd on the host, builds the app + test in the VM, and runs the test
# against it. Run from the dev shell (`nix develop`).
test-winui: build-server
	clients/winui/run-uitest.sh

# Host-driven Go e2e tests for the GUI client (go test -tags gui). Boots the VM,
# builds the app, then runs the GUI test variants which read the client's
# rendered grid back over the NXTERM_TEST_HOOK hostfwd. Run from the dev shell.
test-winui-e2e: build-server
	clients/winui/run-e2e.sh

test: test-e2e

test-e2e: all build-upgrade-test-binaries
	PATH="$(CURDIR)/.local/bin:$(PATH)" UPGRADE_BINARIES_DIR="$(CURDIR)/$(UPGRADE_TEST_DIR)" go test $(RACEFLAG) -v -timeout 30s -parallel 8 ./e2e

# Build race-instrumented host binaries and run the full test suite under the
# Go race detector. Race detection slows execution ~5-10x and roughly doubles
# memory, so timeouts are looser and parallelism is reduced. Unit-style
# packages run first (fast feedback) before the e2e suite.
test-race: RACEFLAG := -race
test-race: all build-upgrade-test-binaries
	go test -race -v -timeout 120s ./pkg/... ./internal/...
	PATH="$(CURDIR)/.local/bin:$(PATH)" UPGRADE_BINARIES_DIR="$(CURDIR)/$(UPGRADE_TEST_DIR)" go test -race -v -timeout 300s -parallel 4 ./e2e

test-upgrade: all build-upgrade-test-binaries
	PATH="$(CURDIR)/.local/bin:$(PATH)" UPGRADE_BINARIES_DIR="$(CURDIR)/$(UPGRADE_TEST_DIR)" go test -v -timeout 30s -parallel 8 -run 'TestLiveUpgrade|TestTUIUpgradeE2E|TestUpgradeCheck|TestClientBinaryDownload' ./e2e

# Stress test (quick). Override with env vars:
#   STRESS_TUI_CLIENTS  — number of nxterm instances        (default: 5)
#   STRESS_RAW_CLIENTS  — number of raw protocol clients   (default: 3)
#   STRESS_DURATION     — how long to run                  (default: 30s)
#   STRESS_SEED         — fixed RNG seed for reproduction  (default: random)
test-stress: all
	PATH="$(CURDIR)/.local/bin:$(PATH)" go test -v -tags stress -run TestStress -timeout 300s ./e2e

test-stress-long: all
	PATH="$(CURDIR)/.local/bin:$(PATH)" \
		STRESS_TUI_CLIENTS=10 STRESS_RAW_CLIENTS=5 STRESS_DURATION=120s \
		go test -v -tags stress -run TestStress -timeout 300s ./e2e

rpm: version
	nix build .#rpm --out-link rpm-result

version:
	@echo "$(VERSION)" | tr -d '*' > dist/.version

clean:
	rm -rf .local/bin .local/share/nxtermd dist/.version
	go clean ./...

# --- nx2: terminal-native application platform (branch nx2) ---
# The "app" is a client-side WASM module loaded by the host via wazero; a
# server-side companion brokers OS resources. See nx2/README.md, nx2/doc/.

build-nx2-guest:
	@mkdir -p .local/share/nx2/apps
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared $(GCFLAGS) -o .local/share/nx2/apps/terminal-guest.wasm ./nx2/apps/terminal/guest

build-nx2-files-guest:
	@mkdir -p .local/share/nx2/apps
	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared $(GCFLAGS) -o .local/share/nx2/apps/files-guest.wasm ./nx2/apps/files/guest

build-nx2-echo:
	go build $(GCFLAGS) -o .local/bin/nx2-echo ./nx2/apps/echo/companion

build-nx2-files:
	go build $(GCFLAGS) -o .local/bin/nx2-files ./nx2/apps/files/companion

build-nx2d:
	go build $(GCFLAGS) -o .local/bin/nx2d ./nx2/cmd/nx2d

build-nx2-term:
	go build $(GCFLAGS) -o .local/bin/nx2-term ./nx2/apps/terminal/companion

build-nx2-host:
	go build $(GCFLAGS) -o .local/bin/nx2-host-tui ./nx2/cmd/nx2-host-tui

# Build the guest(s) + companions + host, then run all nx2 tests (they load the
# .wasm and spawn the companions).
test-nx2: build-nx2-guest build-nx2-files-guest build-nx2-echo build-nx2-files build-nx2d build-nx2-term build-nx2-host
	go test ./nx2/...

# Cross-compile gate (mirrors check-windows): fail fast if a WASM-hostile
# dependency creeps into pkg/te or any nx2 guest.
check-wasm:
	GOOS=wasip1 GOARCH=wasm go build -o /dev/null ./pkg/te
	GOOS=wasip1 GOARCH=wasm go build -o /dev/null ./nx2/apps/terminal/guest
	GOOS=wasip1 GOARCH=wasm go build -o /dev/null ./nx2/apps/files/guest
	GOOS=js GOARCH=wasm go build -o /dev/null ./pkg/te

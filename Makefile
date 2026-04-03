.PHONY: all build-server changelog build-tui build-tui-windows build-termctl build-mousehelper build-upgrade-binaries build-upgrade-test-binaries check-windows test test-e2e test-upgrade test-stress test-stress-long rpm version clean

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/-g[0-9a-f]*//;s/-dirty/*/' || echo "dev")
LDFLAGS := -X main.version=$(VERSION)
ifndef RELEASE
  GCFLAGS := -gcflags "all=-N -l"
endif

all: build-server build-tui build-tui-windows build-termctl build-mousehelper build-upgrade-binaries

build-server:
	go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/termd ./server

changelog:
	@tmp=$$(mktemp); \
	if ! git diff --quiet HEAD 2>/dev/null || test -n "$$(git ls-files --others --exclude-standard)"; then \
		printf '%18s %s\n' "$$(git describe --tags --always --dirty='*' 2>/dev/null):" "$$(git status --short | tr '\n' ' ')" > "$$tmp"; \
	fi; \
	git log --format='%H %s' -100 | while read hash rest; do \
		ver=$$(git describe --tags --always $$hash 2>/dev/null); \
		printf '%18s %s\n' "$$ver:" "$$rest"; \
	done >> "$$tmp"; \
	mv "$$tmp" frontend/changelog.txt

build-tui: changelog
	go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/ttui ./frontend
	ln -sf ttui .local/bin/termd-tui

build-tui-windows: changelog
	GOOS=windows GOARCH=amd64 go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/ttui.exe ./frontend

build-mousehelper:
	cd e2e/testdata/mousehelper && go build -o ../../../.local/bin/mousehelper .

build-termctl:
	go build $(GCFLAGS) -ldflags "$(LDFLAGS)" -o .local/bin/termctl ./termctl

UPGRADE_DIR := .local/share/termd

build-upgrade-binaries: changelog
	@mkdir -p $(UPGRADE_DIR)
	RELEASE=1 go build -ldflags "$(LDFLAGS)" -o $(UPGRADE_DIR)/termd-$$(go env GOOS)-$$(go env GOARCH) ./server
	RELEASE=1 go build -ldflags "$(LDFLAGS)" -o $(UPGRADE_DIR)/termd-tui-$$(go env GOOS)-$$(go env GOARCH) ./frontend
	GOOS=windows GOARCH=amd64 RELEASE=1 go build -ldflags "$(LDFLAGS)" -o $(UPGRADE_DIR)/termd-tui-windows-amd64.exe ./frontend

UPGRADE_TEST_DIR := .local/upgrade-binaries
UPGRADE_TEST_VERSION := upgrade-test-v2

build-upgrade-test-binaries: changelog
	@mkdir -p $(UPGRADE_TEST_DIR)
	go build $(GCFLAGS) -ldflags "-X main.version=$(UPGRADE_TEST_VERSION)" -o $(UPGRADE_TEST_DIR)/termd-$$(go env GOOS)-$$(go env GOARCH) ./server
	go build $(GCFLAGS) -ldflags "-X main.version=$(UPGRADE_TEST_VERSION)" -o $(UPGRADE_TEST_DIR)/termd-tui-$$(go env GOOS)-$$(go env GOARCH) ./frontend

check-windows:
	GOOS=windows GOARCH=amd64 go build -o /dev/null ./frontend
	GOOS=windows GOARCH=amd64 go build -o /dev/null ./transport

test: test-e2e

test-e2e: all build-upgrade-test-binaries
	PATH="$(CURDIR)/.local/bin:$(PATH)" UPGRADE_BINARIES_DIR="$(CURDIR)/$(UPGRADE_TEST_DIR)" go test -v -timeout 120s ./e2e

test-upgrade: all build-upgrade-test-binaries
	PATH="$(CURDIR)/.local/bin:$(PATH)" UPGRADE_BINARIES_DIR="$(CURDIR)/$(UPGRADE_TEST_DIR)" go test -v -timeout 120s -run 'TestLiveUpgrade|TestTUIUpgradeE2E|TestUpgradeCheck|TestClientBinaryDownload' ./e2e

# Stress test (quick). Override with env vars:
#   STRESS_TUI_CLIENTS  — number of termd-tui instances    (default: 5)
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
	rm -rf .local/bin dist/.version
	go clean ./...

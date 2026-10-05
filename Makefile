.PHONY: build clean test test-race fmt fmt-check lint vuln tidy-check shellcheck check install release-assets dist

BINARY := aibris
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/sungjunlee/aibris/cmd.version=$(VERSION)
PREFIX ?= $(HOME)/.local/bin

# Pinned so local and CI checks agree.
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.1
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.8.0
GO_FILES = $(shell git ls-files '*.go')
SHELL_FILES = install.sh $(wildcard .github/scripts/*.sh)

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

clean:
	rm -f $(BINARY) $(BINARY).exe tools/perfharness/perfharness coverage.out
	rm -rf dist/ release-assets/

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	@out=$$(gofmt -l $(GO_FILES)); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

lint:
	go vet ./...
	go run $(STATICCHECK) ./...

vuln:
	go run $(GOVULNCHECK) ./...

tidy-check:
	go mod tidy -diff

shellcheck:
	shellcheck $(SHELL_FILES)

# Everything CI's check job runs; fast enough to run before every commit.
check: fmt-check tidy-check lint vuln shellcheck

install: build
	mkdir -p $(PREFIX)
	cp $(BINARY) $(PREFIX)/

release-assets:
	mkdir -p release-assets
	go run ./tools/gen-release-assets release-assets

dist: release-assets
	goreleaser release --snapshot --clean

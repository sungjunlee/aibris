.PHONY: build clean test install

BINARY := aibris
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/sungjunlee/aibris/cmd.version=$(VERSION)

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test ./...

install: build
	cp $(BINARY) /usr/local/bin/

lint:
	go vet ./...

tidy:
	go mod tidy

dist:
	goreleaser release --snapshot --clean

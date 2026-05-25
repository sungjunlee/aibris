.PHONY: build clean test install

BINARY := aibris

build:
	go build -o $(BINARY) .

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

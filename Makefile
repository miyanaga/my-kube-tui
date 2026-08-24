BIN := bin/my-kube-tui
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build run check clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/my-kube-tui

run: build
	./$(BIN)

check:
	go vet ./...
	go build ./...

clean:
	rm -rf bin

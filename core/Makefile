.PHONY: build test matrix

VERSION := $(shell cat ../VERSION 2>/dev/null || echo dev)

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "-X quotamon/internal/version.Value=$(VERSION)" -o bin/quotamon ./cmd/quotamon

test:
	go vet ./...
	go test ./...

matrix:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X quotamon/internal/version.Value=$(VERSION)" -o bin/quotamon-darwin-arm64 ./cmd/quotamon
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w -X quotamon/internal/version.Value=$(VERSION)" -o bin/quotamon-darwin-amd64 ./cmd/quotamon
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X quotamon/internal/version.Value=$(VERSION)" -o bin/quotamon-linux-amd64 ./cmd/quotamon
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X quotamon/internal/version.Value=$(VERSION)" -o bin/quotamon-linux-arm64 ./cmd/quotamon

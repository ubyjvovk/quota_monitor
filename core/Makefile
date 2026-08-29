.PHONY: build test matrix

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/quotamon ./cmd/quotamon

test:
	go vet ./...
	go test ./...

matrix:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/quotamon-darwin-arm64 ./cmd/quotamon
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/quotamon-linux-amd64 ./cmd/quotamon
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o bin/quotamon-linux-arm64 ./cmd/quotamon

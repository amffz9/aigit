.PHONY: build test

build:
	go build -ldflags="-s -w" -o aigit .

test:
	go test ./... -v

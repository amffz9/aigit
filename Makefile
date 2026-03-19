ONEDRIVE := $(HOME)/Library/CloudStorage/OneDrive-CityofChesterfield/Claude
DIST := dist
LDFLAGS := -s -w

.PHONY: build build-linux build-all clean test deploy

# Build native binary for the current platform (macOS/arm64)
build:
	go build -ldflags="$(LDFLAGS)" -o aigit .

# Cross-compile for WSL / Linux (amd64)
build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o aigit-linux .

# Build release binaries for macOS, Linux, and Windows
build-all: clean
	mkdir -p "$(DIST)"
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o "$(DIST)/aigit-darwin-arm64" .
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o "$(DIST)/aigit-linux-amd64" .
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o "$(DIST)/aigit-windows-amd64.exe" .

clean:
	rm -rf "$(DIST)"

# Run the full test suite
test:
	go test ./... -v

# Build the Linux binary and copy it to OneDrive for transfer to WSL
deploy: build-linux
	cp aigit-linux "$(ONEDRIVE)/aigit-linux"
	@echo "Copied aigit-linux to $(ONEDRIVE)"
	@echo "In WSL, copy it from your Windows OneDrive mount, e.g.:"
	@echo "  cp /mnt/c/Users/<you>/OneDrive/Claude/aigit-linux ~/bin/aigit"
	@echo "  chmod +x ~/bin/aigit"

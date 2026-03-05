ONEDRIVE := $(HOME)/Library/CloudStorage/OneDrive-CityofChesterfield/Claude

.PHONY: build build-linux test deploy

# Build native binary for the current platform (macOS/arm64)
build:
	go build -ldflags="-s -w" -o aigit .

# Cross-compile for WSL / Linux (amd64)
build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o aigit-linux .

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

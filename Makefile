.PHONY: dev run build build-linux css css-watch clean test

# Tailwind standalone CLI
TAILWIND := ./bin/tailwindcss
DAISYUI := ./bin/daisyui.mjs
DAISYUI_THEME := ./bin/daisyui-theme.mjs

$(TAILWIND):
	@mkdir -p bin
	@echo "Downloading Tailwind CSS standalone CLI..."
	@curl -sL https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-macos-arm64 -o $(TAILWIND)
	@chmod +x $(TAILWIND)

$(DAISYUI):
	@mkdir -p bin
	@echo "Downloading daisyUI plugin..."
	@curl -sL https://github.com/saadeghi/daisyui/releases/latest/download/daisyui.mjs -o $(DAISYUI)

$(DAISYUI_THEME):
	@mkdir -p bin
	@echo "Downloading daisyUI theme plugin..."
	@curl -sL https://github.com/saadeghi/daisyui/releases/latest/download/daisyui-theme.mjs -o $(DAISYUI_THEME)

# Build CSS
css: $(TAILWIND) $(DAISYUI) $(DAISYUI_THEME)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --minify

# Watch CSS for development
css-watch: $(TAILWIND) $(DAISYUI) $(DAISYUI_THEME)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --watch

# Run in development mode
run:
	DEV_MODE=true go run ./cmd/server

# Build production binary (host OS/arch)
build: css
	CGO_ENABLED=0 go build -o latasya-erp ./cmd/server

# Cross-compile for the Linux VPS (amd64 by default; override with GOARCH=arm64)
GOARCH ?= amd64
build-linux: css
	GOOS=linux GOARCH=$(GOARCH) CGO_ENABLED=0 go build -o latasya-erp ./cmd/server

# Run tests
test:
	go test ./... -v

# Clean build artifacts
clean:
	rm -f latasya-erp
	rm -f static/css/app.css
	rm -rf bin/

.PHONY: dev run build build-linux css css-watch clean test test-reference

# Tailwind standalone CLI
TAILWIND := ./bin/tailwindcss
DAISYUI := ./bin/daisyui.mjs
DAISYUI_THEME := ./bin/daisyui-theme.mjs

# Pinned CSS toolchain versions — keep in sync with .github/workflows/deploy.yml
TAILWIND_VERSION := v4.3.3
DAISYUI_VERSION := v5.6.18

$(TAILWIND):
	@mkdir -p bin
	@echo "Downloading Tailwind CSS standalone CLI..."
	@curl -sL https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-macos-arm64 -o $(TAILWIND)
	@chmod +x $(TAILWIND)

$(DAISYUI):
	@mkdir -p bin
	@echo "Downloading daisyUI plugin..."
	@curl -sL https://github.com/saadeghi/daisyui/releases/download/$(DAISYUI_VERSION)/daisyui.mjs -o $(DAISYUI)

$(DAISYUI_THEME):
	@mkdir -p bin
	@echo "Downloading daisyUI theme plugin..."
	@curl -sL https://github.com/saadeghi/daisyui/releases/download/$(DAISYUI_VERSION)/daisyui-theme.mjs -o $(DAISYUI_THEME)

# Build CSS
css: $(TAILWIND) $(DAISYUI) $(DAISYUI_THEME)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --minify

# Watch CSS for development
css-watch: $(TAILWIND) $(DAISYUI) $(DAISYUI_THEME)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --watch

# Run in development mode
run:
	DEV_MODE=true bun run src/main.ts

# Build identity, surfaced at /healthz. CI overrides with the commit SHA;
# local builds stay "dev".
VERSION ?= dev

# Build production binary (host OS/arch)
build: css
	VERSION=$(VERSION) bun run build:bun
	cp dist/latasya-erp latasya-erp

# Build the standalone Linux executable used by the amd64 VPS.
build-linux: css
	VERSION=$(VERSION) bun run build:bun:linux
	cp dist/latasya-erp latasya-erp

# Run tests
test:
	bun run check:bun

# Keep the Go implementation green as the rollback reference through cutover.
test-reference:
	go test ./... -v

# Clean build artifacts
clean:
	rm -f latasya-erp
	rm -rf dist/
	rm -f static/css/app.css
	rm -rf bin/

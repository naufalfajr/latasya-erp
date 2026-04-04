.PHONY: dev run build css css-watch clean test

# Tailwind standalone CLI
TAILWIND := ./bin/tailwindcss

$(TAILWIND):
	@mkdir -p bin
	@echo "Downloading Tailwind CSS standalone CLI..."
	@curl -sL https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-macos-arm64 -o $(TAILWIND)
	@chmod +x $(TAILWIND)

# Build CSS
css: $(TAILWIND)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --minify

# Watch CSS for development
css-watch: $(TAILWIND)
	$(TAILWIND) -i static/css/input.css -o static/css/app.css --watch

# Run in development mode
run:
	DEV_MODE=true go run ./cmd/server

# Build production binary
build: css
	CGO_ENABLED=0 go build -o latasya-erp ./cmd/server

# Run tests
test:
	go test ./... -v

# Clean build artifacts
clean:
	rm -f latasya-erp
	rm -f static/css/app.css
	rm -rf bin/

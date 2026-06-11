BINARY := translai
PKG    := ./...

.PHONY: all build run test test-integration lint vet fmt check tidy clean

all: check

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) ./cmd/translai

run: build
	./$(BINARY) $(ARGS)

test:
	go test $(PKG)

# Tests intégration LLM réel (Ollama local). Skip auto si endpoint absent.
test-integration:
	go test -tags=integration $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -l -w .

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "⚠ golangci-lint absent — lint sauté. Install:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "  (ou: brew install golangci-lint)"; \
	fi

tidy:
	go mod tidy

# Gate milestone : doit être 100% vert AVANT chaque commit gitmoji.
check: tidy vet lint test build
	@echo "OK — gate milestone verte"

clean:
	rm -f $(BINARY)
	rm -rf dist/

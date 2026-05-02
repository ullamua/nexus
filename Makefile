BINARY      = bin/nexus
MAIN        = .
BUILD_FLAGS = -ldflags="-s -w"
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Load .env automatically if it exists.
ifneq (,$(wildcard .env))
  include .env
  export
endif

.PHONY: all build test live vet check clean run dev setup zip install release docker docker-push mod-tidy lint

all: build

build:
	@mkdir -p bin
	go build $(BUILD_FLAGS) -o $(BINARY) $(MAIN)

test:
	go test ./tests/... -v -timeout 60s

live:
	go test -tags live -v -timeout 120s ./tests/... -run TestLive

vet:
	go vet ./...

check: vet test

clean:
	rm -rf bin/ dist/

run: build
	@test -n "$(NEXUS_VAULT_KEY)" || { \
	  echo ""; \
	  echo "  ERROR: NEXUS_VAULT_KEY is not set."; \
	  echo "  Run: make setup   then edit .env"; \
	  echo ""; \
	  exit 1; \
	}
	@echo ""
	@echo "  Nexus $(VERSION) starting on http://localhost:$(or $(NEXUS_PORT),8080)"
	@echo "  Dashboard      : http://localhost:$(or $(NEXUS_PORT),8080)/dashboard/"
	@echo "  Connectors dir : $(or $(NEXUS_CONNECTORS_DIR),./connectors.d)"
	@echo "  Press Ctrl+C to stop."
	@echo ""
	NEXUS_PORT=$(or $(NEXUS_PORT),8080) NEXUS_DASHBOARD=true ./$(BINARY)

dev: build
	@test -n "$(NEXUS_VAULT_KEY)" || { echo "ERROR: NEXUS_VAULT_KEY is not set. Run: make setup"; exit 1; }
	NEXUS_PORT=$(or $(NEXUS_PORT),8080) NEXUS_LOG_LEVEL=debug NEXUS_TRACE=true NEXUS_DASHBOARD=true ./$(BINARY)

setup:
	@if [ ! -f .env ]; then \
	  cp .env.example .env; \
	  echo ""; \
	  echo "  Created .env from .env.example"; \
	  echo "  Edit .env and set NEXUS_VAULT_KEY to a 32+ char secret."; \
	  echo "  Tip: openssl rand -hex 32"; \
	  echo ""; \
	else \
	  echo ".env already exists -- skipping (delete it to reset)"; \
	fi

zip: build
	@mkdir -p dist
	@echo "Packaging nexus-$(VERSION).zip ..."
	zip -r dist/nexus-$(VERSION).zip . \
	  --exclude "bin/*" --exclude ".env" --exclude "dist/*" \
	  --exclude "*.log" --exclude "vault.enc" --exclude ".git/*" \
	  --exclude "*.enc" --exclude "node_modules/*"
	@echo "Created dist/nexus-$(VERSION).zip"

install: build
	@install -d $(HOME)/.local/bin
	@install -m 0755 $(BINARY) $(HOME)/.local/bin/nexus
	@echo "Installed nexus to ~/.local/bin/nexus"
	@echo "Make sure ~/.local/bin is in your PATH."

release:
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64  go build $(BUILD_FLAGS) -o dist/nexus-linux-amd64    $(MAIN)
	GOOS=linux   GOARCH=arm64  go build $(BUILD_FLAGS) -o dist/nexus-linux-arm64    $(MAIN)
	GOOS=darwin  GOARCH=arm64  go build $(BUILD_FLAGS) -o dist/nexus-darwin-arm64   $(MAIN)
	GOOS=darwin  GOARCH=amd64  go build $(BUILD_FLAGS) -o dist/nexus-darwin-amd64   $(MAIN)
	GOOS=windows GOARCH=amd64  go build $(BUILD_FLAGS) -o dist/nexus-windows-amd64.exe $(MAIN)
	@echo "Binaries in dist/"

docker:
	docker build -f deploy/Dockerfile -t nexus:$(VERSION) -t nexus:latest .

docker-push:
	docker push $${REGISTRY:?REGISTRY is required}/nexus:$(VERSION)
	docker push $${REGISTRY}/nexus:latest

mod-tidy:
	go mod tidy

lint:
	golangci-lint run ./...

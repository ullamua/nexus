#!/usr/bin/env bash
# Nexus one-liner setup script.
# Usage: bash install.sh
set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

info()    { printf "${GREEN}[nexus]${NC} %s\n" "$*"; }
warn()    { printf "${YELLOW}[nexus]${NC} %s\n" "$*"; }
error()   { printf "${RED}[nexus]${NC} %s\n" "$*" >&2; exit 1; }

# ── Requirements ──────────────────────────────────────────────────────────────
command -v go  >/dev/null 2>&1 || error "Go is required. Install from https://go.dev/dl/"
command -v make>/dev/null 2>&1 || error "make is required (sudo apt install make  or  xcode-select --install)"

GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
MAJOR=$(echo "$GO_VER" | cut -d. -f1)
MINOR=$(echo "$GO_VER" | cut -d. -f2)
if [ "$MAJOR" -lt 1 ] || { [ "$MAJOR" -eq 1 ] && [ "$MINOR" -lt 22 ]; }; then
  error "Go 1.22+ required (found go$GO_VER). Upgrade from https://go.dev/dl/"
fi

info "Go $GO_VER — OK"

# ── Build ─────────────────────────────────────────────────────────────────────
info "Building nexus binary ..."
make build

# ── .env setup ────────────────────────────────────────────────────────────────
if [ ! -f .env ]; then
  cp .env.example .env
  warn ".env created from .env.example"
  warn "Edit .env and set NEXUS_VAULT_KEY to a 32+ character secret."
  warn "  Tip: openssl rand -hex 32"
else
  info ".env already exists — skipping"
fi

# ── Check NEXUS_VAULT_KEY ─────────────────────────────────────────────────────
# shellcheck disable=SC1091
source .env 2>/dev/null || true
if [ -z "${NEXUS_VAULT_KEY:-}" ]; then
  warn ""
  warn "NEXUS_VAULT_KEY is not set in .env."
  warn "Generate one with:  openssl rand -hex 32"
  warn "Then run:           make run"
  warn ""
else
  KEY_LEN=${#NEXUS_VAULT_KEY}
  if [ "$KEY_LEN" -lt 32 ]; then
    warn "NEXUS_VAULT_KEY is only $KEY_LEN chars — use 32+ for AES-256 security."
  else
    info "NEXUS_VAULT_KEY — OK (${KEY_LEN} chars)"
  fi
fi

# ── Done ──────────────────────────────────────────────────────────────────────
info ""
info "Setup complete. Quick start:"
info ""
info "  make run            # start server (loads .env automatically)"
info "  make dev            # start with debug logging + trace enabled"
info "  make test           # run unit tests"
info "  make live           # run E2E tests against live APIs"
info "  make release        # cross-compile for all platforms"
info ""
info "  curl http://localhost:8080/health"
info "  curl http://localhost:8080/registry"
info ""

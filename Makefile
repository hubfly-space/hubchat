# Hubchat — one binary, embedded React.
#
# The production artifact is a single Go binary. Frontend tooling runs only at
# build time; nothing from web/ ships as a runtime dependency.

BINARY      := hubchat
PKG         := ./cmd/hubchat
DIST        := dist
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

GO      ?= go
PNPM    ?= pnpm

.DEFAULT_GOAL := help

## ---------------------------------------------------------------- help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## ---------------------------------------------------------------- setup

.PHONY: install
install: ## Install frontend + Go dependencies
	$(PNPM) install
	$(GO) mod download

## ---------------------------------------------------------------- dev

.PHONY: dev-db
dev-db: ## Start PostgreSQL, MailHog, and MinIO, then wait for readiness
	docker compose up -d
	@echo "==> waiting for PostgreSQL"
	@until docker compose exec -T postgres pg_isready -U hubchat -d hubchat >/dev/null 2>&1; do sleep 1; done
	@echo "==> postgres :5432   mailhog http://localhost:8025   minio http://localhost:9001"

.PHONY: dev-db-down
dev-db-down: ## Stop the dev containers, keeping their data
	docker compose down

.PHONY: dev-db-reset
dev-db-reset: ## Stop the dev containers and destroy their data
	docker compose down -v

.PHONY: dev-db-shell
dev-db-shell: ## Open psql against the dev database
	docker compose exec postgres psql -U hubchat -d hubchat

.PHONY: dev
dev: ## Run Go server + dashboard dev server (proxied)
	@echo "==> Go API on :8080, dashboard on :5173 (proxying /api and /ws)"
	@$(MAKE) -j2 dev-server dev-dashboard

.PHONY: dev-server
dev-server: ## Run the Go server in dev mode (serves from disk, not embed)
	HUBCHAT_DEV=1 $(GO) run $(PKG) serve

.PHONY: dev-dashboard
dev-dashboard: ## Vite dev server for the agent/admin dashboard
	$(PNPM) --filter @hubchat/dashboard dev

.PHONY: dev-portal
dev-portal: ## Vite dev server for the customer portal
	$(PNPM) --filter @hubchat/portal dev

.PHONY: dev-widget
dev-widget: ## Vite dev server for the embeddable widget
	$(PNPM) --filter @hubchat/widget dev

## ---------------------------------------------------------------- build

.PHONY: web
web: ## Build all React bundles into embedded/assets
	$(PNPM) -r --filter "./web/*" build

.PHONY: build
build: web ## Build the release binary with assets embedded
	mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)
	@echo "==> $(DIST)/$(BINARY) ($(VERSION))"

.PHONY: build-go
build-go: ## Build the binary without rebuilding web assets
	mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)

.PHONY: checksums
checksums: ## Produce SHA256 checksums for release artifacts
	cd $(DIST) && sha256sum * > SHA256SUMS

## ---------------------------------------------------------------- quality

.PHONY: check
check: typecheck lint vet test ## Run every check the CI pipeline runs

.PHONY: typecheck
typecheck: ## TypeScript project-wide typecheck
	$(PNPM) -r --filter "./web/*" typecheck

.PHONY: lint
lint: ## ESLint across the web workspaces
	$(PNPM) -r --filter "./web/*" lint

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: test
test: ## Go unit tests
	$(GO) test ./... -race -count=1

# Deliberately a separate variable from HUBCHAT_DATABASE_URL: these tests
# truncate tenant data, so pointing them at a database has to be a choice
# rather than something a stray export does for you.
TEST_DATABASE_URL ?= postgres://hubchat:hubchat@localhost:5432/hubchat?sslmode=disable

# -p 1 runs one package at a time. These tests share one database and reset it
# between cases, so running two packages concurrently has them deleting each
# other's fixtures mid-test — which shows up as foreign key violations and
# deadlocks that look like product bugs but are not.
.PHONY: test-integration
test-integration: ## Go tests that require a live PostgreSQL (make dev-db first)
	HUBCHAT_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" \
		$(GO) test ./... -race -count=1 -p 1 -tags=integration

.PHONY: fmt
fmt: ## Format Go sources
	$(GO) fmt ./...

## ---------------------------------------------------------------- database

.PHONY: migrate
migrate: ## Apply pending migrations
	$(GO) run $(PKG) migrate

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run $(PKG) migrate status

## ---------------------------------------------------------------- housekeeping

.PHONY: clean
clean: ## Remove build output
	rm -rf $(DIST) web/*/dist embedded/assets/dashboard embedded/assets/portal embedded/assets/widget

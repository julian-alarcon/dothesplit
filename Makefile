.PHONY: help gen gen-go gen-ts migrate-up migrate-down test test-go test-web dev dev-api dev-web lint lint-go lint-web build tidy up licenses sbom compliance

SHELL := /bin/bash

DATABASE_URL ?= postgres://dts:dts@localhost:5432/dts?sslmode=disable

help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

gen: gen-go gen-ts ## Regenerate server + client bindings from docs/openapi.yaml

gen-go: ## Generate Go types + chi server interface
	cd api && go generate ./...

gen-ts: ## Generate TypeScript types for the web client
	cd web && npx openapi-typescript ../docs/openapi.yaml -o src/lib/api/schema.d.ts

migrate-up: ## Apply all migrations
	cd api && go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate \
		-path ./migrations -database "$(DATABASE_URL)" up

migrate-down: ## Roll back the most recent migration
	cd api && go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate \
		-path ./migrations -database "$(DATABASE_URL)" down 1

test: test-go test-web ## Run all tests

test-go: ## Run Go unit + integration tests
	cd api && go test ./... -race

test-web: ## Run web tests
	cd web && npm test --silent || true

dev-api: ## Run the API locally
	cd api && go run ./cmd/api

dev-web: ## Run the web frontend locally
	cd web && npm run dev

dev: ## Run api + web concurrently (requires tmux or two terminals)
	@echo "Run 'make dev-api' and 'make dev-web' in separate terminals."

tidy: ## go mod tidy
	cd api && go mod tidy

lint: lint-go lint-web ## Lint everything

lint-go: ## Lint Go
	cd api && golangci-lint run ./...

lint-web: ## Lint web
	cd web && npm run lint --silent || true

build: ## Build Go binaries
	cd api && go build -o bin/api ./cmd/api && go build -o bin/worker ./cmd/worker

up: ## Rebuild + start the full stack, baking git SHA + web/package.json version into images
	BUILD_COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo dev) \
	BUILD_VERSION=$$(node -p "require('./web/package.json').version" 2>/dev/null || echo dev) \
		docker compose up -d --build

licenses: ## Generate THIRD_PARTY_LICENSES.md and web/src/lib/credits.json
	./scripts/generate-licenses.sh

sbom: ## Generate CycloneDX SBOMs into ./sbom/
	./scripts/generate-sbom.sh

compliance: licenses sbom ## Run all license + SBOM generation

# WhatsApp V2 — developer entry points
# Docs: Docs/README.md · Tasks: Docs/12-planning/task-breakdown.md

COMPOSE := docker compose -f deploy/compose/docker-compose.yml

.PHONY: help dev-up dev-up-all dev-down dev-logs dev-clean build test lint fmt proto

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

dev-up: ## Start core infra (PG, Valkey, NATS, MinIO)
	$(COMPOSE) up -d

dev-up-all: ## Start everything incl. observability + offline-profile services
	$(COMPOSE) --profile observability --profile offline up -d

dev-down: ## Stop the dev stack (data kept)
	$(COMPOSE) down

dev-logs: ## Tail dev-stack logs
	$(COMPOSE) logs -f --tail=100

dev-clean: ## Stop and DELETE all dev data volumes
	$(COMPOSE) --profile observability --profile offline down -v

build: ## Build all Go deployables
	cd server && go build ./...

test: ## Run Go tests with race detector
	cd server && go test -race ./...

lint: ## Run golangci-lint (server) — install: https://golangci-lint.run
	cd server && golangci-lint run

fmt: ## Format Go code
	cd server && gofmt -w .

proto: ## Generate Go + TS from protobuf (requires buf)
	cd server/proto && buf generate

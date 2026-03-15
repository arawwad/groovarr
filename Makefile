PKGS_TEST := ./cmd/... ./graph ./internal/...
SONIC_ENABLED ?= $(SONIC_ANALYSIS_ENABLED)
DEV_ENV_FILE ?= .env
DEV_BASE_COMPOSE := docker-compose.dev.yml
DEV_SONIC_COMPOSE := docker-compose.dev.sonic.yml
DIST_BASE_COMPOSE := docker-compose.yml
DIST_SONIC_COMPOSE := docker-compose.sonic.yml
ifeq ($(strip $(SONIC_ENABLED)),)
SONIC_ENABLED := true
endif
ifeq ($(SONIC_ENABLED),false)
DEV_COMPOSE_ARGS := --env-file $(DEV_ENV_FILE) -f $(DEV_BASE_COMPOSE)
DIST_COMPOSE_ARGS := --env-file $(DEV_ENV_FILE) -f $(DIST_BASE_COMPOSE)
else
DEV_COMPOSE_ARGS := --env-file $(DEV_ENV_FILE) -f $(DEV_BASE_COMPOSE) -f $(DEV_SONIC_COMPOSE)
DIST_COMPOSE_ARGS := --env-file $(DEV_ENV_FILE) -f $(DIST_BASE_COMPOSE) -f $(DIST_SONIC_COMPOSE)
endif

.PHONY: test test-all fmt dev-up dev-down dev-ps up down ps

test:
	go test $(PKGS_TEST)

test-all:
	go test ./...

fmt:
	gofmt -w cmd/server/main.go cmd/server/lidarr_cleanup.go internal/agent/executor.go internal/db/postgres.go internal/db/sync.go

dev-up:
	docker compose $(DEV_COMPOSE_ARGS) up -d --build

dev-down:
	docker compose $(DEV_COMPOSE_ARGS) down

dev-ps:
	docker compose $(DEV_COMPOSE_ARGS) ps

up:
	docker compose $(DIST_COMPOSE_ARGS) up -d

down:
	docker compose $(DIST_COMPOSE_ARGS) down

ps:
	docker compose $(DIST_COMPOSE_ARGS) ps

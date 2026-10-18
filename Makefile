# ballistic-mannequin: local development, tests, image and production deploy. `make` lists the targets.
# Runtime settings: docs/contracts/layout.md. Operations guide (in Russian): docs/ops.md.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

DEV_COMPOSE  := docker compose -f deploy/compose.dev.yml
PROD_COMPOSE := docker compose -f deploy/compose.prod.yml --env-file deploy/.env

# 1 when Postgres is provided from outside (the Codespaces devcontainer): then nothing here starts or stops it.
MQ_EXTERNAL_DB ?= 0
MQ_PG_PORT     ?= 5432
PORT           ?= 8080
DATABASE_URL      ?= postgres://mq:mq@localhost:$(MQ_PG_PORT)/mq?sslmode=disable
TEST_DATABASE_URL ?= postgres://mq:mq@localhost:$(MQ_PG_PORT)/mq_test?sslmode=disable
BUILD ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
export MQ_PG_PORT BUILD

HAVE_SERVER := $(wildcard server/go.mod)
NO_SERVER_MSG := server/ is not in this checkout yet (team B's Go API, module server/, main package server/cmd/api)

.PHONY: help
help: ## list the targets
	@grep -hE '^[a-z][a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-14s %s\n", $$1, $$2}'

# --- development ---------------------------------------------------------------------------------------------------

.PHONY: db-up db-down db-reset db-shell
db-up: ## start the dev Postgres (docker) and wait until it is ready
ifeq ($(MQ_EXTERNAL_DB),1)
	@pg_isready -q -d "$(DATABASE_URL)" || { echo "Postgres at DATABASE_URL does not answer" >&2; exit 1; }
else
	$(DEV_COMPOSE) up -d --wait postgres
endif

db-down: ## stop the dev Postgres (data is kept)
ifneq ($(MQ_EXTERNAL_DB),1)
	$(DEV_COMPOSE) down
endif

db-reset: ## stop the dev Postgres and delete its data
ifneq ($(MQ_EXTERNAL_DB),1)
	$(DEV_COMPOSE) down -v
endif

db-shell: ## psql into the dev database
ifeq ($(MQ_EXTERNAL_DB),1)
	psql "$(DATABASE_URL)"
else
	$(DEV_COMPOSE) exec postgres psql -U mq -d mq
endif

.PHONY: dev need-server
need-server:
	@test -n "$(HAVE_SERVER)" || { echo "$(NO_SERVER_MSG): nothing to run or build." >&2; exit 1; }

dev: need-server db-up ## Postgres + go run ./server/cmd/api on http://localhost:8080
	@echo "API and simulator: http://localhost:$(PORT)  (Ctrl-C stops the API; make db-down stops Postgres)"
	PORT=$(PORT) STATIC_DIR="$(CURDIR)" DATABASE_URL="$(DATABASE_URL)" BUILD=$(BUILD) go -C server run ./cmd/api

# --- tests ---------------------------------------------------------------------------------------------------------

.PHONY: test test-go test-browser
test: test-go test-browser ## go test ./... in server/ and the browser tests in tests/

test-go: ## go test ./... against the dev Postgres (database mq_test)
ifeq ($(HAVE_SERVER),)
	@echo "skipping go test: $(NO_SERVER_MSG)."
else
	@$(MAKE) --no-print-directory db-up
	cd server && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...
endif

tests/node_modules: tests/package-lock.json
	cd tests && npm ci
	@touch $@

test-browser: tests/node_modules ## the headless Chromium tests (needs: cd tests && npx playwright-core install chromium)
	cd tests && npm test

# --- production (on the VPS) ---------------------------------------------------------------------------------------

.PHONY: build-image up-prod down-prod logs-prod ps-prod db-shell-prod backup restore-check need-env
need-env:
	@test -f deploy/.env || { echo "deploy/.env is missing: cp deploy/.env.example deploy/.env and fill it in (docs/ops.md)" >&2; exit 1; }

build-image: need-server ## docker build of the API image, tagged mq-api:<git sha> and mq-api:latest
	docker build -f deploy/Dockerfile.api --build-arg BUILD=$(BUILD) -t mq-api:$(BUILD) -t mq-api:latest .

up-prod: need-server need-env ## build and (re)start caddy + api + postgres from deploy/compose.prod.yml
	$(PROD_COMPOSE) up -d --build --wait --remove-orphans
	@$(PROD_COMPOSE) ps

down-prod: need-env ## stop the production stack (volumes are kept)
	$(PROD_COMPOSE) down

logs-prod: need-env ## follow the production logs (all services; e.g. make logs-prod S=api)
	$(PROD_COMPOSE) logs -f --tail=200 $(S)

ps-prod: need-env ## state and health of the production containers
	$(PROD_COMPOSE) ps

db-shell-prod: need-env ## psql into the production database
	$(PROD_COMPOSE) exec postgres psql -U mq -d mq

backup: need-env ## dump the production database now (deploy/backup/backup.sh)
	deploy/backup/backup.sh

restore-check: need-env ## restore a dump into the throwaway database mq_restore_check: make restore-check FILE=…
	@test -n "$(FILE)" || { echo "usage: make restore-check FILE=/var/backups/mq/mq-….dump" >&2; exit 2; }
	deploy/backup/restore.sh --db mq_restore_check --fresh "$(FILE)"

# --- checks of the infrastructure files ----------------------------------------------------------------------------

.PHONY: lint-infra
lint-infra: ## compose config, shellcheck and actionlint over deploy/, .devcontainer/ and .github/
	$(DEV_COMPOSE) --profile api config -q
	DOMAIN=example.ru ACME_EMAIL=a@example.ru POSTGRES_PASSWORD=x docker compose -f deploy/compose.prod.yml --env-file /dev/null config -q
	docker compose -f .devcontainer/compose.yml config -q
	shellcheck -x deploy/backup/*.sh deploy/postgres/*.sh .devcontainer/*.sh
	actionlint

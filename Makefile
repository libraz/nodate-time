.PHONY: dev dev-api db-up db-down db-schema db-apply db-seed db-seed-users sqlc web api build-api create-user test-api test-e2e test-e2e-storage minio-up format lint

# Local dev defaults: enable dev-login (admin rights come from the users.is_admin
# flag, seeded for admin@example.com). Respects values already set in the shell.
DEV_API_ENV = TC_ENV=$${TC_ENV:-development}

# Development – start everything (DB + API + Web) in parallel
dev: db-up
	@trap 'kill 0' EXIT; \
	(cd apps/api && $(DEV_API_ENV) go run ./cmd/api) & \
	(cd apps/web && bun run dev) & \
	wait

# Database
db-up:
	docker compose up -d mysql

db-down:
	docker compose down

db-schema:
	bash sql/build-schema.sh

db-apply: db-schema
	docker run --rm -i --network host mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < $(CURDIR)/sql/schema.sql

# Create the demo/admin accounts via the helper (no password hashes in SQL),
# then load the sample calendars/events/memos that reference them by email.
db-seed: db-apply db-seed-users
	docker run --rm -i --network host mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < $(CURDIR)/sql/seed.sql

db-seed-users:
	cd apps/api && go run ./cmd/createuser -skip-existing \
		-email demo@example.com -password password123 -name "Demo User" -icon 😊 -color "#2ECC87"
	cd apps/api && go run ./cmd/createuser -skip-existing -admin \
		-email admin@example.com -password password123 -name "Admin User" -icon 🛠️ -color "#E73B3B"

# Code generation
sqlc:
	cd sql && sqlc generate

# API
api:
	cd apps/api && $(DEV_API_ENV) go run ./cmd/api

build-api:
	cd apps/api && go build -o ../../bin/api ./cmd/api

# Create a user. Example:
#   make create-user ARGS="-email admin@foo.com -password secret123 -admin"
create-user:
	cd apps/api && go run ./cmd/createuser $(ARGS)

# Testing
test-api:
	cd apps/api && go test ./... -count=1

test-e2e:
	cd apps/api && TC_TEST_INTEGRATION=1 go test ./tests/e2e/ -v -count=1

test-e2e-storage:
	cd apps/api && TC_TEST_INTEGRATION=1 TC_TEST_MINIO=1 go test ./tests/e2e/ -v -count=1

minio-up:
	docker compose up -d minio

# Formatting & linting
# format: apply fixes in place – gofmt for Go, Biome (format + safe lint fixes) for web.
format:
	gofmt -w apps/api
	bunx biome check --write .

# lint: report-only (CI-friendly), no writes. Fails if anything is unformatted.
lint:
	@unformatted="$$(gofmt -l apps/api)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: the following files need formatting (run 'make format'):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	bunx biome check .

# Frontend
web:
	cd apps/web && bun run dev

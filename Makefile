.PHONY: dev dev-api db-up db-down db-schema db-apply db-seed sqlc web api build-api test-api test-e2e

# Development – start everything (DB + API + Web) in parallel
dev: db-up
	@trap 'kill 0' EXIT; \
	(cd apps/api && go run ./cmd/api) & \
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
	docker run --rm --network host -v $(CURDIR)/sql:/sql mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < /sql/schema.sql

db-seed: db-apply
	docker run --rm --network host -v $(CURDIR)/sql:/sql mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < /sql/seed.sql

# Code generation
sqlc:
	cd sql && sqlc generate

# API
api:
	cd apps/api && go run ./cmd/api

build-api:
	cd apps/api && go build -o ../../bin/api ./cmd/api

# Testing
test-api:
	cd apps/api && go test ./... -count=1

test-e2e:
	cd apps/api && TC_TEST_INTEGRATION=1 go test ./tests/e2e/ -v -count=1

# Frontend
web:
	cd apps/web && bun run dev

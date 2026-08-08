.PHONY: dev dev-api db-up db-down db-schema db-apply db-seed db-seed-users sqlc verify-codegen web api build-api create-user test-api test-e2e test-e2e-storage test-e2e-storage-absent test-conformance check-core minio-up format lint

# What the API server needs to run locally. Only the server: a command that
# talks to the database answers none of this, so none of it is passed to one.
# Respects values already set in the shell, so CI can pass its own.
#
# TC_ENV=development registers /auth/dev-login, the password-less sign-in for
# the seeded @example.com accounts. Admin rights are not part of it: they come
# from the instance_admins table, which createuser -admin writes below.
#
# The other two say out loud what running locally actually relaxes -- the
# object store ships with published credentials and there is no mail relay --
# rather than letting one environment value stand for both.
DEV_JWT_SECRET_FILE = secrets/jwt-dev.key

DEV_API_ENV = TC_ENV=$${TC_ENV:-development} \
	TC_JWT_SECRET=$${TC_JWT_SECRET:-$$(cat $(CURDIR)/$(DEV_JWT_SECRET_FILE))} \
	TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS=$${TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS:-true} \
	TC_ALLOW_CONSOLE_MAILER=$${TC_ALLOW_CONSOLE_MAILER:-true}

# Generated once per checkout, never committed: secrets/ is gitignored. A
# literal here would be published exactly like the default it replaces, and
# being published is the whole reason that default is refused. Generating it
# once rather than per run keeps sessions alive across an API restart.
$(DEV_JWT_SECRET_FILE):
	@mkdir -p $(dir $@)
	@openssl rand -hex 32 > $@
	@echo "generated a development signing secret at $@"

# Development – start everything (DB + API + Web) in parallel
dev: db-up $(DEV_JWT_SECRET_FILE)
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
	bash sql/build-schema.sh > sql/schema.sql

db-apply: db-schema
	docker run --rm -i --network host mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < $(CURDIR)/sql/schema.sql

# Create the demo/admin accounts via the helper (no password hashes in SQL),
# then load the sample calendars/events/memos that reference them by email.
db-seed: db-apply db-seed-users
	docker run --rm -i --network host mysql:8.4 \
		mysql --default-character-set=utf8mb4 -u root -prootpw -h 127.0.0.1 -P $${TC_DB_PORT:-33306} $${TC_DB_NAME:-timetree_clone} < $(CURDIR)/sql/seed.sql

# A user has no colour of its own: the colour that identifies someone on a
# calendar is per-membership, so the seed sets it in calendar_members.
#
# createuser reads the database and the workspace and nothing else, so it is
# given nothing else. The accounts it writes are what /auth/dev-login signs in
# later, but that endpoint is the server's to expose -- seeding does not need
# TC_ENV to create an @example.com account.
db-seed-users:
	cd apps/api && go run ./cmd/createuser -skip-existing \
		-email demo@example.com -password password123 -name "Demo User"
	cd apps/api && go run ./cmd/createuser -skip-existing -admin \
		-email admin@example.com -password password123 -name "Admin User"

# Code generation
sqlc:
	@bash scripts/check-codegen-drift.sh --check-tool
	cd sql && sqlc generate

# Fail when the composed schema or the sqlc output no longer matches the
# layered sources they are generated from.
verify-codegen:
	bash scripts/check-codegen-drift.sh
	bash scripts/check-error-messages.sh

# API
api: $(DEV_JWT_SECRET_FILE)
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

# The 503 for absent object storage is the one answer the storage run cannot
# observe: the test server is built once per process, so a run with MinIO has
# storage for every test in it and the tests about its absence skip. This is
# the run where they execute. They are selected by name, and the package's
# storage_absent_test.go asserts that the name, the opt-out and this selector
# never drift apart.
test-e2e-storage-absent:
	cd apps/api && TC_TEST_INTEGRATION=1 go test ./tests/e2e/ -v -count=1 -run '^TestStorageAbsent'

# Check this application's schema against the shared contract it claims to
# implement. Runs on a scratch database so it never touches dev data.
test-conformance: db-schema
	bash sql/core/conformance/run.sh \
		--dsn root:$${TC_DB_ROOT_PASSWORD:-rootpw}@127.0.0.1:$${TC_DB_PORT:-33306}/$${TC_DB_NAME:-timetree_clone} \
		--mode all

# Verify sql/core is still the upstream text, unedited and current.
check-core:
	bash scripts/check-vendored-core.sh

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

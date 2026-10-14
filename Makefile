# Local development. Nothing here deploys anything.
#
#   make setup   install JS dependencies and start the local infrastructure
#   make api     run the API with the job workers (migrates on start)
#   make web     run the web app at http://localhost:5173
#   make test    unit tests (Go + TypeScript)
#   make e2e     server integration tests against the local infrastructure

-include .env
export

.PHONY: setup infra infra-down api worker web test test-go test-ts e2e e2e-web typecheck admin

setup: infra
	pnpm install

infra:
	docker compose up -d

infra-down:
	docker compose down

api:
	cd server && go run ./cmd/api -migrate -with-worker

worker:
	cd server && go run ./cmd/worker

web:
	pnpm --filter @song-sleeve/web dev

test: test-go test-ts

test-go:
	cd server && go vet ./... && go test ./...

test-ts:
	pnpm -r test

typecheck:
	pnpm -r typecheck

e2e:
	cd server && go test -tags integration -count=1 ./internal/e2e/

# Browser tests; needs `make api` and `make web` running.
e2e-web:
	pnpm --filter @song-sleeve/web e2e

# make admin ARGS="usage owner@example.com"
admin:
	cd server && go run ./cmd/admin $(ARGS)

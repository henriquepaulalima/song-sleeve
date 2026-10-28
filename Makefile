# Local development. Nothing here deploys anything.
#
#   make setup   install JS dependencies and start the local infrastructure
#   make api     run the API with the job workers (migrates on start)
#   make web     run the web app at http://localhost:5173
#   make test    unit tests (Go + TypeScript)
#   make e2e     server integration tests against the local infrastructure
#   make desktop run the desktop app against the local API (needs `make api`)
#   make linux-packages      AppImage + .deb in dist/release
#   make try-desktop-update  a local update channel to test the AppImage updating itself

-include .env
export

.PHONY: setup infra infra-down api worker web desktop desktop-ui desktop-build linux-packages try-desktop-update test test-go test-ts e2e e2e-web typecheck admin

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

# Linux builds use WebKitGTK 4.1 (webkit2_41): Debian 13 and recent Ubuntu ship
# only that one. Needs libgtk-3-dev and libwebkit2gtk-4.1-dev.
DESKTOP_TAGS ?= webkit2_41

# Release builds set these (.github/workflows/release-desktop.yml). Without an
# update URL and public key a build never updates itself; without a Sentry DSN it
# sends no crash reports.
DESKTOP_VERSION ?= 0.1.0
DESKTOP_API ?= http://localhost:8088/api
DESKTOP_UPDATE_URL ?=
DESKTOP_UPDATE_PUBKEY ?=
DESKTOP_SENTRY_DSN ?=
DESKTOP_LDFLAGS = -s -w -X main.version=$(DESKTOP_VERSION) -X main.apiBase=$(DESKTOP_API) \
	-X main.updateURL=$(DESKTOP_UPDATE_URL) -X main.updateKey=$(DESKTOP_UPDATE_PUBKEY) \
	-X main.sentryDSN=$(DESKTOP_SENTRY_DSN)

desktop-ui:
	SONG_SLEEVE_VERSION=$(DESKTOP_VERSION) pnpm --filter @song-sleeve/desktop-ui build

desktop: desktop-ui
	cd apps/desktop && go run -tags dev,$(DESKTOP_TAGS) .

desktop-build: desktop-ui
	mkdir -p dist
	cd apps/desktop && go build -tags production,$(DESKTOP_TAGS) -trimpath -ldflags "$(DESKTOP_LDFLAGS)" -o ../../dist/song-sleeve .

# Needs dpkg-deb and curl (fetches the pinned AppImage tools once).
linux-packages: desktop-build
	rm -rf dist/release
	apps/desktop/packaging/linux/build.sh $(DESKTOP_VERSION) dist/song-sleeve dist/release

# Builds 0.1.1 in dist/try and serves 0.1.2 from a local channel; run
# dist/try/Song-Sleeve-x86_64.AppImage and watch it update (README).
try-desktop-update:
	apps/desktop/packaging/linux/try-update.sh

test: test-go test-ts

test-go:
	cd server && go vet ./... && go test ./...
	cd apps/desktop && go vet ./... && go test ./...

test-ts:
	pnpm -r test

typecheck:
	pnpm -r typecheck

e2e:
	cd server && go test -tags integration -count=1 ./internal/e2e/ ./cmd/admin/
	cd apps/desktop && go test -tags integration -count=1 ./internal/device/ ./internal/shell/
	# The tray against a stand-in tray host on a private session bus.
	cd apps/desktop && dbus-run-session -- env SONG_SLEEVE_PRIVATE_BUS=1 go test -tags integration -count=1 ./internal/tray/

# Browser tests; needs `make api` and `make web` running.
e2e-web:
	pnpm --filter @song-sleeve/web e2e

# make admin ARGS="usage owner@example.com"
admin:
	cd server && go run ./cmd/admin $(ARGS)

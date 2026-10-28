#!/bin/sh
# A local update channel for testing the AppImage updater end to end: builds
# version 0.1.1 into dist/try, version 0.1.2 into dist/try/channel with a signed
# latest.json, and serves the channel on http://127.0.0.1:8099 until Ctrl+C.
# Both stay at or above the API's default minimum desktop version (0.1.0); set
# MIN_CLIENT_VERSION_DESKTOP=0.1.2 in .env to try the update-required screen.
# Uses a throwaway key in dist/try/key, never the release key. --no-serve only builds.
set -eu
cd "$(dirname "$0")/../../../.."

TRY=dist/try
rm -rf "$TRY" && mkdir -p "$TRY/channel"
(cd apps/desktop && go run ./cmd/release keygen -out "../../$TRY/key" >/dev/null)
PUB=$(cat "$TRY/key/update-key.pub")
URL=http://127.0.0.1:8099

build() { # version outdir
  make --no-print-directory linux-packages DESKTOP_VERSION="$1" DESKTOP_UPDATE_URL="$URL/latest.json" DESKTOP_UPDATE_PUBKEY="$PUB"
  cp dist/release/* "$2/"
}
build 0.1.2 "$TRY/channel"
(cd apps/desktop && go run ./cmd/release manifest -version 0.1.2 -page "$URL/" -base "$URL" \
  -key "../../$TRY/key/update-key" -out "../../$TRY/channel/latest.json" \
  "linux-appimage=../../$TRY/channel/Song-Sleeve-x86_64.AppImage" "linux-deb=../../$TRY/channel/song-sleeve_amd64.deb")
build 0.1.1 "$TRY"
rm -f "$TRY/song-sleeve_amd64.deb"

cat <<MSG

Version 0.1.1: $TRY/Song-Sleeve-x86_64.AppImage
Channel with 0.1.2: $URL (serving now)

Run the AppImage (with \`make api\` up), open Settings, press Check for updates,
then Restart to update. Settings should then say version 0.1.2.
MSG
[ "${1:-}" = --no-serve ] && exit 0
exec python3 -m http.server 8099 --bind 127.0.0.1 -d "$TRY/channel"

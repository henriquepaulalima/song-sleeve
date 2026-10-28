#!/bin/sh
# Packages one Linux build as an AppImage and a .deb (§9.4, D-1): the same binary in
# both. Usage: build.sh VERSION BINARY OUTDIR
#
# The AppImage tools are pinned and checked by SHA-256 (the digests GitHub publishes
# for those release assets) and cached in ~/.cache/song-sleeve-build.
set -eu

VERSION=$1
BIN=$2
OUT=$3
HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

APPIMAGETOOL_URL=https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage
APPIMAGETOOL_SHA=ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0
RUNTIME_URL=https://github.com/AppImage/type2-runtime/releases/download/20251108/runtime-x86_64
RUNTIME_SHA=2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d

CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/song-sleeve-build"
mkdir -p "$CACHE" "$OUT"

fetch() { # url sha256 file
  if ! echo "$2  $3" | sha256sum -c --status 2>/dev/null; then
    curl -fsSL -o "$3.part" "$1"
    echo "$2  $3.part" | sha256sum -c --quiet
    mv "$3.part" "$3"
  fi
}
fetch "$APPIMAGETOOL_URL" "$APPIMAGETOOL_SHA" "$CACHE/appimagetool-1.9.1"
fetch "$RUNTIME_URL" "$RUNTIME_SHA" "$CACHE/runtime-20251108"
chmod +x "$CACHE/appimagetool-1.9.1"

# Files both packages install under /usr.
install_tree() { # root
  install -Dm755 "$BIN" "$1/usr/bin/song-sleeve"
  install -Dm644 "$HERE/song-sleeve.desktop" "$1/usr/share/applications/song-sleeve.desktop"
  for n in 32 48 64 128 256 512; do
    install -Dm644 "$HERE/icons/$n.png" "$1/usr/share/icons/hicolor/${n}x$n/apps/song-sleeve.png"
  done
  install -Dm644 "$HERE/icons/scalable.svg" "$1/usr/share/icons/hicolor/scalable/apps/song-sleeve.svg"
}

# .deb: the package manager owns updates; the app only shows a notice.
DEB="$WORK/deb"
install_tree "$DEB"
mkdir -p "$DEB/DEBIAN"
SIZE=$(du -sk "$DEB/usr" | cut -f1)
cat > "$DEB/DEBIAN/control" <<CONTROL
Package: song-sleeve
Version: $VERSION
Architecture: amd64
Maintainer: Song Sleeve <henriquepaulalima@users.noreply.github.com>
Installed-Size: $SIZE
Depends: libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0, gstreamer1.0-plugins-base, gstreamer1.0-plugins-good
Recommends: gstreamer1.0-libav
Section: sound
Priority: optional
Homepage: https://github.com/henriquepaulalima/song-sleeve
Description: Music player with an optional cloud library
 Plays the music on this computer and, when cloud sync is on, keeps your
 library in step with your other devices.
CONTROL
dpkg-deb --root-owner-group -Zxz --build "$DEB" "$OUT/song-sleeve_amd64.deb" >/dev/null

# AppImage: one file that runs without installing and replaces itself on update.
APPDIR="$WORK/Song_Sleeve.AppDir"
install_tree "$APPDIR"
install -m755 "$HERE/AppRun" "$APPDIR/AppRun"
cp "$HERE/song-sleeve.desktop" "$APPDIR/song-sleeve.desktop"
cp "$HERE/icons/256.png" "$APPDIR/song-sleeve.png"
ln -s song-sleeve.png "$APPDIR/.DirIcon"
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 VERSION="$VERSION" \
  "$CACHE/appimagetool-1.9.1" --no-appstream --runtime-file "$CACHE/runtime-20251108" \
  "$APPDIR" "$OUT/Song-Sleeve-x86_64.AppImage" >"$WORK/appimagetool.log" 2>&1 || { cat "$WORK/appimagetool.log"; exit 1; }

ls -l "$OUT/song-sleeve_amd64.deb" "$OUT/Song-Sleeve-x86_64.AppImage"

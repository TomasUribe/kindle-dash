#!/bin/sh
# Build the downloadable installers into dist/:
#
#   tools/build-release.sh [VERSION]
#
# Each kdash-setup-<system>.zip is one program with the Kindle program, launcher and certificates inside.
# You need Go (see app/go.mod for the version), zip and gzip. Nothing is uploaded anywhere.
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO="$ROOT/tools/go.sh"
VERSION="${1:-dev}"
OUT="$ROOT/dist"
PAY="$ROOT/app/cmd/kdash-setup/payload"
mkdir -p "$OUT"
rm -f "$OUT"/kdash-setup-* "$OUT"/SHA256SUMS

echo "1/3 Building the Kindle program (32-bit ARM) ..."
( cd "$ROOT/app" && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    "$GO" build -trimpath -ldflags "-s -w" -o "$OUT/kdash-kindle" . )
gzip -9 -n -c "$OUT/kdash-kindle" > "$PAY/kdash.gz"
( cd "$ROOT/app" && "$GO" run . -icon "$PAY/icon.png" )

echo "2/3 Building the installers ..."
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
    os="${target%/*}"; arch="${target#*/}"
    name="kdash-setup-$os-$arch"
    exe="$name"; [ "$os" = windows ] && exe="$name.exe"
    ( cd "$ROOT/app" && GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
        "$GO" build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$exe" ./cmd/kdash-setup )
    echo "   $exe"
done

echo "3/3 Packing ..."
cd "$OUT"
for f in kdash-setup-*; do
    case "$f" in *.zip) continue;; esac
    zip -q -9 -X "${f%.exe}.zip" "$f"
    rm -f "$f"
done
sha256sum kdash-setup-*.zip > SHA256SUMS
ls -l kdash-setup-*.zip | awk '{printf "  %9s  %s\n", $5, $9}'
echo "Done: $OUT"

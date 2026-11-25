#!/bin/sh
# Run Go: the project's own copy in .toolchain/ if there is one, otherwise the `go` on your PATH.
# Usage: tools/go.sh build ./...     (run from anywhere)
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export GOCACHE="${GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/kindle-dash-go-build}"
export GOFLAGS="${GOFLAGS:--buildvcs=false}"
if [ -x "$ROOT/.toolchain/go/bin/go" ]; then
    export GOROOT="$ROOT/.toolchain/go"
    export GOMODCACHE="$ROOT/.toolchain/gomod"
    export GOTOOLCHAIN=local   # never fetch another toolchain behind our back
    exec "$GOROOT/bin/go" "$@"
fi
exec go "$@"

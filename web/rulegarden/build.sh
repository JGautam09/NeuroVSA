#!/bin/sh
# Builds the RuleGarden wasm engine and copies Go's JS support shim next to it.
# Run from the repository root:  sh web/rulegarden/build.sh
set -e
GOOS=js GOARCH=wasm go build -o web/rulegarden/rulegarden.wasm ./cmd/rulegarden-wasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/rulegarden/wasm_exec.js
ls -lh web/rulegarden/rulegarden.wasm
echo "serve with:  python3 -m http.server 8090 -d web/rulegarden"

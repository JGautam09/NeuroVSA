#!/bin/sh
# Runs the dimensionality study: TestDimensionPoint compiled at each dimension, then the
# merged report (docs/DIMENSIONALITY.md) from the default build.
#
#   sh scripts/dimscan.sh
#
# Optional: fetch the standard corpora first (python3 arena/datasets/fetch.py) so the
# accuracy table includes CLINC150/Banking77 columns.
set -e
cd "$(dirname "$0")/.."

for tag in hd_d1024 hd_d2048 hd_d4096; do
  echo "== $tag =="
  DIMSCAN=1 go test -tags "$tag" -count=1 ./arena -run '^TestDimensionPoint$' -v | grep '==='
done
echo "== default (10000) =="
DIMSCAN=1 go test -count=1 ./arena -run '^TestDimensionPoint$' -v | grep '==='

echo "== report =="
DIMSCAN=1 go test -count=1 ./arena -run '^TestDimScanReport$' -v | tail -2
echo "wrote docs/DIMENSIONALITY.md"

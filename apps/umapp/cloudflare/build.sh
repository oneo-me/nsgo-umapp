#!/bin/sh
# Builds the web target into dist/web, the directory wrangler.jsonc serves.
set -eu
cd "$(dirname "$0")/.."
out=dist/web-build
rm -rf "$out" dist/web
go tool nsgo build -target web -output "$out"
mv "$out"/*-web dist/web
rm -rf "$out"

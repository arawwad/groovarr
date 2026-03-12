#!/usr/bin/env bash
set -euo pipefail

echo "Running gqlgen generate..."

# Ensure the tool is available
if ! command -v gqlgen >/dev/null 2>&1; then
  echo "gqlgen not found — installing..."
  go install github.com/99designs/gqlgen@latest
fi

gqlgen generate

echo "gqlgen generate finished. You can now build with:"
echo "  go build -tags gqlgen ./cmd/server"

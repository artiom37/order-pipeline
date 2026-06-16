#!/usr/bin/env bash
set -euo pipefail

echo "Checking required tools..."

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1"
    exit 1
  fi
}

require_cmd go
require_cmd node
require_cmd npm
require_cmd docker
require_cmd git

echo
echo "Tool versions:"
go version
node --version
npm --version
docker --version
docker compose version
git --version

echo
echo "Checking project files..."

required_files=(
  "docker-compose.yml"
  "backend/go.mod"
  "backend/Dockerfile"
  "frontend/package.json"
)

for file in "${required_files[@]}"; do
  if [ ! -f "$file" ]; then
    echo "Missing required file: $file"
    exit 1
  fi
done

echo
echo "Local environment looks ready."
echo
echo "Next steps:"
echo "  docker compose up --build"
echo
echo "Then open:"
echo "  http://localhost:5173"
#!/usr/bin/env bash
set -euo pipefail

echo "Checking tools..."
go version
node --version
npm --version
docker --version
docker compose version

echo "Initializing git repo if needed..."
if [ ! -d .git ]; then
  git init
  git add .
  git commit -m "chore: bootstrap order pipeline project"
else
  echo "Git repo already exists"
fi

echo "Done. Next: docker compose up --build"

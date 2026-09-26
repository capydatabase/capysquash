#!/bin/bash
set -e

# Change to the project root directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_ROOT"

echo "🏃 Running E2E Tests..."
echo "📂 Project Root: $PROJECT_ROOT"

# Run the tests with increased timeout for container operations
go test -v -timeout 30m ./tests/e2e/...

echo "✅ E2E Tests Complete"

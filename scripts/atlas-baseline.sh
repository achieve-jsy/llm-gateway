#!/bin/bash
set -euo pipefail

ENV=${1:-}
if [[ -z "$ENV" ]]; then
    echo "Usage: $0 <env>" >&2
    echo "  env: dev, test, prod" >&2
    exit 1
fi

# Validate DATABASE_URL is set
if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "Error: DATABASE_URL environment variable is not set" >&2
    exit 1
fi

# Get baseline version from atlas-migrations directory
BASELINE_VERSION=$(ls atlas-migrations/*.sql | head -1 | xargs basename | cut -d'_' -f1)

echo "=== Atlas Baseline Marking ==="
echo "  Environment:   $ENV"
echo "  Baseline:      $BASELINE_VERSION"
echo "  Database URL:  ${DATABASE_URL//:*@/:***@}"
echo ""
echo "Running: atlas migrate apply --baseline \"$BASELINE_VERSION\" --env \"$ENV\""
atlas migrate apply --baseline "$BASELINE_VERSION" --env "$ENV"

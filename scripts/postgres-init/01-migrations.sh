#!/usr/bin/env bash
# PostgreSQL initialization - runs migrations on first startup.
# Mounted at /docker-entrypoint-initdb.d/

set -euo pipefail

MIGRATIONS_DIR="/migrations"

echo "[postgres-init] Running migrations from ${MIGRATIONS_DIR}..."

for migration in $(ls ${MIGRATIONS_DIR}/*.up.sql | sort); do
    echo "[postgres-init] Applying ${migration}..."
    psql -v ON_ERROR_STOP=1 -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -f "${migration}"
done

echo "[postgres-init] All migrations applied."
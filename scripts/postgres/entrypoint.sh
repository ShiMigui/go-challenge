#!/bin/sh
# Entrypoint script to run migrations during postgres initialization
# This runs during database initialization (first run only)

set -e

# Wait for postgres to be ready using Unix socket
echo "Waiting for postgres to be ready..."
until pg_isready -U "${POSTGRES_USER:-wagering}" -d "${POSTGRES_DB:-wagering}" -h /var/run/postgresql; do
    echo "Waiting for postgres to be ready..."
    sleep 1
done

echo "Postgres is ready - running migrations..."

# Run migrations using golang-migrate with Unix socket
# The correct format for golang-migrate with Unix socket:
# postgres://user:pass@/dbname?host=/path/to/socket/directory&sslmode=disable
for i in 1 2 3 4 5; do
    if migrate \
        -path /migrations \
        -database "postgres://${POSTGRES_USER:-wagering}:${POSTGRES_PASSWORD:-wagering}@/${POSTGRES_DB:-wagering}?host=/var/run/postgresql&sslmode=disable" \
        up; then
        echo "Migrations completed successfully!"
        exit 0
    fi
    echo "Migration attempt $i failed, retrying in 2 seconds..."
    sleep 2
done

echo "Migrations failed after 5 attempts"
exit 1
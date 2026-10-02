#!/bin/sh
set -e

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to become ready..."
until pg_isready -h postgres -U wagering -d wagering; do
    echo "PostgreSQL is unavailable - sleeping"
    sleep 1
done

echo "PostgreSQL is ready - running migrations..."

# Apply migrations
migrate \
    -path /migrations \
    -database "postgres://wagering:wagering@postgres:5432/wagering?sslmode=disable" up

echo "Migrations completed successfully!"
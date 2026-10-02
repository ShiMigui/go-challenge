#!/bin/sh
set -e

export PGPASSWORD=wagering

# Wait for PostgreSQL to be ready
until pg_isready -h postgres -U wagering -d wagering; do
  echo "waiting for postgres..."
  sleep 1
done

echo "applying migrations..."
for f in /migrations/*.up.sql; do
  echo "running $f"
  psql -h postgres -U wagering -d wagering -f "$f" || exit 1
done

echo "migrations completed"
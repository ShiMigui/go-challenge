# Custom Postgres image with migrations baked in
# Usage: docker build -t wagering-postgres:latest -f scripts/postgres/Dockerfile .
# This image has all migrations applied and ready to use.

ARG PG_VERSION=17-alpine
FROM postgres:${PG_VERSION}

# Install golang-migrate and gosu for running as postgres user
RUN apk add --no-cache \
    curl \
    tar \
    gosu \
    && curl -fsSL https://github.com/golang-migrate/migrate/releases/download/v4.20.1/migrate.linux-amd64.tar.gz | tar xzf - -C /usr/local/bin migrate

# Copy migration files
COPY migrations/ /migrations/

# Copy entrypoint that runs migrations on first init
COPY entrypoint.sh /docker-entrypoint-initdb.d/00-run-migrations.sh

# The standard postgres entrypoint will run scripts in /docker-entrypoint-initdb.d/
# during database initialization (first run only)
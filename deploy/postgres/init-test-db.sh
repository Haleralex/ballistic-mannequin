#!/bin/sh
# Runs once, when the dev Postgres volume is created: adds a second database for `go test`
# (TEST_DATABASE_URL), so the tests never touch the data `make dev` works with.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<SQL
CREATE DATABASE ${POSTGRES_DB}_test OWNER "$POSTGRES_USER";
SQL

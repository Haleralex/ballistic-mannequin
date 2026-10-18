# Dev container: Go 1.24 (Debian bookworm, user vscode), Node 22 and the Postgres client for `make db-shell`.
# Node is copied from the official image rather than added as a devcontainer feature: one registry (Docker Hub)
# fewer to depend on, and the same Node as CI. Chromium for the browser tests comes from post-create.sh.
FROM node:22-bookworm-slim AS node

FROM mcr.microsoft.com/devcontainers/go:1.24-bookworm
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && apt-get update \
 && apt-get install -y --no-install-recommends postgresql-client \
 && rm -rf /var/lib/apt/lists/*

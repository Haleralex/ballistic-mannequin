# Caddy as a non-root user. The stock image runs as root and its /data and /config (certificates, ACME account)
# are root-owned, so a fresh named volume would not be writable for another user. Here they belong to caddy:caddy,
# and Docker copies that ownership into the empty volumes on first start.
# The Caddyfile is mounted by compose.prod.yml, so editing it needs no rebuild.
# Ports 80/443 inside the container need no capability: Docker sets net.ipv4.ip_unprivileged_port_start=0.
# The stock binary carries the file capability cap_net_bind_service; with cap_drop: [ALL] (compose.prod.yml) exec of
# such a binary fails with "operation not permitted", so the capability is removed.
ARG CADDY_VERSION=2.11
FROM caddy:${CADDY_VERSION}-alpine
RUN apk add --no-cache libcap && setcap -r /usr/bin/caddy && apk del libcap \
 && addgroup -S -g 1000 caddy && adduser -S -D -H -u 1000 -G caddy caddy \
 && mkdir -p /data/caddy /config/caddy \
 && chown -R caddy:caddy /data /config
USER caddy:caddy

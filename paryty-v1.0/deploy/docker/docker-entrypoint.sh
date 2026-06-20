#!/bin/sh
# Paryty Frontend - Docker entrypoint
# Resolves backend service DNS names at container startup and
# substitutes them into the nginx configuration via envsubst.

set -e

# Default backend URLs (Docker Compose service names)
export PARYTY_API_URL="${PARYTY_API_URL:-http://query:8081}"
export PARYTY_WS_URL="${PARYTY_WS_URL:-http://query:8081}"

# Substitute environment variables into nginx config
envsubst '${PARYTY_API_URL} ${PARYTY_WS_URL}' \
    < /etc/nginx/conf.d/default.conf \
    > /etc/nginx/conf.d/default.conf.tmp \
    && mv /etc/nginx/conf.d/default.conf.tmp /etc/nginx/conf.d/default.conf

echo "[paryty-frontend] API upstream: ${PARYTY_API_URL}"
echo "[paryty-frontend] WS  upstream: ${PARYTY_WS_URL}"

# Execute the CMD (nginx)
exec "$@"

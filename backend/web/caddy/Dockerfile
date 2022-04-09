FROM abiosoft/caddy

LABEL "com.datadoghq.ad.logs"='[{"source": "caddy", "service": "web"}]'

COPY backend/web/caddy/Caddyfile /etc/
COPY backend/web/landing/www/ /srv/landing

FROM abiosoft/caddy

LABEL "com.datadoghq.ad.logs"='[{"source": "caddy", "service": "web"}]'

COPY web/caddy/Caddyfile /etc/
COPY web/landing/www/ /srv/landing

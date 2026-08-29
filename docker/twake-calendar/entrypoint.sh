#!/bin/sh
set -e

mkdir -p /etc/nginx/conf.d

# Generate .env.js at runtime from environment variables
cat <<EOF > /usr/share/nginx/html/.env.js
var CALENDAR_BASE_URL = '${CALENDAR_BASE_URL:-http://localhost:3334}';
var DAV_BASE_URL = '${DAV_BASE_URL:-http://localhost:8088/remote.php/dav}';
var MAIL_SPA_URL = '${MAIL_SPA_URL:-http://localhost:3333}';
var CALDAV_PREFER_HANDLING = '${CALDAV_PREFER_HANDLING:-strict}';
var DEBUG = ${DEBUG:-false};
var LANG = '${LANG:-en}';
var ENABLE_REFRESH_BUTTON = true;
var DISABLE_PUBLIC_VISIBILITY = false;
var ASK_FOR_TZ_UPDATE = true;
EOF

# Ensure appList.js exists so browser does not trigger fallback banner
if [ ! -f /usr/share/nginx/html/appList.js ]; then
  if [ -f /usr/share/nginx/html/appList.example.js ]; then
    cp /usr/share/nginx/html/appList.example.js /usr/share/nginx/html/appList.js
  else
    echo "var appList = [];" > /usr/share/nginx/html/appList.js
  fi
fi

echo "map \$uri \$static_cache_control { default \"public, max-age=31536000, immutable\"; }" > /etc/nginx/conf.d/cache_env.conf
echo "map \$uri \$html_cache_control { default \"no-store, no-cache, must-revalidate, proxy-revalidate\"; }" >> /etc/nginx/conf.d/cache_env.conf

exec nginx -g "daemon off;"

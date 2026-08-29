#!/bin/sh
set -e

mkdir -p /etc/nginx/conf.d

# Generate .env.js at runtime only if missing or writable
if [ ! -f /usr/share/nginx/html/.env.js ] || [ -w /usr/share/nginx/html/.env.js ]; then
  cat <<EOF > /usr/share/nginx/html/.env.js 2>/dev/null || true
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
fi

# Generate appList.js dynamically from environment variables if not mounted as read-only volume
if [ ! -f /usr/share/nginx/html/appList.js ] || [ -w /usr/share/nginx/html/appList.js ]; then
  if [ -n "$APP_LIST" ]; then
    echo "var appList = $APP_LIST;" > /usr/share/nginx/html/appList.js 2>/dev/null || true
  else
    MAIL_URL="${MAIL_SPA_URL:-http://localhost:3333}"
    DRIVE_URL="${DRIVE_URL:-http://localhost:8088}"
    cat <<EOF > /usr/share/nginx/html/appList.js 2>/dev/null || true
var appList = [
  {
    name: 'Mail',
    link: '${MAIL_URL}',
    icon: '/assets/images/svg/app-mail.svg'
  },
  {
    name: 'Drive',
    link: '${DRIVE_URL}',
    icon: '/assets/images/svg/app-drive.svg'
  }
];
EOF
  fi
fi

# Apply cache-busting query strings in index.html to bust intermediary/browser caches
if [ -w /usr/share/nginx/html/index.html ]; then
  sed -i 's|/appList\.js[^"]*|/appList.js?v=3|g' /usr/share/nginx/html/index.html 2>/dev/null || true
  sed -i 's|/\.env\.js[^"]*|/.env.js?v=3|g' /usr/share/nginx/html/index.html 2>/dev/null || true
  sed -i 's|/version\.js[^"]*|/version.js?v=3|g' /usr/share/nginx/html/index.html 2>/dev/null || true
  # Regenerate gzipped index.html if gzip exists
  if command -v gzip >/dev/null 2>&1; then
    gzip -k -f /usr/share/nginx/html/index.html 2>/dev/null || true
  fi
fi

echo "map \$uri \$static_cache_control { default \"public, max-age=31536000, immutable\"; }" > /etc/nginx/conf.d/cache_env.conf 2>/dev/null || true
echo "map \$uri \$html_cache_control { default \"no-store, no-cache, must-revalidate, proxy-revalidate\"; }" >> /etc/nginx/conf.d/cache_env.conf 2>/dev/null || true

exec nginx -g "daemon off;"

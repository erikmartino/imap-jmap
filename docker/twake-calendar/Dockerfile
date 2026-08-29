FROM alpine/git:latest AS cloner
WORKDIR /src
RUN git clone --depth 1 https://github.com/linagora/twake-calendar-frontend.git .

FROM node:22-alpine AS builder
WORKDIR /app
COPY --from=cloner /src ./
RUN npm ci
RUN npm run build:private

FROM nginx:alpine
RUN apk add --no-cache gzip

COPY --from=cloner /src/nginx.conf /etc/nginx/
COPY --from=builder /app/apps/private/dist /usr/share/nginx/html
ARG BUILD_VERSION=2.0
RUN echo "var APP_VERSION = \"${BUILD_VERSION}\";" > /usr/share/nginx/html/version.js && \
    cp /usr/share/nginx/html/appList.example.js /usr/share/nginx/html/appList.js
RUN find /usr/share/nginx/html -type f ! -name '.env.js' ! -name 'version.js' ! -name 'appList.js' -exec gzip -k -f {} \;

RUN mkdir -p /etc/nginx/conf.d && \
    echo "map \$uri \$static_cache_control { default \"public, max-age=31536000, immutable\"; }" > /etc/nginx/conf.d/cache_env.conf && \
    echo "map \$uri \$html_cache_control { default \"no-store, no-cache, must-revalidate, proxy-revalidate\"; }" >> /etc/nginx/conf.d/cache_env.conf

COPY docker/twake-calendar/entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]

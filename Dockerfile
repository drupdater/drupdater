ARG PHP_VERSION=8.3

# Build go binary.
FROM golang:1.24.2-bookworm AS build

RUN mkdir -p /build/

# Copy full source code.
COPY . /build/

WORKDIR /build/

RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

RUN --mount=type=cache,target=/gomod-cache go mod download
RUN --mount=type=cache,target=/gomod-cache --mount=type=cache,target=/go-cache GOOS=linux go build -o /build/drupdater .


# Build php image.
FROM php:${PHP_VERSION}-cli-bookworm AS base

COPY --from=ghcr.io/mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/
RUN install-php-extensions pdo_mysql gd zip imagick intl

RUN apt-get update \
    && apt-get install -y --no-install-recommends git unzip patch sqlite3 \
    && rm -rf /var/lib/apt/lists/*;

ENV COMPOSER_HOME=/usr/local/composer
ENV COMPOSER_CACHE_DIR=/tmp/composer/cache
ENV COMPOSER_ALLOW_SUPERUSER=1
ENV COMPOSER_NO_AUDIT=1
ENV COMPOSER_FUND=0
ENV COMPOSER_PROCESS_TIMEOUT=0
COPY --from=composer:2 /usr/bin/composer /usr/local/bin/composer

# Add mglaman/composer-drupal-lenient as a global composer plugin.
RUN composer global config --no-plugins allow-plugins.mglaman/composer-drupal-lenient true; \
    composer global config --no-plugins allow-plugins.ion-bazan/composer-diff true; \
    composer global require mglaman/composer-drupal-lenient ion-bazan/composer-diff;

RUN git config --global user.email "update@drupdater.com" && \
    git config --global user.name "Drupdater"

COPY scripts/ /opt/drupdater/
COPY --from=build /build/drupdater /opt/drupdater/bin

CMD [""]
ENTRYPOINT ["/opt/drupdater/bin"]

ARG PHP_VERSION=8.3

# Pinned to a minor, not `composer:2`: patches carry fixes, minors change defaults, and composer
# decides what a run does -- so a minor bump is a deliberate commit CI runs against.
# Named stages because dependabot reads FROM, not COPY --from.
FROM composer:2.10 AS composer
FROM ghcr.io/mlocati/php-extension-installer:2 AS php-extension-installer

# Build go binary.
FROM golang:1.26.5-trixie AS build

# Without this the images report drupdater_version "dev". Keep in step with the Makefile.
ARG VERSION=dev

RUN mkdir -p /build/

# Copy full source code.
COPY . /build/

WORKDIR /build/

RUN go env -w GOCACHE=/go-cache; \
    go env -w GOMODCACHE=/gomod-cache

RUN --mount=type=cache,target=/gomod-cache go mod download
RUN --mount=type=cache,target=/gomod-cache --mount=type=cache,target=/go-cache GOOS=linux go build \
    -ldflags "-X github.com/drupdater/drupdater/internal.Version=${VERSION}" -o /build/drupdater .


# Build php image.
FROM php:${PHP_VERSION}-cli-trixie AS base

RUN echo "memory_limit = -1" > "$PHP_INI_DIR/conf.d/memory-limit.ini"

COPY --from=php-extension-installer /usr/bin/install-php-extensions /usr/local/bin/
RUN install-php-extensions pdo_mysql gd zip Imagick/imagick@3.8.1 intl

RUN apt-get update \
    && apt-get install -y --no-install-recommends git unzip patch sqlite3 \
    && rm -rf /var/lib/apt/lists/*;

ENV COMPOSER_HOME=/usr/local/composer
ENV COMPOSER_CACHE_DIR=/tmp/composer/cache
ENV COMPOSER_ALLOW_SUPERUSER=1
ENV COMPOSER_NO_AUDIT=1
ENV COMPOSER_FUND=0
ENV COMPOSER_PROCESS_TIMEOUT=0
COPY --from=composer /usr/bin/composer /usr/local/bin/composer

# Add mglaman/composer-drupal-lenient as a global composer plugin.
RUN composer global config --no-plugins allow-plugins.mglaman/composer-drupal-lenient true; \
    composer global config --no-plugins allow-plugins.ion-bazan/composer-diff true; \
    composer global require mglaman/composer-drupal-lenient ion-bazan/composer-diff;

COPY scripts/ /opt/drupdater/
COPY --from=build /build/drupdater /opt/drupdater/bin

CMD [""]
ENTRYPOINT ["/opt/drupdater/bin"]

ARG PHP_VERSION=8.3

# Build go binary.
FROM golang:bookworm AS build

RUN mkdir -p /build/

# Copy full source code.
COPY . /build/

WORKDIR /build/

RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

RUN --mount=type=cache,target=/gomod-cache go mod download
RUN --mount=type=cache,target=/gomod-cache --mount=type=cache,target=/go-cache GOOS=linux go build -o /build/drupdater ./cmd/updater


# Build php image.
FROM php:${PHP_VERSION}-cli-bookworm AS base

COPY --from=ghcr.io/mlocati/php-extension-installer:2 /usr/bin/install-php-extensions /usr/local/bin/
RUN install-php-extensions pdo_mysql gd zip imagick

RUN apt-get update \
    && apt-get install -y --no-install-recommends git unzip patch \
    && rm -rf /var/lib/apt/lists/*;

ENV COMPOSER_ALLOW_SUPERUSER=1
ENV COMPOSER_NO_AUDIT=1
ENV COMPOSER_FUND=0
COPY --from=composer:2 /usr/bin/composer /usr/local/bin/composer

# Add mglaman/composer-drupal-lenient as a global composer plugin.
RUN composer global config --no-plugins allow-plugins.mglaman/composer-drupal-lenient true; \
    composer global config --no-plugins allow-plugins.ion-bazan/composer-diff true; \
    composer global require mglaman/composer-drupal-lenient ion-bazan/composer-diff

RUN git config --global user.email "update@drupdater.com" && \
    git config --global user.name "Drupdater"

CMD [ "" ]
ENTRYPOINT [ "" ]


# Production image.
FROM base AS prod
COPY --from=build /build/drupdater /opt/drupdater


# Development image.
FROM base AS dev

# Install go.
RUN apt-get update \
    && apt-get install -y --no-install-recommends golang \
    && rm -rf /var/lib/apt/lists/*;
RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

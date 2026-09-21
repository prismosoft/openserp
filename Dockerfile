# Build
FROM --platform=$BUILDPLATFORM golang:1.24.6-bookworm@sha256:ab1d1823abb55a9504d2e3e003b75b36dbeb1cbcc4c92593d85a84ee46becc6c AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /app/openserp .

# `chromedp/headless-shell:stable` also works here
FROM chromedp/headless-shell:stable@sha256:f7e7ac721b023cb8717f8108aef8b3e49995fb1e5a912f41e570c29e45d24961

WORKDIR /usr/src/app

# ca-certificates: REQUIRED. The server binary is built CGO_ENABLED=0, so Go
# uses its own TLS stack and reads the trust store off disk. This base image
# ships no CA bundle at any path Go looks in (/etc/ssl/certs is empty), so
# without this every outbound HTTPS call from Go fails with
# "x509: certificate signed by unknown authority" — which is what silently
# broke 2captcha solving. Chrome is unaffected because it carries its own
# roots, so the browser-driven engines keep working and hide the problem.
# wget: used by HEALTHCHECK (localhost, no TLS).
# dumb-init: already provided by `docker run --init` / compose `init: true`,
# so we do NOT add tini here — the PID1 reaper is supplied by the runtime.
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates wget \
  && rm -rf /var/lib/apt/lists/* \
  && getent passwd chrome >/dev/null 2>&1 || useradd --create-home --uid 1001 --shell /bin/bash chrome \
  && chown chrome:chrome /usr/src/app \
  && echo '#!/bin/sh\nexec /headless-shell/headless-shell --no-sandbox "$@"' > /usr/local/bin/headless-wrapper \
  && chmod +x /usr/local/bin/headless-wrapper

COPY --from=builder /app/openserp /usr/local/bin/openserp
COPY --chown=chrome:chrome config.yaml ./config.yaml

# Rod's launcher.LookPath does not know about /headless-shell/headless-shell.
# Viper auto-binds OPENSERP_APP_BROWSER_PATH to app.browser_path.
# Railway's container runtime can hit Chromium sandbox permission errors
# when running as this non-root user, so browser launches go through
# a wrapper that disables Chromium's Linux sandbox.
ENV OPENSERP_APP_BROWSER_PATH=/usr/local/bin/headless-wrapper \
  OPENSERP_SERVER_HOST=0.0.0.0 \
  OPENSERP_SERVER_PORT=7000

USER chrome

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget --quiet --tries=1 --spider "http://127.0.0.1:${OPENSERP_SERVER_PORT}/health" || exit 1

ENTRYPOINT ["openserp"]
CMD ["serve"]

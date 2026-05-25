# syntax=docker/dockerfile:1.7

# --- build stage -----------------------------------------------------------
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# Cache deps separately from sources so dep-only changes don't bust the
# whole layer cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, stripped, reproducible-ish build.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/server \
        .

# --- runtime stage ---------------------------------------------------------
# FROM scratch: nothing but the binary. No shell, no package manager, no CVEs
# in things we don't use. When M3 adds outbound HTTPS (Open-Meteo) we'll need
# to COPY ca-certificates from the builder; not required yet.
FROM scratch

COPY --from=builder /out/server /server

# Run as nobody:nogroup. The server doesn't need root for anything.
USER 65534:65534

# Documentation only; the actual port is whatever PORT env var is set to.
EXPOSE 8080

ENTRYPOINT ["/server"]

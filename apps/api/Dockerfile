# syntax=docker/dockerfile:1.7
# Multi-stage build for the nodate-time Go API.
# The build context is the repository root.

FROM golang:1.27-alpine AS build
WORKDIR /src

# Manifests first so a source-only change reuses the download layer.
COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY apps/api .

# createuser ships in the same image as the server. Creating the first
# administrator is a deployment step, and a deployment that has only the
# server has no way to perform it.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/createuser ./cmd/createuser && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY --from=build /out/createuser /createuser
COPY --from=build /out/healthcheck /healthcheck
USER nonroot:nonroot
# 8080 serves the product; 9464 serves metrics, profiles and the detailed
# health report and is expected to stay inside the host or the compose network.
EXPOSE 8080 9464
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=5 \
    CMD ["/healthcheck"]
ENTRYPOINT ["/api"]

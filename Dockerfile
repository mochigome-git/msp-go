# Stage 1: Build the Go program
FROM golang:1.26.5-alpine AS builder

# Accept platform args from buildx
ARG TARGETOS
ARG TARGETARCH

# Configure Go environment for static build
ENV CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH

WORKDIR /app

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

RUN go build -trimpath -ldflags="-s -w" -o main ./cmd/

# --- Stage 2: Runtime ---
# Static binary, no shell, runs as uid 65532 (nonroot).
# Mounted certificates must be readable by that user.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/main /app/main

WORKDIR /app
USER nonroot:nonroot

ENTRYPOINT ["/app/main"]

# Multi-arch build:
#   docker buildx create --use
#   docker buildx build --platform linux/amd64,linux/arm64 -t mochigome/msp-go:<version> --push .
#
# Single-arch build:
#   docker build -t mochigome/msp-go:<version> .
#   docker push mochigome/msp-go:<version>
#
# AWS ECR: see script/deploy-ecr.sh

# Stage 1: Builder
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/daemon ./cmd/daemon

# Stage 2: Final Image (Non-Root)
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN groupadd -r appgroup && useradd -r -g appgroup -m -d /app -s /sbin/nologin appuser

WORKDIR /app

COPY --from=builder /app/bin/daemon /app/daemon

RUN chown -R appuser:appgroup /app && chmod -R 755 /app

USER appuser

ENTRYPOINT ["/app/daemon"]

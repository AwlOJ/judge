# --------- Stage 1: Build Go App ---------
FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the main daemon for a Linux environment
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Final Runtime Image ---------
FROM ubuntu:22.04

WORKDIR /app

# Install runtime dependencies: compilers, firejail, and trusted CAs
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    g++ \
    python3 \
    firejail \
    ca-certificates

# Copy the compiled Go application from the 'builder' stage
COPY --from=builder /app/bin/daemon /app/daemon

# Set the entrypoint
CMD ["/app/daemon"]

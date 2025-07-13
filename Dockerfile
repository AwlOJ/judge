# --------- Stage 1: Build Go App ---------
FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the main daemon for a Linux environment
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/bin/daemon ./cmd/daemon/main.go \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/bin/unsupported ./internal/runner/sandbox_unsupported.go

# --------- Stage 2: Final Runtime Image ---------
FROM ubuntu:22.04

WORKDIR /app

# Install runtime dependencies: compilers and firejail
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    g++ \
    python3 \
    firejail

# Create a dummy source file for the ldd check
RUN echo 'int main() { return 0; }' > /tmp/main.cpp
# Compile it
RUN g++ /tmp/main.cpp -o /tmp/main.out -O2 -static -Wall
#
# --- DIAGNOSTIC STEP ---
# Run ldd to check for dynamic dependencies. This will show us if "-static" is truly static.
# If this command shows anything other than "not a dynamic executable", our theory is correct.
RUN echo "--- Running ldd on the static executable ---" && ldd /tmp/main.out || true
#
#

# Copy the compiled Go application from the 'builder' stage
COPY --from=builder /app/bin/daemon /app/daemon

# Set the entrypoint
CMD ["/app/daemon"]

# Stage 1: Builder
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application for a non-root environment
# The output binary is statically linked and doesn't depend on C libraries (CGO_ENABLED=0)
# This makes it portable and suitable for a minimal final image.
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/daemon ./cmd/daemon

# Stage 2: Final Image (Non-Root)
FROM ubuntu:22.04

# Install only necessary runtime dependencies for the judger
# We are removing firejail as it's not used anymore.
# ca-certificates is needed for making HTTPS requests (if any).
# build-essential contains compilers like g++, which are needed for the Compile step.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# Create a dedicated user and group for the application
RUN groupadd -r appgroup && useradd -r -g appgroup -m -d /app -s /sbin/nologin appuser

# Set the working directory
WORKDIR /app

# Copy the application binary from the builder stage
COPY --from=builder /app/bin/daemon /app/daemon

# Change ownership of the app directory to the new user.
# This is crucial so the application can write files if needed (e.g., logs).
# We also ensure /tmp is writable, which is default but good to be explicit.
RUN chown -R appuser:appgroup /app && chmod -R 755 /app

# Switch to the non-root user
USER appuser

# Set the entrypoint for the container
ENTRYPOINT ["/app/daemon"]

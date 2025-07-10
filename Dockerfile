# ================================
# Builder stage: Build Go daemon
# ================================
FROM golang:1.22 AS judge_go_builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build Go binary (no CGO)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# ================================
# Final stage: Runtime with Isolate
# ================================
FROM ubuntu:20.04

# Prevent interactive prompts during build (e.g., tzdata)
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies and Isolate via apt
RUN apt-get update && \
    apt-get install -y curl gnupg software-properties-common && \
    mkdir -p /etc/apt/keyrings && \
    curl https://www.ucw.cz/isolate/debian/signing-key.asc | tee /etc/apt/keyrings/isolate.asc && \
    echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/isolate.asc] http://www.ucw.cz/isolate/debian/ bookworm-isolate main" > /etc/apt/sources.list.d/isolate.list && \
    apt-get update && \
    apt-get install -y isolate && \
    rm -rf /var/lib/apt/lists/*

# Add the judge binary from builder
WORKDIR /app
COPY --from=judge_go_builder /app/bin/daemon ./judge

# ENV (update as needed)
ENV MONGO_URI=none
ENV REDIS_URI=none

CMD ["./judge"]

#iukhuyen:333333
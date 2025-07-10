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

WORKDIR /app

# Cài các gói cần thiết
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    gcc g++ make \
    python3 python3-pip \
    git curl ca-certificates \
    libcap-dev libsystemd-dev \
    pkg-config asciidoc

# Cài isolate
RUN git clone https://github.com/ioi/isolate.git /tmp/isolate && \
    cd /tmp/isolate && \
    make && make install && \
    cd / && rm -rf /tmp/isolate

# Copy binary từ builder
COPY --from=judge_go_builder /app/bin/daemon ./judge

# Environment (nếu có)
ENV MONGO_URI=none

# Run command
ENTRYPOINT ["./judge"]

#iukhuyen:333333
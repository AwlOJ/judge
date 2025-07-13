# --------- Stage 1: Build Go App ---------
FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Build nsjail ---------
FROM ubuntu:22.04 AS nsjail_builder

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    git \
    g++ \
    make \
    flex \
    bison \
    libprotobuf-dev \
    protobuf-compiler \
    libnl-3-dev \
    libnl-route-3-dev \
    pkg-config

RUN git clone --depth 1 https://github.com/google/nsjail.git /nsjail_src
WORKDIR /nsjail_src
RUN make

# --------- Stage 3: Runtime ---------
FROM ubuntu:22.04

WORKDIR /app

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    g++ \
    python3 \
    time \
    ca-certificates

COPY --from=builder /app/bin/daemon /app/daemon

COPY --from=nsjail_builder /nsjail_src/nsjail /usr/local/bin/nsjail

# Copy runtime assets (if any)
# COPY --from=builder /app/config /app/config

CMD ["/app/daemon"]

#iukuyenn&haanhh:3
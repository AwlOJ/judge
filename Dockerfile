# --------- Stage 1: Build Judge Service Go application ---------
FROM golang:1.22 AS judge_go_builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Runtime for Judge Service (with isolate) ---------
FROM ubuntu:20.04

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    gcc \
    g++ \
    python3 \
    git \
    && rm -rf /var/lib/apt/lists/*

# Build isolate từ GitHub
RUN git clone https://github.com/ioi/isolate.git /tmp/isolate && \
    cd /tmp/isolate && \
    make && \
    make install && \
    cd / && rm -rf /tmp/isolate

COPY --from=judge_go_builder /app/bin/daemon ./judge

ENV MONGO_URI=none
ENV REDIS_URL=none

ENTRYPOINT ["./judge"]


#iukhuyen:333333
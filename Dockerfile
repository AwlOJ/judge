# --------- Stage 1: Build Go binary ---------
FROM golang:1.22 AS judge_go_builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Runtime with isolate ---------
FROM ubuntu:20.04

ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    gcc \
    g++ \
    python3 \
    git \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Nếu vẫn lỗi thì uncomment dòng này:
# RUN git config --global http.sslVerify false

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
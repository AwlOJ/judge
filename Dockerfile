# --------- Stage 1: Build Judge Service Go application ---------
FROM golang:1.22 AS judge_go_builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Runtime for Judge Service (with isolate) ---------
FROM ubuntu:latest

WORKDIR /app

# Enable universe repository and install isolate and necessary compilers/interpreters
RUN apt-get update && apt-get install -y --no-install-recommends software-properties-common
RUN add-apt-repository universe
RUN apt-get update && apt-get install -y --no-install-recommends \
    isolate \
    gcc \
    g++ \
    python3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=judge_go_builder /app/bin/daemon ./judge

ENV MONGO_URI=none
ENV REDIS_URL=none

ENTRYPOINT ["./judge"]

#iukhuyen:333333
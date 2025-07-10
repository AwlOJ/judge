# --------- Stage 1: Build ---------
FROM golang:1.22 AS awloj-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Runtime ---------
FROM ubuntu:20.04

WORKDIR /app

RUN mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://www.ucw.cz/isolate/debian/signing-key.asc -o /etc/apt/keyrings/isolate.asc && \
    echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/isolate.asc] http://www.ucw.cz/isolate/debian/ focal-isolate main" > /etc/apt/sources.list.d/isolate.list

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    isolate \
    gcc g++ make \
    python3 python3-pip \
    curl ca-certificates \
    libcap-dev libsystemd-dev pkg-config

COPY --from=awloj-builder /app/bin/daemon ./judge

ENTRYPOINT ["./judge"]

#iukhuyen:333333
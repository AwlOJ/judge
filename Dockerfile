# --------- Stage 1: Build ---------
FROM golang:1.22 AS awloj-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/daemon ./cmd/daemon/main.go

# --------- Stage 2: Runtime ---------
FROM ubuntu:22.04

WORKDIR /app

# Ensure /run/isolate directory exists and is writable for isolate to manage cgroups
RUN mkdir -p /run/isolate && chmod 777 /run/isolate

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates

RUN mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://www.ucw.cz/isolate/debian/signing-key.asc -o /etc/apt/keyrings/isolate.asc && \
    echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/isolate.asc] http://www.ucw.cz/isolate/debian/ jammy-isolate main" > /etc/apt/sources.list.d/isolate.list

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    isolate \
    gcc g++ make \
    python3 python3-pip \
    libcap-dev libsystemd-dev pkg-config

COPY --from=awloj-builder /app/bin/daemon ./judge

ENV MONGO_URI=mongodb+srv://longathelstan:tlowngxhaanh@online-judge.wexmq0b.mongodb.net/
ENV MONGO_DB_NAME=test
ENV REDIS_URL=redis://default:49kjkndd@dbprovider.ap-southeast-1.clawcloudrun.com:30399
ENV REDIS_QUEUE_NAME=submission_queue

ENTRYPOINT ["./judge"]

#iukhuyen:333333
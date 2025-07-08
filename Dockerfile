# --------- Stage 1: Build ---------
FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o judge ./cmd/daemon/main.go

# --------- Stage 2: Runtime ---------
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/judge .

RUN apk --no-cache add ca-certificates

# environment
ENV MONGO_URI=none
ENV REDIS_URL=none

ENTRYPOINT ["./judge"]

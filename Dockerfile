FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download


COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o boltcache .


FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/boltcache ./boltcache

# Default config 
COPY config.yaml ./config.yaml

# Data directory
RUN mkdir -p /app/data

EXPOSE 6380

# command
ENTRYPOINT ["./boltcache"]
CMD ["server", "--config", "config.yaml"]

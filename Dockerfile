FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /autoresearch ./cmd/autoresearch

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /autoresearch /usr/local/bin/autoresearch
ENTRYPOINT ["autoresearch"]

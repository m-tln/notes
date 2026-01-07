FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates openssl

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags='-w -s' -o ca-service .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata openssl && \
    update-ca-certificates

RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/ca-service .

RUN mkdir -p /certs && chown -R appuser:appuser /certs /app

USER appuser

VOLUME ["/certs"]

CMD ["./ca-service"]
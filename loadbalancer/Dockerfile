FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o loadbalancer main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata openssl && \
    update-ca-certificates

RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/loadbalancer .

RUN mkdir -p /certs && chown -R appuser:appuser /app /certs

USER appuser

EXPOSE 80 443

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider https://localhost/health --no-check-certificate || exit 1

CMD ["./loadbalancer"]
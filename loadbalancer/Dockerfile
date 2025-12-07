FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o loadbalancer main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/loadbalancer .
EXPOSE 80
CMD ["./loadbalancer"]
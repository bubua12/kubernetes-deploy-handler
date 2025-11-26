FROM golang:1.25 AS builder

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o controller ./cmd/controller

FROM alpine:3
WORKDIR /app
COPY --from=builder /app/controller .

CMD ["./controller"]
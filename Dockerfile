FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o latasya-erp ./cmd/server

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Jakarta

RUN adduser -D -h /app appuser
WORKDIR /app
COPY --from=builder /app/latasya-erp .
RUN chown -R appuser:appuser /app

USER appuser
EXPOSE 8080

CMD ["./latasya-erp"]

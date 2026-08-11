FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY . .

ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_TIME=""

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X imap-jmap/jmap.Version=${VERSION} -X imap-jmap/jmap.Commit=${COMMIT} -X imap-jmap/jmap.BuildTime=${BUILD_TIME}" \
    -o imap-jmap main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/imap-jmap .

EXPOSE 8080

ENTRYPOINT ["./imap-jmap"]

FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY . .

ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_TIME=""

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X imap-jmap/jmap.Version=${VERSION} -X imap-jmap/jmap.Commit=${COMMIT} -X imap-jmap/jmap.BuildTime=${BUILD_TIME}" \
    -o imap-jmap main.go && \
    CGO_ENABLED=0 GOOS=linux go build -o /mock-ldap ./cmd/mock-ldap

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/imap-jmap .
COPY --from=builder /mock-ldap /mock-ldap

EXPOSE 8080

ENTRYPOINT ["./imap-jmap"]


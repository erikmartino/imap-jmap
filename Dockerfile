FROM golang:1.25-alpine AS builder

WORKDIR /app

# The third_party copy of ldapserver is required by the replace directive in
# go.mod, so it must be present before `go mod download`.
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_TIME=""

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X imap-jmap/jmap.Version=${VERSION} -X imap-jmap/jmap.Commit=${COMMIT} -X imap-jmap/jmap.BuildTime=${BUILD_TIME}" \
    -o imap-jmap main.go && \
    CGO_ENABLED=0 GOOS=linux go build -o /mock-ldap ./cmd/mock-ldap && \
    CGO_ENABLED=0 GOOS=linux go build -o /mock-smtp ./cmd/mock-smtp

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/imap-jmap .
COPY --from=builder /mock-ldap /mock-ldap
COPY --from=builder /mock-smtp /mock-smtp


EXPOSE 8080

ENTRYPOINT ["./imap-jmap"]

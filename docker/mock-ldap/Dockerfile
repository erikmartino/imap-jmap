FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /mock-ldap ./cmd/mock-ldap

FROM alpine:latest
EXPOSE 389 8080
COPY --from=builder /mock-ldap /mock-ldap
ENTRYPOINT ["/mock-ldap"]

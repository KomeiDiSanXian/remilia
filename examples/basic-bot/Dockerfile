# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/KomeiDiSanXian/remilia.Version=${VERSION} -X main.commit=${GIT_COMMIT} -X main.date=${BUILD_TIME}" \
    -o /bin/basic-bot ./examples/basic-bot/

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/basic-bot /basic-bot

ENTRYPOINT ["/basic-bot"]

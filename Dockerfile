FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=1 go build -ldflags="\
  -s -w \
  -X github.com/KomeiDiSanXian/remilia.Version=${VERSION} \
  -X main.commit=${GIT_COMMIT} \
  -X main.date=${BUILD_TIME}" \
  -o /build/remilia ./cmd/bot

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S remilia && adduser -S remilia -G remilia

WORKDIR /app
COPY --from=builder /build/remilia .

RUN mkdir -p /app/data/db && chown -R remilia:remilia /app/data

USER remilia

VOLUME ["/app/data"]

EXPOSE 8080
EXPOSE 9001

ENTRYPOINT ["/app/remilia"]
CMD ["run"]

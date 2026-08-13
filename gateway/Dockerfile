# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/owu-proxy ./cmd/owu-proxy

FROM alpine:3.21
RUN addgroup -S owu && adduser -S -G owu owu
COPY --from=build /out/owu-proxy /usr/local/bin/owu-proxy
ENV OWU_PROXY_LISTEN_ADDR=0.0.0.0:3211
USER owu:owu
EXPOSE 3211
ENTRYPOINT ["/usr/local/bin/owu-proxy"]

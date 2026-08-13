# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/permit-gateway ./cmd/gateway

FROM alpine:3.21
RUN addgroup -S permit && adduser -S -G permit permit
COPY --from=build /out/permit-gateway /usr/local/bin/permit-gateway
USER permit:permit
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/permit-gateway"]

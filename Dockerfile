FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY static ./static
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/palctrl .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose tzdata \
    && addgroup -S palctrl \
    && adduser -S -G palctrl -h /app palctrl \
    && mkdir -p /app/backups \
    && chown -R palctrl:palctrl /app

WORKDIR /app
COPY --from=build /out/palctrl /usr/local/bin/palctrl

USER palctrl
ENV PANEL_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/palctrl"]


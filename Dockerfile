FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/network-stats-exporter .

FROM alpine:3.21
RUN apk add --no-cache iputils libcap \
    && setcap cap_net_raw+ep /bin/ping \
    && apk del libcap \
    && addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /data \
    && chown app:app /data
COPY --from=build /out/network-stats-exporter /usr/local/bin/network-stats-exporter
ENV CONFIG_PATH=/data/config.json
EXPOSE 8080
VOLUME ["/data"]
USER app
ENTRYPOINT ["/usr/local/bin/network-stats-exporter"]

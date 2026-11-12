FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/network-stats-exporter .

FROM alpine:3.21
ARG TARGETARCH
RUN apk add --no-cache iputils libcap su-exec \
    && setcap cap_net_raw+ep /bin/ping \
    && apk del libcap \
    && addgroup -S app \
    && adduser -S -G app app \
    && case "$TARGETARCH" in \
        amd64) ookla_arch=x86_64 ;; \
        arm64) ookla_arch=aarch64 ;; \
        *) echo "unsupported Ookla Speedtest architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && wget -qO /tmp/ookla-speedtest.tgz "https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-linux-${ookla_arch}.tgz" \
    && tar -xzf /tmp/ookla-speedtest.tgz -C /usr/local/bin speedtest \
    && chmod 0755 /usr/local/bin/speedtest \
    && rm /tmp/ookla-speedtest.tgz
COPY --from=build /out/network-stats-exporter /usr/local/bin/network-stats-exporter
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint
RUN chmod 0755 /usr/local/bin/docker-entrypoint
ENV CONFIG_PATH=/data/config.json HOME=/data
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
CMD ["/usr/local/bin/network-stats-exporter"]

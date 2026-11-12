# Network Stats Exporter

A web-configurable network exporter that exposes live ICMP ping and optional Ookla or Cloudflare speedtest results for Prometheus to scrape.

## Run locally or on TrueNAS

The Compose stack runs **only the exporter**. It does not install or manage Prometheus or Grafana:

```sh
docker compose up --build -d
```

Open the configuration page at <http://localhost:8080> to set ping targets and probe options.

The Compose file has a persistent named Docker volume, `network-stats-exporter-config`, mounted at `/data`, where the configuration is saved. Restarting or recreating the exporter container keeps the configuration. `docker compose down` also retains it; **do not** use `docker compose down -v` unless you intend to delete the volume and saved configuration. In TrueNAS, keep the app's `/data` volume (or map it to a persistent dataset) when editing/redeploying the app.

Publish or map container port `8080` so both you and Prometheus can reach it. In your existing Prometheus configuration, add a scrape job using an address reachable **from the Prometheus server/container**:

```yaml
scrape_configs:
  - job_name: network-stats-exporter
    scrape_interval: 1s
    static_configs:
      - targets: ["<TRUENAS-IP-OR-HOSTNAME>:8080"]
```

Replace the placeholder with the TrueNAS address and published exporter port, then reload/restart Prometheus as appropriate for your setup. Verify in Prometheus that `up{job="network-stats-exporter"}` is `1`. In Grafana, use your existing Prometheus data source and query:

- `network_ping_success` — `1` means reachable and `0` means unreachable.
- `network_ping_duration_seconds * 1000` — latest successful ping latency in milliseconds.
- `network_ping_jitter_seconds * 1000` — mean absolute change in RTT between consecutive successful packets in the latest check, in milliseconds. It is omitted if fewer than two consecutive packets reply.
- `network_ping_packet_loss_ratio * 100` — packet loss percentage for the latest ping check.
- Every ping metric includes `target` and `type` labels. For example, `avg by (type) (network_ping_duration_seconds)` shows average latest successful latency per target type (in seconds); multiply by `1000` for milliseconds.
- `network_speedtest_download_bits_per_second{provider="ookla|cloudflare"}` and `network_speedtest_upload_bits_per_second{provider="ookla|cloudflare"}` — latest successful throughput for each provider.
- `network_speedtest_latency_seconds` and `network_speedtest_packet_loss_ratio` — latency and packet loss from the latest successful speedtest, also labeled by provider; packet loss is omitted when a provider does not supply it.
- `network_speedtest_success`, `network_speedtest_running`, and `network_speedtest_last_run_timestamp_seconds` — latest status and completion time for each provider.
- `network_speedtest_provider_info{provider="ookla|cloudflare"}` — identifies the two provider series.
- To graph throughput in Mbps, query `network_speedtest_download_bits_per_second{provider="cloudflare"} / 1000000` or use the corresponding Ookla label.

Set the Grafana dashboard refresh to 1 second for near-live updates. Prometheus stores scraped samples as history; the exporter values themselves always represent the latest completed check.

In the configuration page, ping, Ookla, and Cloudflare settings have independent save buttons. Saving one section preserves the others. Each provider can be manually run or independently enabled on a schedule, and both providers can run concurrently. The page shows each provider's next automatic execution separately and identifies whether it comes from an interval or cron schedule. Set an interval (default: 30 minutes, minimum: 5 minutes), or provide a five-field cron expression to override that provider's interval (for example `0 */6 * * *` for every six hours). Cron schedules use the exporter host/container's local timezone. The speedtest timeout is shared by both providers and defaults to 5 minutes (allowed: 30 seconds–30 minutes). A configuration migration carries an existing enabled single-provider schedule into that provider's new settings.

Ookla's CLI controls its own transfer workload and does not expose the Cloudflare-style concurrent-request count or request size. Cloudflare lets you configure concurrent requests per direction (default: 4, allowed: 1–8) and each request's size (default: 5 MB, allowed: 1–50 MB). Each run makes that many concurrent downloads and uploads; the approximate maximum transfer is `2 × concurrent requests × request size` MB (default 40 MB, maximum 800 MB). Five small latency requests also run first. Throughput is estimated from total transferred bytes and elapsed time. Requests expose the exporter host's public IP to Cloudflare and are subject to its privacy policy. Packet loss is not measured by this Cloudflare integration; Ookla may report it. Speedtests do not pause ping checks and can consume substantial bandwidth. All settings are persisted in the Docker volume.

At startup the container prepares the configuration directory for its `app` user, then drops root privileges before running the exporter. If saving or resetting fails, the page displays the storage error. On TrueNAS, check that the mounted volume is read/write and that security settings do not force the container to run as a non-root user before this ownership initialization.

The web page and configuration API have no authentication. Keep the exporter on a trusted network and do not expose port 8080 directly to the public internet.

## Build and publish

`.github/workflows/build-publish.yml` runs `go test ./...` and `go vet ./...` for pull requests and pushes to `main`. A push or merge to `main` publishes multi-platform (`linux/amd64` and `linux/arm64`) images to `ghcr.io/untalturner/network-stats-exporter` with `latest` and commit-SHA tags, only after both checks pass. No personal access token is required; the workflow uses `GITHUB_TOKEN`.

The first GHCR publication may create a private package. In GitHub, open the repository's **Packages** settings and change the package visibility to **Public** to allow unauthenticated pulls.

To run the published image, replace `build: .` and `image: network-stats-exporter:local` in `compose.yaml` with:

```yaml
image: ghcr.io/untalturner/network-stats-exporter:latest
```

The image includes its own ping utility and Ookla Speedtest CLI and supports Linux amd64 and arm64 hosts. The web UI requires acknowledgement of Ookla's EULA, Terms of Use, and Privacy Policy before an Ookla test can run, or acknowledgment of the Cloudflare privacy notice before a Cloudflare test can run. For local builds outside Docker, install the official Ookla Speedtest CLI from <https://www.speedtest.net/apps/cli> and make sure `speedtest` is in `PATH`. Ookla's CLI is restricted to personal, non-commercial use. Cloudflare's test uses public edge endpoints and requires outbound HTTPS access.

## Test and build without Docker

Go 1.22 or newer:

```sh
go test ./...
go vet ./...
go build .
```

Run the binary with `CONFIG_PATH=./data/config.json` to persist settings locally (`CONFIG_PATH` defaults to `/data/config.json` in the container). The host must have a `ping` command available.

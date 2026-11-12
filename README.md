# Network Stats Exporter

A web-configurable ICMP ping exporter that exposes the latest latency, reachability, and packet-loss results for Prometheus to scrape.

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
- `network_ping_packet_loss_ratio * 100` — packet loss percentage for the latest ping check.

Set the Grafana dashboard refresh to 1 second for near-live updates. Prometheus stores scraped samples as history; the exporter values themselves always represent the latest completed check.

The configuration page accepts one IP address or hostname per line. **Schedule** is the interval in seconds between rounds (default: 1 second); **delay** is the pause between targets (default: 0); **timeout** is the reply wait per packet (default: 1000ms); **packets per ping check** is the number of ICMP echo requests per target (default: 5, allowed: 1–20). Five packets give loss readings in 20% increments. Settings are persisted in the Docker volume.

The web page and configuration API have no authentication. Keep the exporter on a trusted network and do not expose port 8080 directly to the public internet.

## Build and publish

`.github/workflows/build-publish.yml` runs `go test ./...` and `go vet ./...` for pull requests and pushes to `main`. A push or merge to `main` publishes multi-platform (`linux/amd64` and `linux/arm64`) images to `ghcr.io/untalturner/network-stats-exporter` with `latest` and commit-SHA tags, only after both checks pass. No personal access token is required; the workflow uses `GITHUB_TOKEN`.

The first GHCR publication may create a private package. In GitHub, open the repository's **Packages** settings and change the package visibility to **Public** to allow unauthenticated pulls.

To run the published image, replace `build: .` and `image: network-stats-exporter:local` in `compose.yaml` with:

```yaml
image: ghcr.io/untalturner/network-stats-exporter:latest
```

The image includes its own ping utility and supports Linux amd64 and arm64 hosts.

## Test and build without Docker

Go 1.22 or newer:

```sh
go test ./...
go vet ./...
go build .
```

Run the binary with `CONFIG_PATH=./data/config.json` to persist settings locally (`CONFIG_PATH` defaults to `/data/config.json` in the container). The host must have a `ping` command available.

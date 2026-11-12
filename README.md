# Network Stats Exporter

A small web-configurable ICMP ping exporter. It probes configured IP addresses or hostnames, serves the latest results as Prometheus metrics, and persists settings in a Docker volume.

## Run locally

Requirements: Docker Desktop (or Docker Engine) and Docker Compose.

```sh
docker compose up --build
```

- Configure ping targets at <http://localhost:8080>.
- Prometheus is available at <http://localhost:9090>.
- Grafana is available at <http://localhost:3000> (default login: `admin` / `admin`; Grafana asks you to change the password).

The page accepts one target per line. **Schedule** is the interval in seconds between rounds (default: 1 second); **delay** is the pause in milliseconds between targets within a round (default: 0); **timeout** is the maximum reply wait in milliseconds per packet (default: 1000); **packets per ping check** is the number of ICMP echo requests sent to each target per round (default: 5, allowed: 1–20). Five packets provide 20% loss increments for each check. On Linux and macOS they are sent 200ms apart. The first round starts as soon as the exporter starts. Settings are saved to the `exporter-data` volume and survive container recreation; existing saved configs receive the default packet count. A changed schedule takes effect after the current round. Probes run serially, so many slow or unreachable targets can make a round take longer than its schedule.

In Grafana, add a Prometheus data source with URL `http://prometheus:9090` if it is not already configured, then use Explore to query:

- `network_ping_success` — `1` means reachable and `0` means unreachable.
- `network_ping_duration_seconds` — most recent successful ping's round-trip latency in seconds; multiply by 1000 for milliseconds.
- `network_ping_packet_loss_ratio` — packet loss from the most recent ping check only, from `0` (no loss) to `1` (all packets lost). The ping summary's sent and received packet counts are used; a failed ping with no reply reports `1`.

The exporter itself exposes these three live values at <http://localhost:8080/metrics>. Prometheus scrapes it every second and the web page refreshes its latest-check table every second. In Grafana, set dashboard refresh to **1s** and use these queries:

- Latency in milliseconds: `network_ping_duration_seconds * 1000`
- Latest ping status: `network_ping_success`
- Packet loss percentage for the latest ping: `network_ping_packet_loss_ratio * 100`

Each new ping replaces the previous values; none of these metrics are cumulative. Prometheus stores each scrape as a time series so Grafana can display their history. Updates happen on the ping and scrape intervals, so they are near-live rather than instantaneous. Packet counts depend on the host's `ping` command summary format.

The web page and configuration API have no authentication. Keep the ports on a trusted network; do not expose them directly to the public internet. You can change the published ports in `compose.yaml`.

To run only the exporter, use `docker compose up --build network-stats-exporter`. To remove the containers while retaining settings and metric history, use `docker compose down`; `docker compose down -v` also deletes persisted data.

## Build and publish

`.github/workflows/build-publish.yml` runs `go test ./...` and `go vet ./...` for pull requests and pushes to `main`. A push or merge to `main` publishes multi-platform (`linux/amd64` and `linux/arm64`) images to `ghcr.io/untalturner/network-stats-exporter` with `latest` and commit-SHA tags, only after both checks pass. No personal access token is required; the workflow uses `GITHUB_TOKEN`.

The first GHCR publication may create a private package. In GitHub, open the repository's **Packages** settings and change the package visibility to **Public** to allow unauthenticated pulls.

After publication, replace `build: .` in the exporter service in `compose.yaml` with:

```yaml
image: ghcr.io/untalturner/network-stats-exporter:latest
```

and remove the `build: .` line. The image is Linux-based and built for amd64 and arm64 hosts. It includes the ping utility and grants that utility only the raw-socket capability needed for ICMP; no elevated container privilege is required.

## Test and build without Docker

Go 1.22 or newer:

```sh
go test ./...
go vet ./...
go build .
```

Run the binary with `CONFIG_PATH=./data/config.json` to persist settings locally (`CONFIG_PATH` defaults to `/data/config.json` for the container), then open <http://localhost:8080>. The host must have a `ping` command available. The Docker image includes its own ping utility.

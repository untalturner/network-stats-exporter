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

The page accepts one target per line. **Schedule** is the interval in seconds between rounds; **delay** is the pause in milliseconds between targets within a round; **timeout** is the maximum time in milliseconds for each ping. The first round starts as soon as the exporter starts. Settings are saved to the `exporter-data` volume and survive container recreation. A changed schedule takes effect after the current round.

In Grafana, add a Prometheus data source with URL `http://prometheus:9090` if it is not already configured, then use Explore to query:

- `network_ping_success` — `1` means reachable and `0` means unreachable.
- `network_ping_duration_seconds` — most recent ping duration in seconds.
- `network_ping_checks_total` — check counts grouped by `target` and `result`.

The exporter itself exposes these at <http://localhost:8080/metrics>. Prometheus scrapes it every 15 seconds. The web page refreshes its latest-check table every 5 seconds.

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

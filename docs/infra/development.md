# Local development with hot reload

Run the control plane in Docker with the repository bind-mounted and Air
watching it:

```sh
make dev
```

Open <http://127.0.0.1:8080>. On first run, complete the normal administrator
setup. The `nagipath-dev-data` Docker volume keeps that SQLite database and its
Master Key across reloads and `make dev-down`; remove the volume only when a
fresh local installation is wanted.

Air polls every 500 ms because Docker Desktop bind mounts do not reliably emit
filesystem events. It rebuilds for Go and migration changes. The development
image includes the pinned Air CLI. UI changes under `ui/src` are handled
separately by Vite's own dev server (`make lab`'s `ui` service, or
`make ui-dev` standalone) — Air only rebuilds and restarts the Go binary.

Useful commands:

```sh
make dev-logs  # follow the reloader and application logs
make dev-down  # stop the development container; preserve data
docker compose -f compose.dev.yml down -v  # discard local dev data and caches
```

`make lab` uses the same Air-based reload setup for its `nagipath` service and
also starts the managed-host fixtures. Use it when working against the fixture
fleet; use `make dev` for a lighter standalone control-plane session.

If port 8080 is in use (for example, by the lab), choose another host port:

```sh
NAGIPATH_DEV_PORT=18080 docker compose -f compose.dev.yml up --build
```

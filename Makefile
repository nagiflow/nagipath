.PHONY: build test proto ui ui-dev dev dev-logs dev-down lab lab-collect lab-logs lab-ui-logs lab-down clean

build:
	CGO_ENABLED=0 go build -ldflags "-X main.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o nagipath ./cmd/nagipath

test:
	go vet ./...
	go test ./...

# -------------------------------------------------------------------- proto
#
# internal/api/pb and ui/src/api/pb are generated from proto/*.proto and
# COMMITTED (ADR-0018): go build and bun run build need no toolchain beyond
# Go and bun even without buf installed. Run `make proto` after touching a
# .proto file. Requires buf, protoc-gen-go, protoc-gen-go-grpc and
# protoc-gen-grpc-gateway on PATH (buf parses .proto itself — no separate
# protoc binary needed) and protoc-gen-es installed under ui/node_modules
# (bun install, in ui/, pulls it in).
#
# proto/google/api/ (vendored google.api.http annotation, ADR-0018) gets
# generated output too, on both sides: the Go side never imports it (grpc-
# gateway's generated code imports the real google.golang.org/genproto
# package instead — see http.proto's go_package option), but Protobuf-ES
# does resolve extension descriptors into TS imports, so the TS side needs
# its google/api/annotations_pb.ts to actually exist.

proto:
	buf generate proto

# ---------------------------------------------------------------------- ui
#
# internal/web/ui/dist is the built React SPA, generated from ui/ and
# COMMITTED — the same "go build needs no toolchain beyond Go" property css
# established (ADR-0016), extended to the whole UI (ADR-0017). Run `make ui`
# after any change under ui/src.

ui:
	cd ui && bun install --frozen-lockfile && bun run build

ui-dev:
	cd ui && bun run dev

# Development server: source is bind-mounted into Docker and Air rebuilds it on
# changes. This has its own data volume; it never touches the test-lab state.
dev:
	docker compose -f compose.dev.yml up --build

dev-logs:
	docker compose -f compose.dev.yml logs -f nagipath

dev-down:
	docker compose -f compose.dev.yml down

# The lab needs no state on the host: nagipath runs inside it, on the same network
# as the nodes, because the 10.90.4.0/24 bridge is not routable from macOS.
lab:
	./testlab/gen.sh
	docker compose -f testlab/docker-compose.yml up -d --build
	@echo
	@echo "  http://127.0.0.1:5173  —  admin / nagipath-lab-admin  (frontend hot reload — use this one)"
	@echo "  http://127.0.0.1:8080  —  same login, no frontend hot reload"
	@echo "  Go, template and CSS changes hot-reload through Air either way;"
	@echo "  ui/src changes hot-reload through Vite only on :5173."
	@echo
	@echo "  next: make lab-collect, approve the four host keys at /nodes,"
	@echo "        then make lab-collect again."

lab-collect:
	docker compose -f testlab/docker-compose.yml exec nagipath nagipath collect all

lab-logs:
	docker compose -f testlab/docker-compose.yml logs -f nagipath

lab-ui-logs:
	docker compose -f testlab/docker-compose.yml logs -f ui

lab-down:
	docker compose -f testlab/docker-compose.yml down

clean:
	rm -f nagipath

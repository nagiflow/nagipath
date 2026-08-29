.PHONY: build test css css-watch ui ui-dev dev dev-logs dev-down lab lab-collect lab-logs lab-down clean

build:
	CGO_ENABLED=0 go build -ldflags "-X main.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o nagipath ./cmd/nagipath

test:
	go vet ./...
	go test ./...

# --------------------------------------------------------------------- css
#
# static/app.css is generated from static/app.src.css and COMMITTED, so a plain
# `go build` (or `go install`, or a clone with no network) still produces a
# working binary. Only editing the theme or the templates needs the CLI, which
# is a standalone binary — no node, no node_modules, no package.json.
# Run `make css` after touching app.src.css or any class name in a template.

TAILWIND_VERSION := v4.3.3
# The development container supplies this as /usr/local/bin/tailwindcss; local
# builds keep the pinned repository tool path and its download rule below.
TAILWIND ?= tools/tailwindcss
CSS_SRC := internal/web/static/app.src.css
CSS_OUT := internal/web/static/app.css

css: $(TAILWIND)
	$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --minify

css-watch: $(TAILWIND)
	$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --watch

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

# uname decides the asset; the CLI is gitignored, one download per checkout.
$(TAILWIND):
	@mkdir -p tools
	@case "$$(uname -s)-$$(uname -m)" in \
		Darwin-arm64)  a=macos-arm64 ;; \
		Darwin-x86_64) a=macos-x64 ;; \
		Linux-aarch64) a=linux-arm64 ;; \
		Linux-x86_64)  a=linux-x64 ;; \
		*) echo "no tailwindcss build for $$(uname -sm); see $(CSS_OUT) — it is committed, you may not need one" >&2; exit 1 ;; \
	esac; \
	echo "fetching tailwindcss $(TAILWIND_VERSION) ($$a)"; \
	curl -fsSL -o $(TAILWIND) \
		"https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$$a"
	@chmod +x $(TAILWIND)

# The lab needs no state on the host: nagipath runs inside it, on the same network
# as the nodes, because the 10.90.4.0/24 bridge is not routable from macOS.
lab:
	./testlab/gen.sh
	docker compose -f testlab/docker-compose.yml up -d --build
	@echo
	@echo "  http://127.0.0.1:8080  —  admin / nagipath-lab-admin"
	@echo "  source changes hot-reload through Air"
	@echo
	@echo "  next: make lab-collect, approve the four host keys at /nodes,"
	@echo "        then make lab-collect again."

lab-collect:
	docker compose -f testlab/docker-compose.yml exec nagipath nagipath collect all

lab-logs:
	docker compose -f testlab/docker-compose.yml logs -f nagipath

lab-down:
	docker compose -f testlab/docker-compose.yml down

clean:
	rm -f nagipath

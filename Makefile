.PHONY: build test lab lab-collect lab-logs lab-down clean

build:
	CGO_ENABLED=0 go build -ldflags "-X main.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o nagipath ./cmd/nagipath

test:
	go vet ./...
	go test ./...

# The lab needs no state on the host: nagipath runs inside it, on the same network
# as the nodes, because the 10.90.4.0/24 bridge is not routable from macOS.
lab:
	./testlab/gen.sh
	docker compose -f testlab/docker-compose.yml up -d --build
	@echo
	@echo "  http://127.0.0.1:8080  —  admin / nagipath-lab-admin"
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

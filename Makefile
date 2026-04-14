MODULE  := github.com/Andrevops/claude-stats
BINARY  := claude-stats
CMD     := ./cmd/claude-stats
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -s -w"
IMAGE   := claude-stats

# ── Local build (requires Go) ─────────────────────────────────────────────────

## build: compile binary to ./bin/  (requires Go locally)
.PHONY: build
build:
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(BINARY) $(CMD)
	@echo "built: bin/$(BINARY)  ($(VERSION))"

## install: build + install to first ~/… directory on PATH
.PHONY: install
install: build
	@INSTALL_DIR="$$HOME/.local/bin"; \
	mkdir -p "$$INSTALL_DIR"; \
	cp bin/$(BINARY) "$$INSTALL_DIR/$(BINARY)"; \
	chmod +x "$$INSTALL_DIR/$(BINARY)"; \
	echo "installed: $$INSTALL_DIR/$(BINARY)"

## update: git pull + reinstall
.PHONY: update
update:
	git pull origin main
	$(MAKE) install

# ── Docker build (no local Go required) ───────────────────────────────────────

## docker-build: build final runtime Docker image
.PHONY: docker-build
docker-build:
	docker build --build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .
	@echo "image: $(IMAGE):$(VERSION)"

## docker-install: extract binary from Docker image → install without local Go
.PHONY: docker-install
docker-install:
	@mkdir -p bin
	docker build --target builder --build-arg VERSION=$(VERSION) \
		-t $(IMAGE)-builder:$(VERSION) .
	docker create --name cs-extract $(IMAGE)-builder:$(VERSION)
	docker cp cs-extract:/$(BINARY) bin/$(BINARY)
	docker rm cs-extract
	@INSTALL_DIR="$$HOME/.local/bin"; \
	mkdir -p "$$INSTALL_DIR"; \
	cp bin/$(BINARY) "$$INSTALL_DIR/$(BINARY)"; \
	chmod +x "$$INSTALL_DIR/$(BINARY)"; \
	echo "installed: $$INSTALL_DIR/$(BINARY)"

## docker-run: run interactively with ~/.claude mounted read-only
.PHONY: docker-run
docker-run: docker-build
	docker run --rm -it \
		-v "$(HOME)/.claude:/root/.claude:ro" \
		$(IMAGE):latest

## docker-run-cmd: run a single command  (e.g. make docker-run-cmd CMD="tokens --week")
.PHONY: docker-run-cmd
docker-run-cmd: docker-build
	docker run --rm \
		-v "$(HOME)/.claude:/root/.claude:ro" \
		$(IMAGE):latest $(CMD)

# ── Release ───────────────────────────────────────────────────────────────────

## release: auto-detect bump from conventional commits, tag, and push
.PHONY: release
release:
	@./scripts/release.sh
	@git push --follow-tags

## release-patch: force a patch version bump, tag, and push
.PHONY: release-patch
release-patch:
	@./scripts/release.sh patch
	@git push --follow-tags

## release-minor: force a minor version bump, tag, and push
.PHONY: release-minor
release-minor:
	@./scripts/release.sh minor
	@git push --follow-tags

## release-major: force a major version bump, tag, and push
.PHONY: release-major
release-major:
	@./scripts/release.sh major
	@git push --follow-tags

## release-build: cross-compile all platforms to ./dist/
.PHONY: release-build
release-build:
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64   $(CMD)
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-arm64   $(CMD)
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64  $(CMD)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe $(CMD)
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-windows-arm64.exe $(CMD)
	chmod +x dist/$(BINARY)-linux-* dist/$(BINARY)-darwin-*
	cd dist && sha256sum * > checksums.txt
	@echo "binaries in dist/"; ls -lh dist/

## release-docker: cross-compile all platforms via Docker (no local Go)
.PHONY: release-docker
release-docker:
	@mkdir -p dist
	MSYS_NO_PATHCONV=1 docker run --rm \
		-v "$(CURDIR):/src" -w /src \
		-e CGO_ENABLED=0 \
		golang:1.21-alpine \
		sh -c ' \
			LDFLAGS="-X main.version=$(VERSION) -s -w"; \
			GOOS=linux   GOARCH=amd64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-linux-amd64   $(CMD) && \
			GOOS=linux   GOARCH=arm64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-linux-arm64   $(CMD) && \
			GOOS=darwin  GOARCH=amd64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-darwin-amd64  $(CMD) && \
			GOOS=darwin  GOARCH=arm64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-darwin-arm64  $(CMD) && \
			GOOS=windows GOARCH=amd64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-windows-amd64.exe $(CMD) && \
			GOOS=windows GOARCH=arm64 go build -ldflags "$$LDFLAGS" -o dist/$(BINARY)-windows-arm64.exe $(CMD) \
		'
	chmod +x dist/$(BINARY)-linux-* dist/$(BINARY)-darwin-*
	cd dist && sha256sum * > checksums.txt
	@echo "binaries in dist/"; ls -lh dist/

## tag: create and push a version tag manually  (e.g. make tag VERSION=v0.2.0)
.PHONY: tag
tag:
	@if [ -z "$(VERSION)" ] || [ "$(VERSION)" = "dev" ]; then \
		echo "Usage: make tag VERSION=v0.2.0  (prefer: make release)"; exit 1; fi
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "tagged $(VERSION) — GitHub Actions will build and publish the release"

# ── Dev ───────────────────────────────────────────────────────────────────────

## run: build and run  (e.g. make run ARGS="tokens --week")
.PHONY: run
run: build
	./bin/$(BINARY) $(ARGS)

## test: run all tests
.PHONY: test
test:
	go test ./...

## lint: run go vet
.PHONY: lint
lint:
	go vet ./...

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf bin/ dist/

## version: print current version
.PHONY: version
version:
	@echo $(VERSION)

## help: show this help
.PHONY: help
help:
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  /'

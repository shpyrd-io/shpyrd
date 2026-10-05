VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CLUSTER      ?= shpyrd
SERVER_IMAGE ?= shpyrd-server:dev
LDFLAGS      := -X github.com/shpyrd-io/shpyrd/pkg/version.Version=$(VERSION)

.PHONY: all build cli server ui pages emails website website-dev image dev-image release-binaries generate \
        test vet lint clean dev-cluster dev-load dev-deploy dev-destroy \
        dev-pause dev-resume installclint commitlint

all: build

## Build both binaries into bin/
build: cli server

## Build both CLI binaries: shpyrd (developers) and shpyrd-ctl (operators).
## They ship together since v0.8.0 (RFC-0052).
cli:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/shpyrd ./cmd/shpyrd
	go build -ldflags "$(LDFLAGS)" -o bin/shpyrd-ctl ./cmd/shpyrd-ctl

server:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/shpyrd-server ./cmd/shpyrd-server

## Build the applications (apps/console, apps/workspace) into pkg/ui/dist,
## a folder each, embedded by `make server`
ui:
	npm ci --no-audit --no-fund
	npm --prefix apps/console run build
	npm --prefix apps/workspace run build
	rm -rf pkg/ui/dist/console pkg/ui/dist/workspace
	cp -R apps/console/out pkg/ui/dist/console
	cp -R apps/workspace/out pkg/ui/dist/workspace

## Draw the pages the server serves by itself (pkg/pages/html) from the
## design library: design/pages/README.md
pages:
	npm install --no-audit --no-fund && npm --prefix design/pages run build

## Draw the emails the platform sends (pkg/emails/html) from their
## designs: design/emails/README.md
emails:
	npm install --no-audit --no-fund && npm --prefix design/emails run build

## Build the website (shpyrd.io); deployed from apps/website/ on Vercel
website:
	npm ci --no-audit --no-fund && npm --prefix apps/website run build

## Serve the website locally on http://localhost:4324
website-dev:
	npm install && npm --prefix apps/website run dev

## Build the server container image (full multi-stage build)
image:
	docker build --build-arg VERSION=$(VERSION) -t $(SERVER_IMAGE) .

## Fast development image: compile the server and the PostgreSQL gateway on
## the host (the applications embedded from pkg/ui/dist), then package them
## with Dockerfile.dev, as the release does. Run `make ui` first when an
## application changed.
GOARCH_HOST := $(shell go env GOARCH)
dev-image:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH_HOST) go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/shpyrd-server-linux-$(GOARCH_HOST) ./cmd/shpyrd-server
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH_HOST) go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/pg-gateway-linux-$(GOARCH_HOST) ./cmd/pg-gateway
	docker build -f Dockerfile.dev --build-arg TARGETARCH=$(GOARCH_HOST) -t $(SERVER_IMAGE) .

## The release image's binaries: the server and the PostgreSQL gateway for
## both architectures (the applications embedded from pkg/ui/dist), for
## Dockerfile.dev. CI builds them on every change, so a release finds
## them compiled.
release-binaries:
	mkdir -p bin
	for arch in amd64 arm64; do \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/shpyrd-server-linux-$$arch ./cmd/shpyrd-server && \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o bin/pg-gateway-linux-$$arch ./cmd/pg-gateway || exit 1; \
	done

## Regenerate deepcopy code and the App CRD from api/
generate:
	go tool controller-gen object paths=./api/...
	go tool controller-gen crd paths=./api/... output:crd:dir=./deploy/components/shpyrd/base/crds

## The npm workspaces with checks of their own: the library and the
## applications. `npm ci` at the root (which `make ui` does) installs them.
WORKSPACES := --workspace design/ui --workspace apps/shared --workspace apps/console --workspace apps/workspace

## Go tests and the workspaces' tests
test:
	go test ./...
	npm run test $(WORKSPACES)

vet:
	go vet ./...

lint: vet
	npm run lint $(WORKSPACES)
	npm run typecheck $(WORKSPACES)

clean:
	rm -rf bin pkg/ui/dist/console pkg/ui/dist/workspace apps/console/out apps/workspace/out

## Local development loop -----------------------------------------------------

## Create the kind cluster and install everything except the shpyrd server
dev-cluster: cli
	./bin/shpyrd cluster create --name $(CLUSTER) --skip shpyrd

## Build the server image and load it into every node of the kind cluster.
## This is what `kind load docker-image` does (import into containerd's k8s.io
## namespace with the node's snapshotter), without needing a kind binary on
## PATH: the cluster is created by the kind library in go.mod, and a kind CLI
## older than that library cannot read the node's containerd config.
dev-load: dev-image
	@set -e; \
	nodes=$$(docker ps -q --filter "label=io.x-k8s.kind.cluster=$(CLUSTER)"); \
	if [ -z "$$nodes" ]; then \
		echo "no kind cluster named $(CLUSTER); run 'make dev-cluster'" >&2; exit 1; \
	fi; \
	archive=$$(mktemp); \
	trap 'rm -f "$$archive"' EXIT; \
	docker save -o "$$archive" $(SERVER_IMAGE); \
	for node in $$nodes; do \
		name=$$(docker inspect --format '{{.Name}}' $$node | cut -c2-); \
		snapshotter=$$(docker exec $$node containerd config dump 2>/dev/null \
			| sed -n "s/^[[:space:]]*snapshotter = '\(..*\)'/\1/p" | head -1); \
		echo "Loading $(SERVER_IMAGE) into $$name ($${snapshotter:-overlayfs})..."; \
		docker exec -i $$node ctr --namespace=k8s.io images import \
			--all-platforms --digests --snapshotter="$${snapshotter:-overlayfs}" - \
			< "$$archive" >/dev/null; \
	done

## Load the image and (re)apply the shpyrd component
dev-deploy: cli dev-load
	./bin/shpyrd cluster init --context kind-$(CLUSTER) --only shpyrd --set SHPYRD_SERVER_IMAGE=$(SERVER_IMAGE)
	kubectl --context kind-$(CLUSTER) -n shpyrd-system rollout restart deployment/shpyrd-server

dev-destroy: cli
	./bin/shpyrd cluster destroy --name $(CLUSTER) --yes

## Switch the local cluster off and on without losing it: the kind nodes
## are containers, stopped and started in place (state stays on their
## disks; the platform is back within a minute of `dev-resume`).
dev-pause:
	@nodes=$$(docker ps -q --filter "label=io.x-k8s.kind.cluster=$(CLUSTER)"); \
	if [ -z "$$nodes" ]; then echo "no running kind cluster named $(CLUSTER)" >&2; exit 1; fi; \
	docker stop $$nodes >/dev/null && echo "Cluster $(CLUSTER) paused (make dev-resume CLUSTER=$(CLUSTER) brings it back)."

dev-resume:
	@nodes=$$(docker ps -aq --filter "label=io.x-k8s.kind.cluster=$(CLUSTER)"); \
	if [ -z "$$nodes" ]; then echo "no kind cluster named $(CLUSTER); run 'shpyrd-ctl cluster create --name $(CLUSTER)'" >&2; exit 1; fi; \
	docker start $$nodes >/dev/null && echo "Cluster $(CLUSTER) resuming; 'shpyrd-ctl cluster status --context kind-$(CLUSTER)' shows when it is ready."

## Tooling ---------------------------------------------------------------------

installclint:
	npm install -g @commitlint/cli @commitlint/config-conventional

commitlint:
	commitlint --from=HEAD~1

## Alias kept for scripts that called it; `make cli` builds both now.
cli-all: cli

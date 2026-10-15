VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/DeLFuS77/completist/internal/server.Version=$(VERSION)

.PHONY: all ui build build-windows build-mac test docker dev clean

all: ui build

# the interface, embedded by the Go build: run it first
ui:
	cd ui && (test -d node_modules || (test -f package-lock.json && npm ci) || npm install) && npm run build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o build/completist ./cmd/completist

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o build/completist.exe ./cmd/completist

build-mac:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o build/completist-mac ./cmd/completist

test:
	go vet ./...
	go test ./...

docker:
	docker build --build-arg VERSION=$(VERSION) -t completist:$(VERSION) .

# the interface rebuilds on every change and the app serves it from its folder
dev:
	cd ui && npm run watch & COMPLETIST_UI_DIR=internal/webui/dist go run ./cmd/completist -data data

clean:
	rm -rf build
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete

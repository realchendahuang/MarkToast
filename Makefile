BINARY  := marktoast
VERSION ?= 0.0.1-dev
LDFLAGS := -s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=$(VERSION)

.PHONY: all build desktop run run-desktop test fmt vet tidy check serve install clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/marktoast

# Wails v3 desktop shell (macOS first; needs CGO). The output must not be
# "MarkToast": on case-insensitive APFS that collides with bin/marktoast.
desktop:
	go build -ldflags "$(LDFLAGS)" -o bin/marktoast-desktop ./cmd/marktoast-desktop

run-desktop: desktop
	./bin/marktoast-desktop

run:
	go run ./cmd/marktoast $(ARGS)

# Install the CLI and the native macOS bundle.
install: build desktop
	./scripts/install.sh

# Local hub + web UI preview (frontend/ served by the hub).
serve:
	go run ./cmd/marktoast serve

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test

tidy:
	go mod tidy

clean:
	rm -rf bin dist

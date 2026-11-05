BINARY         := marktoast
DESKTOP_BINARY := marktoast-desktop
VERSION        ?= 0.0.1-dev
LDFLAGS        := -s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=$(VERSION)
UNAME_S        := $(shell uname -s)

# Fail fast before verify/build when release is invoked without an explicit
# version. `origin` must not be "file" (the Makefile default is not explicit).
ifneq ($(filter release,$(MAKECMDGOALS)),)
ifeq ($(origin VERSION),file)
$(error release requires an explicit VERSION, e.g. make release VERSION=0.1.0)
endif
ifeq ($(strip $(VERSION)),)
$(error release requires a non-empty VERSION, e.g. make release VERSION=0.1.0)
endif
endif

# Wails needs CGO on every platform. The minimum macOS deployment target is
# passed only when building on Darwin; do not leak Apple CGO flags elsewhere.
ifeq ($(UNAME_S),Darwin)
DESKTOP_CGO_ENV := CGO_ENABLED=1 CGO_CFLAGS="-mmacosx-version-min=12.0" CGO_LDFLAGS="-mmacosx-version-min=12.0" MACOSX_DEPLOYMENT_TARGET=12.0
else
DESKTOP_CGO_ENV := CGO_ENABLED=1
endif

.PHONY: all build desktop run run-desktop serve test test-race fmt fmt-check vet tidy tidy-check node-check verify check install release clean

all: build

build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/marktoast

# Wails v3 desktop shell (macOS first; needs CGO). The output must not be
# "MarkToast": on case-insensitive APFS that collides with bin/marktoast.
desktop:
	@mkdir -p bin
	$(DESKTOP_CGO_ENV) go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(DESKTOP_BINARY) ./cmd/marktoast-desktop

run-desktop: desktop
	./bin/$(DESKTOP_BINARY)

run:
	go run ./cmd/marktoast $(ARGS)

# Build and install the CLI and native macOS app bundle. The script owns both
# builds so `make install` and a standalone `scripts/install.sh` behave alike.
install:
	VERSION="$(VERSION)" ./scripts/install.sh

# The release script requires an explicit Apple-numeric VERSION such as 0.1.0.
release: verify
	@if [ "$(origin VERSION)" = "file" ] || [ -z "$(strip $(VERSION))" ]; then \
	echo "error: release requires an explicit VERSION, e.g. make release VERSION=0.1.0" >&2; \
	exit 2; \
	fi
	./scripts/release.sh "$(VERSION)"

# Local hub + web UI preview.
serve:
	go run ./cmd/marktoast serve

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w .

# Non-mutating formatting check: report files that need gofmt, never rewrite.
fmt-check:
	@files="$$(gofmt -l .)" || { echo "error: gofmt failed" >&2; exit 1; }; \
	if [ -n "$$files" ]; then \
	echo "error: gofmt is required for:" >&2; \
	echo "$$files" >&2; \
	exit 1; \
	fi

vet:
	go vet ./...

# go mod tidy -diff exits non-zero when go.mod/go.sum need changes, without
# writing them. This keeps `make check` and CI source trees pristine.
tidy-check:
	go mod tidy -diff

tidy:
	go mod tidy

# The frontend is static HTML/CSS/JS; no bundler or frontend dependency tree is
# required. Node is used only for syntax checking in verification/CI.
node-check:
	node --check frontend/app.js
	node --check frontend/i18n.js
	node --check frontend/theme-init.js

verify: fmt-check tidy-check vet test-race node-check build

check: verify

clean:
	rm -rf bin dist

BINARY  := marktoast
VERSION ?= 0.0.1-dev
LDFLAGS := -s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=$(VERSION)

.PHONY: all build run test fmt vet tidy check serve clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/marktoast

run:
	go run ./cmd/marktoast $(ARGS)

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

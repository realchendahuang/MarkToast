BINARY  := marktoast
VERSION ?= 0.0.1-dev
LDFLAGS := -s -w -X github.com/realchendahuang/MarkToast/internal/version.Version=$(VERSION)

.PHONY: all build run test fmt vet tidy clean

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

run:
	go run . $(ARGS)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin dist

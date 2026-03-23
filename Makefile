VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -ldflags "-X github.com/ghostwright/specter/pkg/version.Version=$(VERSION) \
                      -X github.com/ghostwright/specter/pkg/version.Commit=$(COMMIT) \
                      -X github.com/ghostwright/specter/pkg/version.Date=$(DATE)"

.PHONY: build install clean test lint ci run

build:
	go build $(LDFLAGS) -o bin/specter ./cmd/specter

install:
	go install $(LDFLAGS) ./cmd/specter

clean:
	rm -rf bin/ dist/

test:
	go test ./...

lint:
	go vet ./...

ci: lint build

run:
	go run $(LDFLAGS) ./cmd/specter $(ARGS)

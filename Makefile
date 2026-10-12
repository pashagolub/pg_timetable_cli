## pgtt project Makefile
##
## Targets:
##   build     build pgtt management CLI binary
##   release   build with version ldflags (uses git tags)
##   test      run all tests (integration tests need Docker)
##   lint      run golangci-lint
##   clean     remove built binary

GO      := go
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell git log -1 --format=%cI 2>/dev/null || echo "unknown")

PGTT_PKG     := github.com/cybertec-postgresql/pg_timetable_cli/cmd
PGTT_LDFLAGS := -X $(PGTT_PKG).version=$(VERSION) -X $(PGTT_PKG).commit=$(COMMIT) -X $(PGTT_PKG).date=$(DATE)

.PHONY: build release test lint clean

build:
	$(GO) build -o pgtt .

release:
	$(GO) build -buildvcs=false -ldflags "$(PGTT_LDFLAGS)" -o pgtt .
	@echo "Built pgtt $(VERSION) ($(COMMIT))"

test:
	$(GO) test -failfast -p 1 -timeout=900s ./...

lint:
	golangci-lint run ./...

clean:
	rm -f pgtt

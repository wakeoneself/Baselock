VERSION ?= 0.1.0
LDFLAGS := -s -w -X github.com/wakeoneself/Baselock/internal/cli.Version=$(VERSION)

.PHONY: build test install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/sec ./cmd/sec

test:
	go test ./...

install: build
	install -m 0755 bin/sec /usr/local/bin/sec

clean:
	rm -rf bin

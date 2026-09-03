PREFIX ?= $(HOME)/.local
LDFLAGS := -s -w

.PHONY: build install test clean

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o tuichk .

install:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(PREFIX)/bin/tuichk .

test:
	go test ./...

clean:
	rm -f tuichk

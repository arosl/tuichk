PREFIX ?= $(HOME)/.local
LDFLAGS := -s -w

.PHONY: build install test clean

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o tuicheck .

install:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(PREFIX)/bin/tuicheck .

test:
	go test ./...

clean:
	rm -f tuicheck

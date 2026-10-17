VERSION := $(or $(VERSION),$(shell git describe --tags --long --dirty --always))
LDFLAGS := -s -w -X github.com/tomzxcode/ghx/internal/version.version=$(VERSION)
OUTPUT ?= ghx
# Per-user by default so `make install` needs no sudo; use PREFIX=/usr/local for a system-wide install.
PREFIX ?= $(HOME)/.local

.PHONY: build clean install uninstall

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(OUTPUT) .

clean:
	rm -f $(OUTPUT)

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(OUTPUT) $(DESTDIR)$(PREFIX)/bin/$(OUTPUT)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(OUTPUT)

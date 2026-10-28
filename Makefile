CONTAINER ?= podman
# :Z relabels the bind mount for SELinux (Fedora). Set VOLUME_OPTS= on hosts without it.
VOLUME_OPTS ?= :Z
IMAGE ?= go-pcre2-build

.PHONY: generate test bench

# Rebuild internal/wasm/pcre2.wasm.go and internal/wasm/versions.go from the
# pinned inputs in buildtools/wasm/versions.env.
generate:
	$(CONTAINER) build -f buildtools/wasm/Containerfile -t $(IMAGE) .
	$(CONTAINER) run --rm -v "$(CURDIR)/internal/wasm:/out$(VOLUME_OPTS)" $(IMAGE)

test:
	go vet ./...
	go test -race ./...

bench:
	cd bench && go test -run '^$$' -bench . -benchmem

CONTAINER ?= podman
# :Z relabels the bind mount for SELinux (Fedora). Set VOLUME_OPTS= on hosts without it.
VOLUME_OPTS ?= :Z
IMAGE ?= go-pcre2-build
FUZZTIME ?= 30s

.PHONY: generate test bench fuzz fuzzseeds

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

# Each wasm2go fuzz target for FUZZTIME. FuzzDifferential needs the cgo
# backend: go test -tags pcre2_cgo -run '^$$' -fuzz '^FuzzDifferential$$' .
fuzz:
	for t in FuzzCompile FuzzMatch FuzzCallout; do \
		go test -run '^$$' -fuzz "^$$t\$$" -fuzztime $(FUZZTIME) . || exit 1; \
	done

# Regenerate testdata/pcre2-seeds.txt from a PCRE2 checkout: make fuzzseeds PCRE2=../pcre2
fuzzseeds:
	go run ./buildtools/fuzzseeds $(PCRE2) > testdata/pcre2-seeds.txt

# trello: agent-facing Trello CLI (base/patterns/go.md §12).
#
#   make            both binaries for this machine: bin/trello (full) and bin/trello-ro (read-only)
#   make test       go vet + go test -race under every build-tag set
#   make release    cross-compiled binaries for every platform, plus SHA256SUMS
#   make image      container image of the read-only build (IMAGE_TAGS=mcp for the full build)

TOOL     := trello
# Versions come from this tool's own tags, trello/v<semver>; tags of other tools in the repository
# are ignored. The prefix is stripped: trello/v1.0.0 builds version 1.0.0. With no such tag, the
# commit hash is used.
VERSION  ?= $(patsubst $(TOOL)/v%,%,$(shell git describe --tags --match '$(TOOL)/v*' --always --dirty 2>/dev/null || echo 0.1.0-dev))
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GOFLAGS  := -trimpath
export CGO_ENABLED := 0

FULL_TAGS     := mcp
READONLY_TAGS := mcp,readonly
TAGS          ?= $(FULL_TAGS)
TAG_SETS      := "" readonly mcp mcp,readonly

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
DIST      := dist
SHA256    := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo shasum -a 256)

IMAGE      ?= $(TOOL)
IMAGE_TAGS ?= $(READONLY_TAGS)

.PHONY: all build full readonly test verify release checksums image clean

all: full readonly

# Any tag set: make build TAGS=mcp OUT=bin/custom
OUT ?= bin/$(TOOL)
build:
	go build $(GOFLAGS) -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o $(OUT) .

full:
	$(MAKE) build TAGS=$(FULL_TAGS) OUT=bin/$(TOOL)

readonly:
	$(MAKE) build TAGS=$(READONLY_TAGS) OUT=bin/$(TOOL)-ro

test:
	@for tags in $(TAG_SETS); do \
		echo "== go vet + go test -race (tags: $${tags:-none})"; \
		go vet -tags "$$tags" ./... && CGO_ENABLED=1 go test -race -tags "$$tags" ./... || exit 1; \
	done

# Contract conformance against both binaries (skills/scaffold-api-cli/scripts/verify_conformance.sh).
verify: all
	bash ../../skills/scaffold-api-cli/scripts/check_checklist.sh
	bash ../../skills/scaffold-api-cli/scripts/verify_conformance.sh bin/$(TOOL) TRELLO
	bash ../../skills/scaffold-api-cli/scripts/verify_conformance.sh bin/$(TOOL)-ro TRELLO

release:
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; ext=""; \
		[ "$$os" = windows ] && ext=".exe"; \
		for flavour in full readonly; do \
			if [ $$flavour = full ]; then tags="$(FULL_TAGS)"; name=$(TOOL); else tags="$(READONLY_TAGS)"; name=$(TOOL)-ro; fi; \
			out=$(DIST)/$$name-$(VERSION)-$$os-$$arch$$ext; \
			echo "== $$out"; \
			GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -tags "$$tags" -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
		done; \
	done
	$(MAKE) checksums

# Only this version's artifacts, so older builds left in $(DIST) never enter the sums.
checksums:
	cd $(DIST) && $(SHA256) $(TOOL)-$(VERSION)-* $(TOOL)-ro-$(VERSION)-* > SHA256SUMS-$(VERSION) && cat SHA256SUMS-$(VERSION)

image:
	docker build --build-arg TAGS=$(IMAGE_TAGS) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(IMAGE):$(VERSION) .

clean:
	rm -rf bin $(DIST)

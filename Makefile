export PATH := $(HOME)/.local/bin:$(PATH)
GO ?= go

RUN ?= .
PKG ?= ./...
E2E_RUN ?= .
UI_RUN ?= .

.PHONY: build test test-pkg vet fmt-check check mod-verify e2e test-ui test-cpp test-cpp-image lock-check test-contract

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/sil ./cmd/sil

test:
	$(GO) test -race -count=1 ./...

test-pkg:
	$(GO) test -race -count=1 -run '$(RUN)' $(PKG)

vet:
	$(GO) vet ./...
	$(GO) vet -tags sil_e2e ./e2e/
	$(GO) vet -tags sil_ui ./uitest/

# Fails when gofmt would change any Go file (prototype/ is the frozen spike).
fmt-check:
	@out=$$(find . \( -path ./.git -o -path ./prototype -o -path ./.planning -o -path ./.gsd \) -prune -o -name '*.go' -print | xargs -r gofmt -l); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# test-cpp (host g++) is part of check. test-cpp-image runs the same test with the gcc of the pinned
# ESPHome image; it needs docker and the image present, so it is run next to check, not inside it.
check: fmt-check vet test test-cpp

CXX ?= g++
CPP_TEST_FLAGS ?= -std=c++20 -Wall -Wextra -Werror -O1
CPP_IMAGE ?= ghcr.io/esphome/esphome:2026.10.1@sha256:eab8cd6b4f51077c3883af4cedc058280ddc34a94f3b2ffee23e9d2cd5f1db64

# Header-only sim-bus codec against protocol/vectors/frames.tsv, the same file the Go test reads.
test-cpp:
	@set -e; d=$$(mktemp -d); trap 'rm -rf "$$d"' EXIT; \
	$(CXX) $(CPP_TEST_FLAGS) -Icomponents/sim_bus -o "$$d/test_protocol" tests/cpp/test_protocol.cpp; \
	"$$d/test_protocol" protocol/vectors/frames.tsv

test-cpp-image:
	@docker run --rm --pull=never --user "$$(id -u):$$(id -g)" -v "$(CURDIR):$(CURDIR):ro" -w "$(CURDIR)" \
		--entrypoint sh $(CPP_IMAGE) -c \
		'g++ --version | head -n 1 && g++ $(CPP_TEST_FLAGS) -Icomponents/sim_bus -o /tmp/test_protocol tests/cpp/test_protocol.cpp && /tmp/test_protocol protocol/vectors/frames.tsv'

mod-verify:
	$(GO) mod verify

e2e:
	$(GO) test -tags sil_e2e -count=1 -timeout 30m -v -run '$(E2E_RUN)' ./e2e/

# Browser tests of the page (headless Chrome, offline) against a fake bench behind the real web handler.
# Needs Chrome (google-chrome on PATH, or SIL_CHROME); not part of `check`, like e2e.
test-ui:
	$(GO) test -tags sil_ui -count=1 -timeout 10m -v -run '$(UI_RUN)' ./uitest/

# The Python test dependencies are reviewed through the product repo's lock: every package of
# tests/uv.lock must exist in LOCK_REF with the same version and the identical sdist/wheel sha256 set.
# Run it before the first `uv sync` installs anything. Read-only on LOCK_REF.
LOCK_REF ?= ../wifi-fancontroller/hil/uv.lock

lock-check:
	@python3 -I tests/tools/lockdiff.py tests/uv.lock $(LOCK_REF)

# Contract test (TEST-01) and aioesphomeapi smoke test (TEST-02) against real Rev 3.3 bench instances.
# Needs docker with the pinned ESPHome image and uv; not part of `check` (no Python in check).
test-contract: build lock-check
	@cd tests && SIL_BIN=$(CURDIR)/bin/sil uv run --locked pytest

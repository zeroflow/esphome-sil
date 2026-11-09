export PATH := $(HOME)/.local/bin:$(PATH)
GO ?= go

RUN ?= .
PKG ?= ./...
E2E_RUN ?= .

.PHONY: build test test-pkg vet fmt-check check mod-verify e2e

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/sil ./cmd/sil

test:
	$(GO) test -race -count=1 ./...

test-pkg:
	$(GO) test -race -count=1 -run '$(RUN)' $(PKG)

vet:
	$(GO) vet ./...
	$(GO) vet -tags sil_e2e ./e2e/

# Fails when gofmt would change any Go file (prototype/ is the frozen spike).
fmt-check:
	@out=$$(find . \( -path ./.git -o -path ./prototype -o -path ./.planning -o -path ./.gsd \) -prune -o -name '*.go' -print | xargs -r gofmt -l); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt-check vet test

mod-verify:
	$(GO) mod verify

e2e:
	$(GO) test -tags sil_e2e -count=1 -timeout 30m -v -run '$(E2E_RUN)' ./e2e/

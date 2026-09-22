PLUGIN_ID ?= model-sort
VERSION   ?= 0.0.0-dev
DIST      ?= dist
GOOS      ?= $(shell go env GOOS)
GOARCH    ?= $(shell go env GOARCH)

EXT := so
ifeq ($(GOOS),darwin)
EXT := dylib
endif
ifeq ($(GOOS),windows)
EXT := dll
endif

LIB     := $(DIST)/$(PLUGIN_ID).$(EXT)
ARCHIVE := $(PLUGIN_ID)_$(VERSION)_$(GOOS)_$(GOARCH).zip

.PHONY: all fmt vet test build package clean

all: test build

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './.git/*')

vet:
	go vet ./...

test:
	go test ./...

# CGO_ENABLED=1 and -buildmode=c-shared are both mandatory: CPA loads plugins
# with dlopen, so a static or non-cgo build produces a library the host
# silently ignores.
build:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -buildvcs=false -buildmode=c-shared \
			-ldflags '-s -w -X main.pluginVersion=$(VERSION)' \
			-o $(LIB) .
	rm -f $(DIST)/$(PLUGIN_ID).h

package: build
	go run ./.github/scripts/package-release.go \
		-library $(LIB) \
		-archive $(ARCHIVE) \
		-checksum $(ARCHIVE).sha256

clean:
	rm -rf $(DIST) *.zip *.zip.sha256

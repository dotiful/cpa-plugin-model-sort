# AGENTS.md

Guidance for AI coding agents working in this repository.

## Commands

- Run tests: `make test` or `go test ./...`
- Run vet: `go vet ./...`
- Run one test: `go test -run TestSortOpenAICatalog`
- Format: `make fmt` (CI fails on unformatted files)
- Build for the host platform: `make build`
- Build and package a release archive: `make package VERSION=0.1.0`
- Clean build output: `make clean`

Do not run `go test ./...` expecting it to cover `.github/scripts/`; that
directory holds a standalone `package main` tool and is excluded from the module
build by the leading dot in its path.

## Architecture

A single-package Go `c-shared` plugin for CLIProxyAPI, split in two files:

- `abi.go` is the C ABI bridge. It exports `cliproxy_plugin_init`,
  `cliproxyPluginCall`, `cliproxyPluginFree` and `cliproxyPluginShutdown`, then
  forwards every call into `handleMethod`. It contains no plugin logic.
- `main.go` holds the logic: `pluginRegistration` declares metadata and
  capabilities, `interceptResponse` decides whether a response is a catalog, and
  `sortModelCatalog` does the sorting.

The host communicates over JSON envelopes (`{"ok":true,"result":{}}`). The C ABI
passes only method names and byte slices; no Go types cross the boundary.

## Invariants

These are the rules that make the plugin safe. Breaking one is a regression even
if the tests still compile.

- **Never modify a completion.** The only thing separating a catalog from a chat
  response is that `Model` and `RequestedModel` are both empty — CPA's
  `WriteModelListResponse` passes empty strings, while any real completion fills
  `Model`. `TestCompletionsAreNotTouched` locks this down; it must stay green.
- **An empty `Body` in the response means "unchanged".** Any uncertain input
  (unparseable JSON, unknown shape, missing sort keys, fewer than two entries)
  must return an empty response rather than a guess.
- **Sort on `id`, falling back to `name`.** The Gemini listing keys on `name`;
  an id-only comparator leaves it unsorted while appearing to work.
- **Declare only the capabilities that are implemented.** A stray `true` in
  `registrationCapability` makes the host call a method that does not exist.
- **All four metadata fields must be non-empty.** `validPlugin` in the host
  (`internal/pluginhost/host.go`) requires `Name`, `Version`, `Author` and
  `GitHubRepository`. Leaving any empty produces a plugin that logs
  `plugin loaded` but never `plugin registered`, with a single warning line:
  `returned invalid metadata or no capabilities`.
  `TestRegistrationMetadataIsComplete` guards this.
- **Preserve unrelated fields.** Sorting re-encodes the catalog, so top-level
  siblings (`object`) and per-entry fields (`owned_by`, `displayName`) must
  survive the round trip.

## Build constraints

- `CGO_ENABLED=1` and `-buildmode=c-shared` are mandatory. CPA loads plugins
  with `dlopen`, so a non-cgo build is silently ignored by the host.
- Never link statically; a static library cannot be `dlopen`ed.
- Build against the same libc as the target. A musl/Alpine build will not load
  on a glibc host.
- The `.h` file produced by `c-shared` is build output, never shipped.
- The library filename is the plugin ID. `model-sort.so` must stay paired with
  the `model-sort` key under `plugins.configs`, and with `pluginID` in
  `main.go`.

## Release

`pluginVersion` defaults to `0.0.0-dev` and is injected at build time with
`-ldflags "-X main.pluginVersion=$(VERSION)"`. Keep the `go.mod` module path and
`Metadata.GitHubRepository` aligned with the real repository URL; the store
validates the repository field.

Releases are published by pushing a `v<version>` tag. The workflow runs tests,
builds every platform, then creates the GitHub release with one zip per platform
plus `checksums.txt`. Archive names are
`model-sort_<version>_<goos>_<goarch>.zip`, and each archive holds the dynamic
library at the zip root with no nested directories —
`.github/scripts/package-release.go` verifies this before writing the checksum,
because the store installer rejects any other layout.

Do not commit build output: `dist/`, `*.zip`, `*.so`, `*.dylib`, `*.dll`, `*.h`.

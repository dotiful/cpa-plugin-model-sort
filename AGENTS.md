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
- `main.go` holds the logic: `pluginRegistration` declares metadata,
  capabilities and config fields, `applyConfig` parses the plugin's YAML block,
  `interceptResponse` decides whether a response is a catalog, and
  `curateModelCatalog` hides, orders and pins entries in that sequence.

The host communicates over JSON envelopes (`{"ok":true,"result":{}}`). The C ABI
passes only method names and byte slices; no Go types cross the boundary.

Configuration arrives in the `config_yaml` field of the register and reconfigure
calls, carrying the raw YAML of `plugins.configs.model-sort`. It is a `[]byte`
on the host side, so JSON transports it base64-encoded — decode it into a
`[]byte` field and let encoding/json handle that, never into a string. The host
calls reconfigure whenever the configuration reloads, so settings apply without
a restart.

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
- **Sort on `id`, then `slug`, then `name`.** Each listing shape names that
  field differently: the Gemini listing keys on `name` and the Codex client
  listing (`GET /v1/models?client_version=...`) on `slug`. A comparator missing
  any of them leaves that catalog unsorted while appearing to work, because an
  empty sort key makes curation bail out by design. When CPA adds a listing
  shape, check which field its entries carry before assuming it is covered.
- **`hidden` must never affect routing.** It removes entries from catalog
  responses only; a hidden model stays fully requestable. That separation is the
  entire point of the setting versus `force-model-prefix`, and
  `TestHiddenModelStillCompletes` locks it down.
- **Curate in the order hide, sort, pin.** Pinning before sorting would let the
  sort undo the pins, and hiding last would waste work on dropped entries.
- **Keep `matchWildcard` byte-for-byte equivalent to CPA's.** It is copied from
  `sdk/cliproxy/service_models.go`, which backs `oauth-excluded-models`. If the
  two diverge, the same pattern means different things in the host config and in
  this plugin, which is worse than having no wildcards at all.
- **Every advertised config field must be read by the plugin.** A field in
  `ConfigFields` that `applyConfig` ignores shows up in the management panel as
  a setting that silently does nothing; `TestConfigFieldsMatchSettings` keeps
  the two in sync.
- **An absent config block resets to defaults.** Removing a key from
  `config.yaml` must take effect on reconfigure rather than leaving the previous
  value active.
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

Add the `CHANGELOG.md` section before tagging. The release job extracts the
`## <version> — <date>` section and publishes it as the release notes, and fails
when the section is missing, so a release cannot ship undocumented. Write entries
as what changed and why it mattered, not as a restatement of the commit subject.

### Before tagging: reconcile the SDK pin

The `github.com/router-for-me/CLIProxyAPI/v7` version in `go.mod` is the plugin
SDK this library is compiled against. The host **rejects a plugin built against a
newer SDK than itself** and accepts an older one, so a lagging pin is safe to run
but means new host behaviour is invisible to the plugin at compile time — exactly
how the 7.3.15 Codex encoder change became a silent runtime regression rather
than a build error.

Therefore, as part of cutting any release:

1. Compare the pin against the gateway version the release will be verified on.
2. If the gateway is newer, read the intervening diff for changes to the response
   path (encoders, listing builders, interceptor invocation) before deciding.
3. Either bump the pin in the same release, or record in the changelog entry that
   the pin lags deliberately and why.

Do not bump the pin outside a release: it changes the compiled artifact, so it
needs the full platform matrix and live verification like any other change.

**Currently pinned at `v7.3.12` while the reference gateway runs `7.3.15`.** Raise
it to a 7.3.15-or-later SDK in the next release and drop this paragraph.

The platform matrix mirrors the plugin-capable builds CLIProxyAPI itself ships:
linux, darwin and windows on amd64 and arm64, plus freebsd/amd64. Native runners
cover everything except windows/arm64 (`go-cross/cgo-actions`) and FreeBSD,
which needs a hand-fetched sysroot — see the `build-freebsd` job. Do not add
freebsd/arm64: Go cannot build `c-shared` for it.

Do not commit build output: `dist/`, `*.zip`, `*.so`, `*.dylib`, `*.dll`, `*.h`.

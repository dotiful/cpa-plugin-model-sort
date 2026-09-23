# Model Sort

[![Build](https://github.com/dotiful/cpa-plugin-model-sort/actions/workflows/build.yml/badge.svg)](https://github.com/dotiful/cpa-plugin-model-sort/actions/workflows/build.yml)
[![Latest release](https://img.shields.io/github/v/release/dotiful/cpa-plugin-model-sort?sort=semver)](https://github.com/dotiful/cpa-plugin-model-sort/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/dotiful/cpa-plugin-model-sort)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that returns
the model catalog in a stable order instead of a different random order on every
restart, and lets you pin or hide entries.

## The problem

CPA builds its catalog by ranging over a Go map, so `/v1/models` comes back in a
different order every time the process starts. The order is stable *within* one
process, which makes the symptom easy to miss: restart the service and every
client's model picker is reshuffled, bookmarks and muscle memory stop matching,
and diffing two catalog dumps produces noise instead of signal.

Upstream declined to sort the listing —
[issue #3081](https://github.com/router-for-me/CLIProxyAPI/issues/3081) was
converted to
[discussion #3888](https://github.com/router-for-me/CLIProxyAPI/discussions/3888)
and closed — and there is no `model-list-sort` configuration option.

## What it does

The plugin curates model listings on their way out and leaves everything else
untouched. Model listings pass through the
[response interceptor](https://help.router-for.me/plugin/response-interceptor)
chain just like completions do, so no forked binary is required:
[`WriteModelListResponse`](https://github.com/router-for-me/CLIProxyAPI/blob/main/sdk/api/handlers/handlers_interceptors.go)
invokes the chain, and
[`server_models_interceptor_test.go`](https://github.com/router-for-me/CLIProxyAPI/blob/main/internal/api/server_models_interceptor_test.go)
covers it for all three formats.

Covered endpoints:

| Endpoint | Format | Sort key |
| --- | --- | --- |
| `GET /v1/models` | OpenAI | `id` |
| `GET /v1/models` with `Anthropic-Version` or a `claude-cli` agent | Claude | `id` |
| `GET /v1/models` with a `grok-shell` agent | Grok Shell | `id` |
| `GET /v1/models?client_version=...` | Codex client | `slug` |
| `GET /v1beta/models` | Gemini | `name` |

This is a matter of response *format*, not of provider. The plugin curates the
JSON that CPA has already produced, so every provider CPA can serve — OpenAI,
Claude, Gemini, Codex, Qwen, iFlow, Grok and any other — is covered by whichever
of these listings the client asks for.

The Gemini format keys on `name` and the Codex client format on `slug`, so an
id-only comparator would silently leave those listings unsorted.

## Install

### From the plugin store

Install `model-sort` from the CLIProxyAPI Management Center plugin store, then
enable it in `config.yaml` as shown below. The store registry lives in
[CLIProxyAPI-Plugins-Store](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store).

### Manual

Download the archive for your platform from
[Releases](https://github.com/dotiful/cpa-plugin-model-sort/releases), verify it
against `checksums.txt`, and place the library in the plugin directory:

```bash
sha256sum -c checksums.txt --ignore-missing
unzip model-sort_<version>_linux_amd64.zip -d /var/lib/cli-proxy-api/plugins/
systemctl restart cli-proxy-api
```

The library filename is the plugin ID: keep it as `model-sort.so`
(`.dylib` on macOS, `.dll` on Windows).

## Configuration

```yaml
plugins:
  enabled: true
  dir: "/var/lib/cli-proxy-api/plugins"
  configs:
    model-sort:
      enabled: true
```

Both `plugins.enabled` and the per-plugin `enabled` are required — with only the
global switch the plugin is listed but stays inactive.

All plugin settings are optional; the defaults sort ascending and change nothing
else. Settings are re-read on configuration reload, so changing them does not
require a restart.

| Setting | Type | Default | Effect |
| --- | --- | --- | --- |
| `order` | `asc` \| `desc` | `asc` | Sort direction. |
| `pinned` | list of patterns | empty | Kept at the top, in the order listed. |
| `hidden` | list of patterns | empty | Removed from catalog responses. |

```yaml
    model-sort:
      enabled: true
      order: asc
      pinned:
        - anthropic-claude-opus-4-6
        - anthropic-claude-sonnet-4-6
      hidden:
        - legacy-model
        - codex-*
```

`hidden` only edits the listing: hidden models stay fully requestable, which is
the catalog-only filtering asked for in
[issue #5995](https://github.com/router-for-me/CLIProxyAPI/issues/5995) and
[issue #5349](https://github.com/router-for-me/CLIProxyAPI/issues/5349).
`oauth-excluded-models` removes a model from the listing *and* from routing, and
`force-model-prefix: true` hides duplicates but also changes routing — neither
keeps a curated catalog with every model still callable.

`pinned` and `hidden` accept `*` wildcards: `codex-*` matches a whole channel,
`*-preview` every preview model, and `*` the entire catalog. Matching follows
CPA's own matcher (the one behind `oauth-excluded-models`), so a pattern means
the same thing in either place. A pattern without `*` is an exact comparison.
A pinned pattern pins every model it matches, keeping their sorted order, and a
model matched by both `pinned` and `hidden` stays hidden.

Patterns are matched against the ID as written in the catalog. For the Gemini
listing, which reports `models/<id>`, the bare ID matches as well.

Under a hardened systemd unit (`ProtectSystem=strict`), point `dir` at a path
inside `StateDirectory`; `/usr/local` is read-only there.

## Verify

The service log must show `plugin registered`, not merely `plugin loaded`:

```
pluginhost: plugin registered plugin_id=model-sort plugin_name=model-sort version=0.4.1
```

Then confirm the catalog is ordered:

```bash
curl -s -H "Authorization: Bearer $CPA_API_KEY" http://127.0.0.1:8317/v1/models \
  | python3 -c 'import sys,json;i=[m["id"] for m in json.load(sys.stdin)["data"]];print(i==sorted(i))'
```

Restart the service a few times and re-run it; the answer must stay `True`.

## Requirements

- [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) built with plugin
  support. Any Management API response carries `X-CPA-SUPPORT-PLUGIN: 1` when the
  running binary supports plugins; `0` means the binary was built without cgo and
  will ignore every `.so`. The official
  [releases](https://github.com/router-for-me/CLIProxyAPI/releases) marked
  `no-plugin` are the ones to avoid.
- Verified against CLIProxyAPI 7.3.15. The plugin is built against the SDK
  version pinned in [`go.mod`](go.mod); the host rejects a plugin built against a
  *newer* SDK than itself, so an older pin stays compatible.

Releases cover the same platforms CLIProxyAPI itself publishes a plugin-capable
build for: linux, darwin and windows on amd64 and arm64, plus freebsd/amd64.

## Build from source

```bash
make test      # go vet + go test
make build     # dist/model-sort.<ext> for the host platform
make package   # release zip + sha256 checksum
```

`CGO_ENABLED=1` and `-buildmode=c-shared` are mandatory, and the build must stay
dynamically linked: CPA loads plugins with `dlopen`, which a static binary
cannot do. Build against the same libc as the target — a musl/Alpine build will
not load on a glibc host.

The [`registry.json`](registry.json) in this repository is a reference copy of
the store entry, kept here so changes can be reviewed alongside the code. The
authoritative entry lives in
[CLIProxyAPI-Plugins-Store](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store);
editing the copy here does not change what the store serves.

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## Contributing

Bug reports and pull requests are welcome — see
[CONTRIBUTING.md](CONTRIBUTING.md) for the toolchain constraints (CGO, no static
linking) and [AGENTS.md](AGENTS.md) for the architecture and the invariants any
change must preserve. Participation is covered by our
[Code of Conduct](CODE_OF_CONDUCT.md).

Please report security issues privately rather than in a public issue; see
[SECURITY.md](SECURITY.md).

## License

MIT

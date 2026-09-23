# Contributing

Thanks for taking the time to contribute. This is a small, focused plugin, so
this guide is short and specific.

## Before you start

For anything beyond a typo, **open an issue first**. The plugin deliberately
does one thing — curate model listings — and the most useful outcome of a
discussion is often "this belongs in CPA itself" or "this is already covered by
`pinned`". That is worth finding out before you write code.

Bug reports are far more actionable with the plugin version, the CLIProxyAPI
version, and the actual response body you got. The issue templates ask for
exactly that.

## Development setup

The plugin is a CGO `c-shared` library, which constrains the toolchain more than
a normal Go module:

- **Go**: the patch version pinned in `go.mod` (currently `1.26.8`). CI installs
  it via `go-version-file: go.mod`, and `govulncheck` fails on the stdlib
  advisories present in earlier patches — so do not loosen it.
- **CGO_ENABLED=1** is mandatory. A `CGO_ENABLED=0` build produces a library the
  host loads and then silently ignores.
- **Never link statically.** A static library cannot be `dlopen`ed, so plugins
  built with `-extldflags '-static'` never register.
- Build on the **same libc family** as your target. For a Debian gateway, build
  on Debian — an Alpine/musl build will not load.

If you do not want to install a toolchain locally, build in a container:

```bash
docker run --rm -v "$PWD":/w -w /w -e GOFLAGS=-buildvcs=false \
  golang:1.26-bookworm sh -c 'go vet ./... && go test ./...'
```

## The loop

```bash
make fmt      # CI fails on unformatted files
make vet
make test     # 35 tests, all must pass
make build    # host-platform library into dist/
```

`make package VERSION=x.y.z` produces the release archive layout CI publishes.

Note that `go test ./...` does not cover `.github/scripts/`: that directory holds
a standalone `package main` tool, excluded from the module by the leading dot.

## Making a change

**Read [`AGENTS.md`](AGENTS.md) first.** It documents the architecture and, more
importantly, the invariants — the rules that keep the plugin safe. They apply to
human contributors exactly as much as to AI agents. The short version:

- Never modify a completion. Only model listings may be rewritten.
- An empty response body means "unchanged"; when uncertain, change nothing.
- Sort on `id`, then `slug`, then `name` — each listing shape names it
  differently, and a missing key leaves that catalog silently unsorted.
- `hidden` removes entries from the catalog only and must never affect routing.

A change that breaks one of these is a regression even if the tests compile.

### Tests

Every behavioural change needs a test. Follow the existing style in
`main_test.go`: table-free, one scenario per function, with a comment explaining
*why* the case matters — several tests exist because a real listing shape was
missed, and that context is the point.

Unit fixtures are small by nature. If your change affects output **size or
encoding**, verify it against a real catalog captured from a live gateway; a
200-byte fixture will not reveal a regression that a 2 MB Codex catalog does.

### Commits

Conventional commits, matching the existing history:

```
fix(codex): keep curated catalogs unescaped to match the host encoder
```

Explain the *why* in the body. Reference upstream issues where relevant.

## Pull requests

Fill in the PR template. It asks you to confirm tests pass, `CHANGELOG.md` is
updated, and documentation still matches the code.

Documentation drift is treated as a real defect here: sample log lines pin old
versions and endpoint tables age badly. If your change alters behaviour, check
`README.md` and `AGENTS.md` against the code rather than against memory.

Releases are cut from `CHANGELOG.md` — CI extracts the section matching the tag
and fails when it is missing, so an undocumented release cannot ship. Add your
entry under a new version heading, or to the existing unreleased one.

## Reporting security issues

Do not open a public issue. See [`SECURITY.md`](SECURITY.md).

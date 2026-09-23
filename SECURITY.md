# Security policy

## What this plugin can access

A CLIProxyAPI plugin is a **shared library loaded into the gateway process**, not
a sandboxed extension. It runs with the gateway's privileges and, through the
response interceptor chain, sees the response bodies it is asked to curate.

Model Sort is deliberately narrow within that access:

- It acts **only** on model listings. A request is treated as a listing when the
  host passes an empty `Model` *and* `RequestedModel`, which `WriteModelListResponse`
  does; anything else returns an empty body, meaning "unchanged". Completions and
  streams are never inspected or rewritten, and a test pins that behaviour.
- It reads `id`, `slug` and `name` to order entries. No other field is parsed.
- It performs **no network I/O, no disk I/O and no subprocess execution**, and
  keeps no state between requests beyond its own configuration.
- Its configuration (`order`, `pinned`, `hidden`) holds model-name patterns only.
  It never touches credentials, API keys or auth files.

`hidden` removes entries from the **catalog only**. Hidden models stay fully
routable, so it is a display preference, not an access control. Do not rely on
it to restrict which models a key may call — CPA cannot filter listings per key
(upstream issue #4949), and a hidden model still answers completions.

## Supported versions

Fixes land on the latest released version. There are no long-term support
branches; upgrade to the newest tag before reporting an issue.

| Version | Supported |
| --- | --- |
| 0.4.x | Yes |
| < 0.4.0 | No |

## Verified platforms

Release CI builds and packages each tag for the platforms below. `linux/amd64`
additionally receives live verification on a production gateway before release.

| Platform | Built in CI | Notes |
| --- | --- | --- |
| linux/amd64 | Yes | Also verified live on a running gateway |
| linux/arm64 | Yes | Native runner |
| darwin/amd64 | Yes | Native runner |
| darwin/arm64 | Yes | Native runner |
| windows/amd64 | Yes | Native runner |
| windows/arm64 | Yes | Cross-compiled |
| freebsd/amd64 | Yes | Cross-compiled against a FreeBSD 14.5 sysroot |

`freebsd/arm64` is absent because Go cannot build `c-shared` for that target.
Platforms outside this matrix are unverified and may not receive fixes.

The plugin is built against the CLIProxyAPI plugin SDK pinned in `go.mod`. The
host rejects a plugin whose `schema_version` is higher than its own, so building
against an SDK **older** than your gateway is supported; the reverse is not.

## Verifying a release artifact

Every release ships `checksums.txt` in `sha256sum` format. Verify before
installing:

```bash
sha256sum -c checksums.txt --ignore-missing
```

Each archive contains exactly one library at the archive root. An archive with
nested directories or extra libraries did not come from this project's CI.

Prefer installing through the official CLIProxyAPI plugin store, which resolves
this repository's GitHub releases, over copying binaries from elsewhere.

## Reporting a vulnerability

Please report security issues **privately**, not in a public issue.

- Open a private security advisory:
  https://github.com/dotiful/cpa-plugin-model-sort/security/advisories/new

Include:

- the plugin version and the CLIProxyAPI version,
- OS and architecture,
- your `plugins.configs.model-sort` block, with any sensitive values redacted,
- a reproduction: the request made and the response body observed.

Expect an initial response within 7 days. If a report is confirmed, the fix ships
in a new tagged release with the details recorded in `CHANGELOG.md`.

Since this repository is maintained by a single author, please do not expect a
guaranteed remediation timeline. Reports that require coordinated disclosure are
welcome to propose one in the advisory thread.

## Out of scope

- Vulnerabilities in CLIProxyAPI itself — report those to
  [the upstream project](https://github.com/router-for-me/CLIProxyAPI/security).
- The fact that `hidden` does not restrict routing. That is documented, intended
  behaviour, not a flaw.

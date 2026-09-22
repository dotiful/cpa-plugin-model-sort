# Changelog

All notable changes to this project are documented here. Versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html), and each release is
published from the matching `v<version>` git tag.

## 0.3.0 — 2026-09-22

Adds wildcard patterns to `pinned` and `hidden`.

- **`pinned` and `hidden` accept `*` wildcards.** Exact IDs alone could not
  express "hide this whole channel", which is the headline request in
  [issue #5995](https://github.com/router-for-me/CLIProxyAPI/issues/5995):
  `oauth-excluded-models` with `"*"` empties the listing *and* breaks routing,
  and the issue records no workaround that keeps both. `hidden: ["codex-*"]`
  now does exactly that, and `"*"` clears the catalog while every model stays
  callable.
- **Matching mirrors CPA's own matcher** in `sdk/cliproxy/service_models.go`,
  the one behind `oauth-excluded-models`, down to how middle segments are
  consumed in order. A pattern therefore means the same thing whether it is
  written for the host or for this plugin. A pattern without `*` remains an
  exact comparison, so existing configurations are unaffected.
- **A pinned pattern pins every model it matches**, keeping their sorted order
  relative to each other, and a model matched by several patterns is pinned
  once. Hiding still wins over pinning.

## 0.2.0 — 2026-09-22

Adds the first configuration surface, and restores the FreeBSD build.

- **Three settings, declared to the host: `order`, `pinned` and `hidden`.**
  They are advertised through `Metadata.ConfigFields`, so the Management Center
  renders a form for them instead of requiring hand-edited YAML. Sorting alone
  was not enough: alphabetical order buries whatever a given operator actually
  uses behind whichever provider prefix sorts first.
- **`pinned` lifts named models to the top**, in the order they are listed,
  leaving the rest of the catalog sorted underneath.
- **`hidden` drops entries from catalog responses without touching routing.**
  A hidden model stays fully requestable; only the listing changes. This is the
  catalog-only filtering asked for in
  [issue #5349](https://github.com/router-for-me/CLIProxyAPI/issues/5349), which
  `force-model-prefix` cannot provide because it also changes how requests are
  routed.
- **Settings apply without restarting the service.** CPA calls
  `plugin.reconfigure` on every configuration reload and passes the plugin's
  YAML block, so edits to `config.yaml` take effect on the next reload. Removing
  a key restores its default rather than leaving the previous value in place.
- **Ship `freebsd/amd64` again.** CLIProxyAPI publishes a plugin-capable FreeBSD
  build, so a plugin that skips the platform cannot be installed there. The
  target had been dropped in 0.1.0 because the cross-build action pinned an EOL
  sysroot; the sysroot is now fetched directly from a currently published
  release. Releases carry seven archives: linux, darwin and windows on amd64 and
  arm64, plus freebsd/amd64. `freebsd/arm64` remains absent because Go cannot
  build `c-shared` for it.

## 0.1.0 — 2026-09-22

Initial release.

- **Model listings come back in a stable order.** CPA builds its catalog by
  ranging over a Go map, so `/v1/models` is reshuffled on every process start —
  stable within one process, which makes the symptom easy to miss until a
  restart rearranges every client's model picker. Upstream declined to sort the
  listing in [issue #3081](https://github.com/router-for-me/CLIProxyAPI/issues/3081),
  and no configuration option covers it.
- **All three catalog formats are handled**: OpenAI (`/v1/models`), Gemini
  (`/v1beta/models`) and the Claude listing. Entries sort on `id`, falling back
  to `name`, because the Gemini listing keys on `name`.
- **Runs on stock CLIProxyAPI.** Model listings pass through the response
  interceptor chain, so no forked binary and no rebuild after an upstream
  upgrade is needed. Verified against CLIProxyAPI 7.3.12.

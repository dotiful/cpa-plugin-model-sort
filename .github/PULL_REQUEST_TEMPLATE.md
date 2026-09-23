<!--
Thanks for contributing. Keep the summary short; the checklist matters more.
For anything beyond a typo, please link the issue this implements.
-->

## What this changes

<!-- One or two sentences. Explain the why, not just the what. -->

Closes #

## How it was verified

<!--
Paste real output, not a claim. For behavioural changes, the test name is
enough; for size or encoding changes, include the byte counts you measured
against a real catalog.
-->

```
make test
```

## Checklist

- [ ] `make fmt`, `make vet` and `make test` all pass
- [ ] New behaviour is covered by a test, with a comment explaining why the case matters
- [ ] `CHANGELOG.md` has an entry under the appropriate version heading
- [ ] `README.md` and `AGENTS.md` were re-checked against the code, not from memory
- [ ] The invariants in `AGENTS.md` still hold — completions untouched, empty body means unchanged, sort keys cover `id`/`slug`/`name`, `hidden` does not affect routing

<!--
If this changes output size or encoding, verify against a real catalog captured
from a live gateway. Unit fixtures are too small to reveal that class of bug.
-->

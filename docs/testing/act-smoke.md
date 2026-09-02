# Optional act smoke for the Integration Gate

`mise run ci:act:smoke` runs one selected job from
`.github/workflows/pr-integration-gate.yml` (the shadow Integration Gate)
locally with [nektos/act](https://nektosact.com).

## What it is for

- A thin debugging aid for **workflow wiring**: step ordering, job
  conditions, expressions, and shell blocks.
- Debugging a single job without pushing a revision to retrigger CI, for
  example `mise run ci:act:smoke -- --job web-e2e`. Without arguments it
  runs the cheapest job (`classify`).
- The task generates a minimal `pull_request` event payload (base = merge
  base with `origin/main`, head = `HEAD`) so the classifier job can run.
  Note that `--job` runs one job without its `needs`, so conditions that
  read the classifier outputs may skip the selected job; run the whole
  workflow (no `--job`) to exercise the full chain.

## What it is not

- **Not authoritative.** The only Integration Gate result that matters is the
  `PR / Integration Gate` check produced by GitHub-hosted runners. act output
  has no bearing on a pull request.
- **Not required.** It is not part of `mise run ci:fast`, `ci:integration`,
  or `ci:full`, and no workflow, hook, or review step invokes it.
- **Not a CI reimplementation.** act runs jobs in local Docker containers, so
  runner images, caches (pnpm, Go, Cargo, Playwright browsers, pinned mpv),
  secrets, and artifacts all behave differently or not at all. Keep the task
  limited to wiring checks; do not extend it to reproduce CI policy.

## Requirements

- [nektos/act](https://nektosact.com/installation) on `PATH`
- Docker (or a compatible container engine) running locally

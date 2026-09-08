# Local storage and CI isolation

Mise automatically routes local build outputs outside the checkout. Each worktree owns its mutable outputs; reusable dependency caches remain shared. Existing commands such as `mise run build`, `mise run desktop:test`, and `mise run ci:fast` remain the public interface.

Moving outputs outside Git does not itself save disk space. Shared compiler caches reduce repeated compilation, and explicit cleanup reclaims obsolete worktree outputs. Separate worktrees still need separate final binaries and build directories.

## Modes and layout

| `EARTHLY_STORAGE_MODE` | Behavior |
| --- | --- |
| `auto` (default) | Selects `ci` when `GITHUB_ACTIONS=true`, `CI=true`, or `CI=1`; otherwise selects `local`. |
| `local` | Enables external worktree storage and local tool wrappers. |
| `ci` | Preserves existing CI build, cache, and artifact paths. |
| `clean-room` | Disables local defaults; the clean-room runner supplies disposable caches. |
| `legacy` | Disables local routing and uses previous checkout-relative defaults. |

The root defaults to `${XDG_CACHE_HOME:-$HOME/.cache}/earthly-audio`; override it with `EARTHLY_CACHE_ROOT`. Resolving paths does not install tools, migrate files, or clean storage.

```text
<root>/clones/<clone-id>/
  owner.json
  locks/
  shared/turbo/
  worktrees/<worktree-id>/
    owner.json
    build/
      cargo-target/
      web/
      docs/
      bin/server
      sidecar/
      server-staging/
    cache/vite/node_modules/.vite/
    runs/<run-id>/
```

The clone ID hashes the canonical Git common-directory path; the worktree ID hashes the canonical checkout path. Linked worktrees share a clone namespace. Switching branches within a checkout reuses storage; moving the checkout changes its identifier.

The external Vite cache retains a `node_modules` path component so React/Babel plugins recognize optimized dependencies and avoid transforming them as application source.

Local production server builds copy current server sources, including uncommitted edits, into external staging and embed generated web/docs distributions there. Runtime music and application data are excluded from staging. Desktop commands consume the resolved frontend, sidecar, and target paths.

Explicit tool-path overrides such as `CARGO_TARGET_DIR` are preserved. Paths outside managed worktree storage are not migration or cleanup targets. Keep hand-maintained files and application data outside the managed root: its owned build, cache, and run directories are disposable.

## Shared caches and validation

Cargo downloads, Go module/build caches, pnpm's package store, and Playwright browser installations retain their normal external locations. Each worktree retains its own `node_modules` links and Rust target. Turbo's local cache is shared within a clone.

Local web/docs build tasks force execution instead of reading a cached Turbo build result. This recreates external distributions after cleanup. Checks and unit tests retain existing Go/Turbo cache behavior; GitHub retains existing build-result caching.

Pre-push still runs the complete `mise run ci:fast` policy. Valid cached test results may be reused under that policy. A compiler-cache hit only reuses compiled code; it does not establish that tests passed.

```bash
mise run cache:setup
```

This installs optional `sccache@0.17.0` through Mise and records its executable under the user's XDG cache directory. The next Mise invocation enables it when there is no existing Rust compiler wrapper. Its shared server defaults to `SCCACHE_CACHE_SIZE=10G`; local incremental compilation defaults to disabled when this wrapper is enabled. Explicit settings and existing wrappers are preserved. Debug information and release profiles are unchanged.

External storage works before sccache is installed. Desktop compilation reports the inactive cache and setup command. sccache does not cache every final linking operation, so separate targets still consume space.

## Locks and test artifacts

Use Mise tasks or `mise exec -- …` to receive the storage environment. Local `cargo`, `go`, and `pnpm` wrappers acquire the worktree's shared storage lock, protecting outputs from concurrent cleanup:

```bash
mise exec -- cargo test --manifest-path desktop/src-tauri/Cargo.toml
mise run web:test
```

An absolute tool executable invoked outside this environment bypasses the wrappers. Avoid concurrent cleanup with such commands.

Test tasks allocate run directories for generated reports, coverage, Playwright traces/screenshots, temporary databases, and managed-import fixtures. Nested tasks reuse the current run. Go tests use a stable worktree-specific `cache/go-test-tmp` parent because Go includes observed `TMPDIR`/`GOTMPDIR` values in cached test inputs. `t.TempDir()` still creates unique directories per test and removes them; Go compilation scratch also receives this stable parent through `GOTMPDIR` and uses unique child directories.

The physical run temporary directory is exported as `TMPDIR`, preserving Managed Storage containment checks. A short `/tmp/earthly-…` symlink is used only for mpv Unix sockets through `EARTHLY_TMP_ALIAS`. The persistent sccache server uses a separate user-level temporary directory. The wrapper removes the alias when the run finishes; retained artifacts remain available for inspection.

Web, docs, and server production builds also serialize per artifact within a worktree. Lock file descriptors are closed in child processes so the persistent sccache server cannot retain a worktree lock.

Browser integrations serialize around fixed ports and refuse to reuse an existing server in local managed runs. Ordinary unit tests and builds in different worktrees remain parallel. Stop an existing development server before retrying a browser task that reports an occupied port.

## Management and migration

Management commands operate in local mode. Inspection and previews do not create storage or change existing files.

| Command | Purpose |
| --- | --- |
| `mise run cache:status` | Show resolved paths, allocated sizes, and compiler-cache status. |
| `mise run cache:migrate` | Preview recognized checkout outputs that can move into managed storage. |
| `mise run cache:prune` | Preview orphan worktree storage and run artifacts older than seven days. |
| `mise run cache:clean` | Preview removal of this worktree's generated build, cache, and run directories. |

Apply a reviewed operation explicitly:

```bash
mise run cache:migrate -- --apply
mise run cache:prune -- --apply
mise run cache:clean -- --apply
```

Migration is never automatic. Relocated Cargo build scripts can retain absolute paths. Migration moves their `build` and `.fingerprint` directories to `build/cargo-migration-backup/<id>/` so Cargo regenerates them; dependency artifacts and incremental data are retained. For an already moved target, preview `mise run cache:migrate -- --refresh-cargo`, then add `--apply` to refresh its metadata. It recognizes old Rust targets, web/docs distributions, the server binary, and Desktop sidecars. It moves sources only into unoccupied managed destinations, skips custom or occupied destinations, and refuses tracked files and symbolic paths. It never merges build trees. Existing incremental files move with the target and remain until cleanup; validate a migrated build before discarding old artifacts. A cross-filesystem rename can fail: migration does not silently copy tens of gigabytes.

Owner records identify managed directories. Cleanup and pruning validate ownership and acquire exclusive locks; active builds block removal. Remove obsolete Git worktrees normally, then use `cache:prune` to reclaim their external storage. Pruning active worktrees removes only expired runs. Cleanup is explicit, with no scheduled service.

Shared dependency caches and clone-level Turbo cache are preserved. These commands do not delete actual music, application databases, tracked fixtures, or generated API sources in the checkout. Removing generated binaries can make the next build slower.

## CI, clean-room, and rollback

GitHub CI keeps existing Cargo targets, cache restore/save paths, uploaded artifacts, and verification policy. Local sccache setup is not installed or enabled there. Clean-room verification clears inherited local routing and Rust compiler wrappers before creating its own caches:

```bash
mise run --skip-tools ci:clean-room
```

The clean-room runner removes disposable state after success, failure, or interruption. An explicit caller temporary-directory setting is preserved rather than treating a developer cache as its own storage.

For local rollback:

```bash
EARTHLY_STORAGE_MODE=legacy mise run build
```

Legacy mode does not move external files back. It may rebuild the old checkout-relative target. For unexpected paths, inspect `cache:status` and explicitly exported tool-path variables. Changing `EARTHLY_CACHE_ROOT` leaves the previous root intact; it does not automatically migrate storage.

## Rollout validation (2026-09-09)

The local rollout exercised the real Linux toolchain, not only configuration snapshots:

- The fast gate passed, including Rust tests/Clippy, workspace checks/tests, and Go tests. Generated API source drift checks passed.
- All 21 browser tests, the production smoke test, HLS playback, four pinned-mpv tests, and the Desktop managed-import parity test passed.
- Production web/docs/server/Desktop builds passed. HTTP responses from the production server matched the external web/docs index files byte for byte. Removing the external web distribution and building again regenerated it without a Turbo result-cache hit.
- Clean-room fixtures verified success, failure, interruption, wrapper removal, and inherited-path isolation. The hosted workflows run the storage regression suite while retaining existing check names and Cargo/artifact locations.
- Two disposable linked worktrees built a small Rust library with a shared source dependency. After clearing both isolated targets, simultaneous rebuilds took 0.170 and 0.171 seconds, compared with initial 0.359 and 0.212 second builds. The shared server recorded four Rust cache hits; each target used 98,304 allocated bytes. Removing the worktrees and applying prune removed both owned output trees.
- An unchanged Go package using `t.TempDir()` returned `(cached)` on its second invocation, despite separate storage-run records.

The small Rust fixture demonstrates reuse and isolation; its timings are not a benchmark for the full Desktop application. Project source crates with different absolute paths do not necessarily share compiler entries. Dependencies compiled from the same shared download/source location can reuse them. See [sccache's Rust limitations](https://github.com/mozilla/sccache/blob/v0.17.0/docs/Rust.md).

The main target was moved rather than copied. Old incremental artifacts and relocated metadata backups remain available for validation/rollback and are reported by `cache:status`; they are not counted as disk savings. No actual music or application database was migrated or deleted.

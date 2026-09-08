# Shadow gate follow-up validation — 2026-09-08

Follow-up to [issue #84](https://github.com/ardamayk/test-project-with-ai/issues/84) and the [initial evaluation](shadow-ci-evaluation-2026-09-08.md). Controlled validation uses [draft PR #138](https://github.com/ardamayk/test-project-with-ai/pull/138). The legacy comparison remains `e2e` plus `lint-test`; no ruleset is modified.

## Why the original failure took 4:09

The [failed Real pinned mpv job](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028330529/job/101473235163) reached the import failure at 10:50:15 UTC, 249 seconds after workflow creation at 10:46:06.

| Stage | Duration | Evidence |
| --- | --- | --- |
| Classification, scheduling and small setup steps | 33s total | Remaining elapsed time outside the four large stages |
| Install Tauri/Linux dependencies | 81s | 286 new packages, 166 MB download, 556 MB installed |
| Restore Cargo cache | 52s | Exact hit; 2,082,936,351-byte compressed archive, about 13s downloading and 39s unpacking |
| Real mpv step | 50s | Cargo compilation 44.32s; actual mpv tests 2.45s |
| Managed Import parity | 33s | Test execution 32.68s; assertion expected `possible_duplicate` but returned `none` |

The Cargo cache did **not** miss in this run. Both Cargo and pinned mpv caches hit, and mpv source compilation was skipped. Changing cache-hit policy would not address the measured 52-second extraction cost. The mpv command compiled unrelated integration targets before running its library-only filter. The subsequent parity test built the Music Server and Go fixtures without restoring the trusted Go cache.

## Targeted changes

- Restore the existing trusted Go module/build cache in the Real pinned mpv job.
- Run Managed Import parity before compiling/running the mpv tests, retaining mpv coverage even after a parity failure.
- Add `--lib` to the public `test:mpv` command. Its existing `playback::tests` filter only selects library tests; the separate Desktop unit matrix retains integration-test coverage.
- Overlap dependency installation with cache restore; wait for its recorded exit status before compilation. A failed installation blocks native tests and its log is retained.
- Install only declared apt dependencies and their dependencies in that job, using `--no-install-recommends` to avoid unrelated recommended packages.
- Include `import_parity_outcome` in gate diagnostics so a parity-only failure links to the retained native logs.
- Use `!cancelled()` for selected integration jobs. GitHub [re-evaluates running job conditions during cancellation](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-cancellation); `always()` allowed the previous jobs to continue.
- Rename the actual Fast Gate check to `PR / Fast Gate`, matching the existing issue requirement.

No new job matrix, broad cache redesign, or blanket test retry was introduced.

## Local verification

The three `govet` findings were already fixed in main by commit [`da40a67`](https://github.com/ardamayk/test-project-with-ai/commit/da40a672e961d2dfb76ec8060f6157cb64aeb804). The initial evaluation used an older checkout. Verification was moved to an isolated worktree based on `34a84d5`, after the shared checkout switched branches during measurement; the interrupted/shared-checkout measurement is excluded.

The recorded non-desktop command is unchanged:

```sh
mise run workspace:check ::: workspace:typecheck ::: workspace:test ::: server:check ::: server:test ::: git-hooks:test ::: classifier:test ::: main-workflows:test ::: clean-room:test
```

- First successful isolated run: **46.678s**.
- Second, warm run: **2.858s**, all tasks successful. After fault removal and the final setup changes, repeat runs passed in **2.586s** and **2.656s**. Turbo and Go cached results are intentionally part of this warm-gate measurement.
- Native Managed Import parity: **1 passed**, 1.24s test execution after 13.21s Cargo preparation.
- Real pinned mpv: **4 passed**, 2.90s test execution using the library-only command.
- New regression checks fail against the original integration workflow and pass against the updated workflow. They cover cancellation guards and actual summary-script behavior for a parity-only failure.

## Hosted cancellation experiment

Clean revision `959850d` was pushed first. While both gates had active non-classifier jobs, revision `650beb6` was pushed at 18:43:26 UTC with temporary failures. The old runs completed as `cancelled`:

- [Fast Gate 34264628490](https://github.com/ardamayk/test-project-with-ai/actions/runs/34264628490): Workspace, Music Server and Desktop jobs were cancelled; generated drift had already completed.
- [Integration Gate 34264628503](https://github.com/ardamayk/test-project-with-ai/actions/runs/34264628503): Web E2E and Real mpv stopped at 18:44:12; Desktop unit stopped at 18:44:15. HLS had already completed.

The replacement incurred queue delay while the prior jobs stopped. The old aggregation jobs ran and reported failure for cancelled upstream jobs; the workflow conclusions were correctly `cancelled`. Cancellation is demonstrated at both workflow and running-job boundaries, not inferred solely from configuration.

## Controlled failure evidence after optimization

The second failure revision (`ad912c8`) was dispatched after the prior gate runs finished, avoiding the cancellation queue delay in the first probe.

| Probe | First failed step from dispatch | Gate conclusion | Retained artifact |
| --- | --- | --- | --- |
| [Fast Gate 34265540202](https://github.com/ardamayk/test-project-with-ai/actions/runs/34265540202) | **2:51** | failure | `fast-gate-music-server-logs` |
| [Integration Gate 34265540252](https://github.com/ardamayk/test-project-with-ai/actions/runs/34265540252) | **2:09** | failure | `integration-gate-real-mpv-logs` |

The native failure is the same `none` versus `possible_duplicate` assertion as the historical case. Cargo preparation took 20.43s and test execution 3.94s. The mpv tests still succeeded afterward, exercising the parity-only diagnostic-summary path.

Native installation began at 18:52:07 UTC while Cargo restored from 18:52:12 through 18:52:51. The dependency barrier completed at 18:53:14, so the former serial setup waits now overlap. Apt installed 264 packages (132 MB downloaded, 494 MB installed), versus 286 packages/166 MB/556 MB in the historical run. Runner/network differences also affect these timings; the full 120-second reduction from 4:09 to 2:09 should not be attributed to any single change in isolation.

The native artifact contains `desktop-import-parity.log`, `desktop-mpv.log`, and `native-ci-dependencies/install.log`. The gate's summary environment reports mpv success and import parity failure; the regression test executes that same summary script and asserts the native artifact link. The Fast Gate artifact identifies the deliberately failing Go test. Archive integrity and bounded secret-pattern checks passed for the downloaded controlled artifacts; the same scan limitations as the initial report apply.

Both temporary test changes were reverted in `8eca198`. They exist only in historical probe revisions, not in the PR's final diff. Git hooks were bypassed for the controlled commits/pushes because intentional failing tests are the purpose of the experiment; explicit local checks and hosted results are recorded instead.

## Updated evaluation

The specific native failure now appears before three minutes, the isolated warm local gate is below 60 seconds, both renamed/stable gate names are observed, and cancellation and intentional failure propagation have hosted evidence. No ruleset changes or merge were performed.

**Do not claim a new representative p95 or unconditional GO from these controlled probes.** The original 20-revision sample remains historical evidence; the optimized configuration has one controlled failed revision with two injected failure paths. The first probe also includes supersession queue delay (native 5:03, Fast 3:06) and is retained rather than silently excluded. Track subsequent representative revisions to establish the new distribution. Clean-head CI results are published in the issue update and PR checks separately from the intentional-failure observations.

## Owner-authorized cutover

After reviewing the evidence, the repository owner explicitly requested removal of duplicate legacy CI and enforcement of the new gates on 2026-09-08. This supersedes the earlier wait-for-more-samples recommendation: the missing representative post-change p95 evidence is accepted as a follow-up risk, not asserted to be satisfied. PR #138 removes the legacy workflow and the ruleset requires `PR / Fast Gate` and `PR / Integration Gate`; main/nightly verification remains in place.

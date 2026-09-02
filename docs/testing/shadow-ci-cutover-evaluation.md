# Shadow CI cutover evaluation

Issue: [#84](https://github.com/ardamayk/test-project-with-ai/issues/84)  
Evaluation date: 2026-09-02  
Decision: **NO-GO**

Do not change the repository ruleset yet. Only five pull-request revisions have
run both shadow gates, the expected `PR / Fast Gate` context did not appear,
the proposed-gate green p95 is above ten minutes, the only observed first
failure arrived after three minutes, and required behavioral evidence remains
missing.

## Method

The sample includes every pull-request revision available at evaluation time
where the old required CI and both shadow workflows ran together. GitHub
Actions run and job timestamps are the source of truth.

- Elapsed time starts at workflow `created_at` and ends at the relevant job's
  `completed_at`.
- Proposed-gate green time is when both shadow aggregator jobs have completed
  successfully. A failed revision has no green time.
- Existing-required green time is when both `e2e` and `lint-test` have completed
  successfully.
- First-failure time is the earliest failed shadow job completion.
- Runner minutes sum wall time from `started_at` to `completed_at` for runnable
  jobs. Skipped jobs are excluded. This is an estimate, not GitHub billing data.
- Queue statistics use `started_at - created_at` for every runnable job.
- p50 and p95 use the nearest-rank method. With this undersized sample, p95 is
  effectively the worst observed value and must not be treated as stable.
- Cache rates count explicit `Cache hit for:` and `Cache not found for input
  keys:` messages for repository task caches. Mise tool-cache restoration is
  excluded because it is not one of the task/dependency caches under review.

## Sample

| PR | Revision | Classifier reasons | Selected integration work | Fast Gate | Integration Gate | Proposed green | Existing required green | Shadow runner min | Old runner min |
| --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: |
| [#94](https://github.com/ardamayk/test-project-with-ai/pull/94) | `7d8a4b7` | documentation, global, HLS, web | all four jobs | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33623952109) | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33623952180) | 5:31 | 5:22 | 21.4 | 7.2 |
| [#94](https://github.com/ardamayk/test-project-with-ai/pull/94) | `0fdbefc` | documentation, global, HLS, server, web | all four jobs | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33625576589) | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33625576593) | 5:18 | 5:30 | 20.2 | 8.2 |
| [#87](https://github.com/ardamayk/test-project-with-ai/pull/87) | `5122e8b` | documentation | none | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33627535739) | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33627535718) | 1:47 | 8:44 | 3.0 | 11.1 |
| [#95](https://github.com/ardamayk/test-project-with-ai/pull/95) | `9426c97` | global | all four jobs | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33629717084) | [failure](https://github.com/ardamayk/test-project-with-ai/actions/runs/33629717227) | — | 11:37 | 20.1 | 16.7 |
| [#95](https://github.com/ardamayk/test-project-with-ai/pull/95) | `d7b6918` | global | all four jobs | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33652272963) | [success](https://github.com/ardamayk/test-project-with-ai/actions/runs/33652273371) | 14:15 | 10:28 | 41.4 | 12.9 |

This is five revisions across three pull requests, not the required ten.
Documentation-only and global/mixed workflow changes are represented. There
are no isolated workspace, Music Server, Desktop Client, contract, or
unknown-path revisions. The sample therefore cannot establish classifier
accuracy across the required change classes.

## Latency, queue, and runner use

| Metric | p50 | p95 | Target | Result |
| --- | ---: | ---: | ---: | --- |
| Proposed-gate green, successful revisions | 5:18 | 14:15 | below 10:00 | fail |
| Existing-required green, all revisions | 8:44 | 11:37 | comparison only | worse than recorded baseline |
| First failure | 3:28 | 3:28 | below 3:00 | fail; one observation |
| Runnable-job queue | 0:02 | 0:04 | no explicit threshold | comparable to baseline |

One Workspace checks job queued for 5:03 in the final revision. It is above
p95 because it is one outlier among 64 runnable jobs, but it materially delayed
that Fast Gate. Queue max must continue to be reported alongside percentiles.

The five revisions consumed an estimated 106.2 shadow runner minutes and 56.1
old-CI runner minutes. Dual-running therefore used about 162.3 runner minutes.
The shadow workflows alone used about 89% more runner time than old CI in this
sample, largely because four of five revisions were conservatively classified
as global and ran all Desktop and integration work.

The 30-run baseline recorded in [#72](https://github.com/ardamayk/test-project-with-ai/issues/72)
had a 5:42 median workflow duration, a typical 5:16–7:25 range, a 5:23 primary
job median, a 2:30 E2E median, seven failures in 30 runs, and usual queue time
of zero to four seconds. The shadow median green time is 24 seconds faster than
the baseline workflow median, but the observed 14:15 p95/worst case and 3:28
first failure miss the cutover thresholds. The small, globally biased sample
does not support a stronger performance comparison.

No authoritative warm non-Desktop local Fast Gate measurement was retained in
the sampled run summaries. The below-60-second local criterion remains
unverified.

## Cache observations

| Cache | Hits | Attempts | Hit rate |
| --- | ---: | ---: | ---: |
| pnpm store | 0 | 29 | 0% |
| Go modules/build | 0 | 13 | 0% |
| golangci-lint | 0 | 5 | 0% |
| Cargo | 0 | 12 | 0% |
| Turbo | 0 | 5 | 0% |
| Playwright browser | 4 | 8 | 50% |
| Verified pinned mpv | 1 | 4 | 25% |

The first two revisions predated a trusted cache population. Later revisions
showed Playwright hits, one mpv hit, and continued misses for pnpm, Go,
golangci-lint, Cargo, and Turbo. The final revision intentionally changed the
mpv cache schema and rebuilt it. Cache behavior is not mature enough for
cutover, and the 5:03 queue plus fresh native builds contributed to the final
14:15 gate time.

## Gate and classifier behavior

- Every sampled revision produced both aggregator jobs, but the Fast status
  context was `Fast Gate`, not the specified `PR / Fast Gate`. The Integration
  context was the expected `PR / Integration Gate`. Ruleset cutover to the
  specified names would leave the Fast requirement unsatisfied.
- The documentation-only revision correctly selected no integration jobs and
  returned a successful Integration Gate in 28 seconds.
- Every global or mixed-global revision selected Web E2E, HLS, Desktop unit,
  and real pinned-mpv. These were conservative extra-test cases, not skips.
- No required integration was observed skipped in the represented revisions.
  Isolated workspace, server, Desktop, contract, and unknown-path behavior was
  not observed, so absence of a false negative is not established.
- The `9426c97` revision failed after its real-mpv test command's test cases all
  passed: `tee` could not write the expected log because the log directory did
  not exist on an mpv cache hit. The Integration Gate correctly propagated the
  job failure. The next revision created the directory before cache handling
  and passed.
- That failure was accidental, not an intentional failure probe. Explicit
  intentional-failure propagation remains unverified.

## Cancellation, diagnostics, and secrets

- No superseded pull-request run was cancelled; all sampled revisions finished
  before their successor started. Both workflows configure PR-scoped
  `cancel-in-progress`, but runtime behavior remains unverified.
- Two overlapping main Integration workflow runs
  ([first](https://github.com/ardamayk/test-project-with-ai/actions/runs/33626081448),
  [second](https://github.com/ardamayk/test-project-with-ai/actions/runs/33628170174))
  both completed, which confirms that pull-request cancellation did not cancel
  those main runs. No scheduled-run cancellation case was observed.
- The failed real-mpv run had zero artifacts. Upload reported no files because
  the missing log directory caused both the failure and the absent diagnostic.
  The gate summary identified the failed job, but the expected retained native
  log was unavailable; failure artifacts were therefore not usable.
- Manual review found GitHub token values masked as `***` and no exposed secret
  in sampled summaries/logs. This is evidence for sampled logs only, not a
  general secret-safety proof.

## Cutover decision

**NO-GO.** Keep `e2e` and `lint-test` required. Do not require the shadow gate
contexts yet.

Before reevaluation:

1. Collect at least five more revisions and cover isolated workspace, Music
   Server, Desktop Client, contract, and unknown-path changes where available.
2. Rename the Fast aggregator context to exactly `PR / Fast Gate` and confirm
   both stable names on every new revision.
3. Bring proposed-gate green p95 below ten minutes and first-failure p95 below
   three minutes; retain a warm non-Desktop local measurement below 60 seconds.
4. Populate and demonstrate useful pnpm, Go, golangci-lint, Cargo, and Turbo
   cache hit rates on trusted-key-compatible revisions.
5. Exercise an intentional job failure and confirm gate propagation plus a
   non-empty, useful, secret-free diagnostic artifact.
6. Exercise superseded-PR cancellation and a scheduled run that is not subject
   to PR cancellation.

Reevaluate all criteria using at least ten representative revisions. A new
decision must use the full sample, not combine this premature p95 with a later
partial sample.

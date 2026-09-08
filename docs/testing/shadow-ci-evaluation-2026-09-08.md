# Shadow CI cutover evaluation — 2026-09-08
Follow-up: [targeted fixes and controlled validation](shadow-ci-validation-2026-09-08.md) resolve several blockers below. This report preserves the original 20-revision measurements; do not mix them with the later controlled probes.

Decision: **NO-GO**. Continue evaluating the shadow gates before a ruleset cutover. This is a fresh evaluation for [issue #84](https://github.com/ardamayk/test-project-with-ai/issues/84), superseding neither historical facts nor the withdrawal of PR #106. It includes 20 revisions from 18 PRs and 60 workflow runs, rather than reusing the withdrawn five-revision sample.

The comparison baseline is the legacy **`e2e` + `lint-test` pair**, as explicitly requested by the repository owner. The live ruleset snapshot currently requires only `e2e`; that difference does not change which checks this report measures. No ruleset changes were made.

## Decision criteria

| Criterion | Evidence | Verdict |
| --- | --- | --- |
| At least ten representative revisions | 20 revisions; workspace, server, desktop, contract, global, mixed, docs, and unknown-path coverage | Pass, with isolated-category limitations below |
| First-failure p95 <3:00 | Shadow first failing step p95 **4:09**, n=2 failed revisions | Fail; small failure sample |
| Both proposed gates green p95 <10:00 | **6:14**, n=18 successful revisions | Pass |
| Warm non-desktop local Fast Gate <60s | Warmup failed on three existing Go `govet` shadow findings | Unverified |
| No required integration skipped | All 20 classifier replays matched recorded selections; every selected integration job ran | Pass for sampled changes |
| Stable gate names | `Fast Gate` and `PR / Integration Gate` present on all 20 revisions | Fast name corrected locally; new name not yet observed in CI |
| Intentional failures propagate | 14 local probes of actual gate shell blocks passed; two real CI failures propagated | Local verification plus observed real failures; intentional hosted failure not demonstrated |
| Superseded PR cancellation | Configuration enables it; no cancellation in the fresh sample | Unverified at the required process boundary |

## Method and sample

Selection: latest available PR revision for each branch having shadow runs from September 3 onward, plus the earlier failed revisions of PRs #133 and #136. Within each SHA, use the latest available run for each of the three workflow paths. This avoids counting duplicate CI runs as independent revisions. The sample is deliberately coverage-oriented, not a random estimate of long-term CI performance; two failures are explicitly oversampled.

All times are seconds from the relevant workflow's `created_at`. Required-green ends when both `e2e` and `lint-test` succeed; proposed-green ends when both shadow aggregators succeed. A failed revision has no green time and is excluded from green percentiles, not treated as a fast success. First-failure ends at the first failed step's completion, before later diagnostic upload and aggregation. Percentiles use nearest rank (`ceil(n*p)`), so p95 of 18 observations is their maximum. Some duplicate legacy runs were triggered later than the shadow pair; these are workflow-relative timings, not developer push-to-feedback measurements.

Runner minutes sum the durations of non-skipped jobs, including setup and cleanup. They are occupied-runner estimates, not billed minutes. Queue time means workflow creation to first job start; it does not claim to separate downstream scheduler wait from dependency wait. Cache denominators are restore observations emitted in job-summary environment values, not PR counts or Turbo task-level cache hits.

The adjacent JSON evidence file records all run IDs, source URLs, job outcomes, cache observations, classifier replay bases, metrics, and local probe results. Historical classifier flags came from downloaded run logs; current classifier replay used `node scripts/classify-pr-changes.mjs --base <baseSha> --head <sha>` on each real revision. All four integration flags and `integration_required` matched. The replay uses PR API base SHAs as fetched after merge, rather than claiming these are archived event payloads.

| PR | Revision | Classifier reasons | Legacy pair green | Shadow pair green | Runs: legacy / fast / integration |
| --- | --- | --- | --- | --- | --- |
| #118 | `8b0278a3` | contract, desktop, server, web | 5:54 | 4:00 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33752122833) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33752122888) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33752122876) |
| #119 | `6f1ce31b` | contract, documentation, server, web | 7:16 | 3:01 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33769754892) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33769754943) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33769754864) |
| #120 | `98074bac` | server | 6:13 | 1:54 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33781663586) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33781663614) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33781663528) |
| #121 | `89f81842` | contract, documentation, server, web | 7:30 | 2:09 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33786598885) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33786598807) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33786598809) |
| #122 | `141d59b8` | contract, documentation, server, web | 6:04 | 3:07 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33789186361) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33789186316) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33789186366) |
| #123 | `621d10ac` | contract, documentation, server, web | 6:11 | 2:50 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33798881560) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33798881559) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33798881628) |
| #124 | `f89e9df4` | documentation, server, web | 4:57 | 2:22 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33803753642) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33803754464) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33803754607) |
| #125 | `466d4e4d` | documentation | 6:34 | 3:17 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33804505762) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33804505815) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33804505783) |
| #126 | `136c6390` | desktop, global, server, web | 6:55 | 5:20 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33807452651) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33807452605) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33807452847) |
| #127 | `7b9a7f27` | documentation, server | 6:33 | 2:14 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33810125403) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33810125409) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33810125426) |
| #129 | `5ddb9999` | documentation, web | 6:12 | 2:09 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33813303897) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33813303824) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33813303882) |
| #130 | `5b2830f4` | contract, desktop, documentation, server, web | 8:03 | 3:49 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33911678582) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33911678631) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33911678592) |
| #132 | `69dd9300` | contract, documentation, global, server, web | 8:16 | 5:03 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/33916058630) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/33916058635) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/33916058555) |
| #133 | `0f5a9aee` | contract, desktop, documentation, server, web | 7:15 | failed | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028342470) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028330616) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028330529) |
| #133 | `c753f90d` | contract, desktop, documentation, server, web | 6:00 | 3:28 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028707916) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028690993) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028691013) |
| #134 | `f6c5a3c1` | contract, desktop, documentation, server, unknown, web | 8:50 | 6:09 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34101967128) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34101967168) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34101967174) |
| #135 | `69caf7a1` | contract, desktop, documentation, hls, server, web | 8:53 | 6:14 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34165955021) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34165896365) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34165896387) |
| #136 | `f10f7110` | documentation, web | failed | failed | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34166759985) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34166759957) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34166759950) |
| #136 | `ab6fe70a` | documentation, web | 8:16 | 2:20 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34168052194) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34168028058) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34168028050) |
| #137 | `5c124f90` | contract, global, server, web | 8:22 | 5:44 | [CI](https://github.com/ardamayk/test-project-with-ai/actions/runs/34254286398) / [F](https://github.com/ardamayk/test-project-with-ai/actions/runs/34254286425) / [I](https://github.com/ardamayk/test-project-with-ai/actions/runs/34254286446) |

## Timing and runner use

| Metric | Legacy CI | Fast Gate | Integration Gate | Both shadow gates |
| --- | --- | --- | --- | --- |
| Green p50 | 6:55 (n=19) | 2:50 (n=20) | 3:00 (n=18) | 3:07 (n=18) |
| Green p95 | 8:53 | 6:09 | 5:07 | 6:14 |
| First-failure p50 | 2:46 (n=1) | No failures | 2:28 (n=2) | 2:28 (n=2) |
| First-failure p95 | 2:46 | Not estimable | 4:09 | 4:09 |
| Initial queue p50 / p95 | 3s / 3s | 3s / 10s | 3s / 5s | Not aggregated |
| Occupied runner minutes, total over 20 revisions | 187.27 | 108.65 | 127.10 | 235.75 |

Shadow gates use about 25.9% more occupied runner time than legacy CI in this sample, while returning successful results earlier through parallel execution. Running old and shadow CI together consumed about 423.02 runner minutes. Parallelism improves elapsed feedback but is not a cost reduction here.

The [#72 baseline](https://github.com/ardamayk/test-project-with-ai/issues/72) records 30 runs with median total workflow duration 5:42, a typical range 5:16–7:25, primary-job median 5:23, E2E median 2:30, and 7 failures (5 at late Go lint). Our shadow-pair median is 3:07 and current legacy-pair median 6:55. Workload and endpoint differences prevent a causal speedup claim against that earlier baseline. #72 does not publish raw first-failure/queue/cache/runner observations or exact p95 values, so those historical comparisons remain unavailable.

## Cache restore results

| Cache | Exact hits | Partial hits | Misses |
| --- | --- | --- | --- |
| PNPM | 93/107 (86.9%) | 0 | 14 |
| GO | 51/57 (89.5%) | 0 | 6 |
| PLAYWRIGHT | 33/35 (94.3%) | 0 | 2 |
| CARGO | 29/39 (74.4%) | 0 | 10 |
| MPV | 13/13 (100.0%) | 0 | 0 |
| GOLANGCI | 18/20 (90.0%) | 0 | 2 |
| TURBO | 18/20 (90.0%) | 1 | 1 |

Turbo restored a usable exact or partial archive in 19/20 observations (95%); exact hits were 90%. Restoring a Turbo archive is not proof that every task inside it was a cache hit. Cargo's 29/39 exact hits (74.4%) are a remaining setup/compile cost. Unlike the withdrawn early report, these fresh observations show caches are being restored successfully.

## Classifier coverage and skip behavior

- Workspace-only code, with docs: #129 and #136 select Web E2E and skip HLS, Desktop unit, and real mpv.
- Music Server only: #120 selects Web E2E and HLS; Desktop jobs are skipped. #127 adds documentation with the same selection.
- Documentation only: #125 has `integration_required=false`, all four integration jobs skipped, and a successful `PR / Integration Gate`. Workspace and Music Server fast checks still run by the documented conservative policy.
- Desktop changes: #118, #126, #130, #133, #134, and #135 exercise Desktop unit and real mpv in mixed revisions. No isolated desktop-only PR is present in this fresh interval.
- Contract and global changes select all integrations; examples #119/#123 and #126/#137. These are mixed PRs, not isolated contract-only or global-only experiments.
- Unknown path: #134 changes `.gitignore`; the recorded reasons include `unknown` and all integrations ran. Contract/desktop changes in the same revision also select that matrix, so the independent unknown-only behavior is supported by classifier fixture tests rather than isolated hosted evidence.
- Extra tests are intentional: contract/global/unknown changes select native checks even without native source changes, and docs still run workspace/server fast checks. No mismatched selection or skipped selected integration appeared in the 20-revision audit. This does not prove the policy covers every possible future path.

## Gate names, failures, and diagnostics

Every sampled revision contains actual check names `Fast Gate` and `PR / Integration Gate`. The issue already expects `PR / Fast Gate`; the workflow job name is now changed locally to match. Historical runs retain their original name. Verify the new name in the next hosted PR before configuring a required context.

The real failures demonstrate propagation through the integration aggregator:

- [PR #133 failed integration run](https://github.com/ardamayk/test-project-with-ai/actions/runs/34028330529): the Real pinned mpv job failed in Managed Import parity, expecting `possible_duplicate` and receiving `none`; the mpv tests themselves passed. First failure: 4:09. The legacy CI was green, showing the new integration matrix caught coverage the old workflow lacked. `integration-gate-real-mpv-logs` contains both `desktop-import-parity.log` and `desktop-mpv.log`.
- [PR #136 failed integration run](https://github.com/ardamayk/test-project-with-ai/actions/runs/34166759950): Web E2E failed and retried once, then the Integration Gate failed. First failure: 2:28. `integration-gate-web-e2e-diagnostics` includes the HTML report, screenshots, error context, trace archives, and retry evidence.

Both artifacts downloaded successfully; archive and nested-trace integrity checks passed. Native logs identify the failing assertion and browser diagnostics contain the expected report/trace/screenshot assets. A bounded content scan found no private-key, GitHub-token, AWS access-key, Authorization bearer/basic, or JSON password-field patterns. This is not a guarantee that all possible secrets, images, or encoded values are absent. Browser artifacts expire after 7 days and native logs after 14 days; links will eventually lose their downloads.

Job-summary publishing steps succeeded and logs contain classifier decisions, cache outcomes, timings, and diagnostic references. This audit checked source/log content, not the rendered GitHub summary UI.

The actual Evaluate gate result shell blocks passed 14 local cases: all-success, each of five upstream results failing, and valid no-conditional-work for each gate. No intentionally failing hosted PR was created. The aggregators currently accept `skipped` upstream jobs without cross-checking selection flags; no such invalid skip occurred in this sample, but the selected-job audit is necessary and should remain part of cutover validation.

## Cancellation and scheduled runs

Both shadow workflows use PR-number concurrency groups and `cancel-in-progress: true`. Main, Nightly, and Clean Room use non-PR groups with `cancel-in-progress: false`. In the fresh interval, 18 Main runs and 7 scheduled runs were observed; none was cancelled (one Clean Room run failed).

There was no cancelled shadow run in the fresh sample. A supplementary September 2 [older integration run](https://github.com/ardamayk/test-project-with-ai/actions/runs/33658858454) is marked cancelled after a newer revision began, but the fetched job list shows successful jobs continuing until 17:31, after the replacement began at 17:14. That ambiguous record is insufficient proof of prompt supersession cancellation and is excluded from the timing sample. Verify overlapping hosted PR revisions for both gates before cutover; configuration alone is not that verification.

## Local validation and unresolved work

Local revision: `80d5d56903579a671f3cdeb7a09abda336acbec0`. The non-desktop equivalent of the local Fast Gate was invoked through public Mise tasks:

```sh
mise run workspace:check ::: workspace:typecheck ::: workspace:test ::: server:check ::: server:test ::: git-hooks:test ::: classifier:test ::: main-workflows:test ::: clean-room:test
```

Warmup stopped after 1.447s because `server:lint` reported existing `govet` shadow findings in `server/internal/modules/playback/store.go` at lines 68, 108, and 146. That failure cancelled sibling tasks. This is not a successful fast timing and no warm-success result is claimed. The public `ci:fast` includes Desktop checks, so the explicit non-desktop task composition is recorded for reproducibility. Earlier command-assembly troubleshooting is excluded from the benchmark.

After the workflow-name edit, 37 classifier/workflow-policy tests passed, and all 14 direct gate shell probes passed. No production code or retry policy was changed.

Before a GO recommendation:

1. Bring first-failure p95 below 3:00 and gather more representative failed revisions; two failures are a small sample.
2. Obtain a successful warm non-desktop local run under 60s after the existing lint failures are resolved.
3. Confirm `PR / Fast Gate` appears on a hosted revision after this name change lands.
4. Demonstrate supersession cancellation for both PR workflows and intentional hosted failure propagation; retain diagnostic evidence before expiration.
5. Keep isolated Desktop/contract/global/unknown coverage gaps and the bounded artifact/summary inspection in view during the next evaluation.

This evaluation is complete as a NO-GO report. It does not authorize or perform a ruleset cutover, and remaining verification gaps should not be marked successful.

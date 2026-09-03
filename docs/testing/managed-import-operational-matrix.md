# Managed Import operational recovery matrix

This matrix is the final operational verification for the Managed Import
release (issue #56, parent #21). Every row is an automated Go test in
`server/internal/modules/managedimport` that drives the versioned HTTP seam or
the in-package fault-injection seams (`newModule` capacity, `commitPhaseHook`,
`replacementPhaseHook`, `migrationPhaseHook`) and asserts only externally
observable outcomes: response codes, library reads, Import History, and the
exact contents of Managed Storage. The whole matrix runs inside the normal
`mise run server:test` task and the Fast Gate `server` job; no extra CI wiring
is needed.

The tests added for this issue live in `operational_matrix_integration_test.go`
(HTTP seam) and `operational_matrix_test.go` (fault-injection seams). Earlier
slices already covered most rows; they are listed here so the matrix can be
audited from one place.

## Storage exhaustion

| Condition | Test |
| --- | --- |
| Reserve would be exhausted before preview | `TestManagedImportRejectsUploadWhenStorageReserveWouldBeExhausted` |
| Migration preview refuses accepted files that do not fit | `TestLibraryMigrationPreviewRejectsAcceptedFilesWhenCapacityIsInsufficient` |
| Free space drops between preview and commit (selected plus artwork bytes) | `TestManagedImportRechecksSelectedAndTemporaryBytesBeforeCommit` |
| Batch confirmation reports the per-file `insufficient_storage` code | `TestManagedImportBatchPreservesConfirmationFailureCode` |
| Exact Duplicate is classified before the commit capacity check | `TestManagedImportClassifiesExactDuplicateBeforeCommitCapacity` |
| Track Replacement rechecks capacity for the temporary second copy and keeps the old Track streamable | `TestTrackReplacementRechecksCapacityBeforeCommitAndKeepsOldTrack` |
| Migration cutover activates verified copies within tight capacity | `TestLibraryMigrationCutoverActivatesVerifiedCopiesWithinTightCapacity` |

## Concurrency and stale state

| Condition | Test |
| --- | --- |
| Concurrent confirmation of one preview commits once | `TestManagedImportSerializesConcurrentConfirmationOfOnePreview` |
| Concurrent exact-byte imports: first commit wins, later report `exact_duplicate` | `TestManagedImportConcurrentExactByteImportsReturnDeterministicDuplicate` |
| Concurrent uploads reserve batch bytes atomically | `TestManagedImportBatchReservesConcurrentUploadBytesAtomically` |
| Concurrent batch confirmation is serialized | `TestManagedImportBatchSerializesConcurrentConfirmation` |
| Concurrent commits for one Album are serialized | `TestManagedImportSerializesConcurrentCommitsForOneAlbum` |
| Artwork publication never replaces a concurrent winner | `TestManagedImportPublishesArtworkWithoutReplacingConcurrentWinner` |
| Stale or future revision on a standalone confirm mutates nothing; current revision still commits | `TestManagedImportRejectsStaleRevisionWithoutMutation` |
| Stale revision on a batch confirm mutates nothing | `TestManagedImportBatchRejectsStaleRevisionWithoutMutation` |
| Stale deletion preview mutates nothing | `TestManagedTrackDeletionRejectsStalePreviewWithoutMutation` |
| Stale replacement token is refused | `TestTrackReplacementPreservesIdentityAndReferences` |
| Exact Duplicate and occupied Album position on replacement | `TestTrackReplacementRejectsExactDuplicateAndOccupiedPosition` |
| Same-edition position collisions become Possible Duplicates that need an explicit decision; different editions are not blocked | `TestManagedImportClassifiesOnlySameEditionPositionAsPossibleDuplicate`, `TestManagedImportDoesNotClassifyDifferentEditionAsDuplicate`, `TestManagedImportRechecksPossibleDuplicateAtConfirmation`, `TestLibraryMigrationPreviewRejectsDuplicateAlbumPositionsWithinMigration` |
| Two replacement jobs for one Track: exactly one commits, the other reports `replacement_preview_changed` | `TestTrackReplacementConcurrentJobsForOneTrackAreSerialized` |
| Track Replacement racing Permanent Track Deletion: exactly one wins, no pending journals remain | `TestTrackReplacementAndPermanentDeletionRaceIsDeterministic` |
| Concurrent migration analysis is refused | `TestLibraryMigrationPreviewRejectsConcurrentAnalysis` |

## Corruption, image bombs, path attacks, upload limits

| Condition | Test |
| --- | --- |
| Corrupt stream head, corrupt stream middle, truncated stream end, and an image bomb front cover are rejected with no staging, canonical, or library residue | `TestManagedImportRejectsCorruptStreamsWithoutResidue` |
| Invalid FLAC leaves the library untouched | `TestManagedImportRejectsInvalidFLACWithoutLibraryMutation` |
| Traversal, absolute, backslash, nested, NUL-byte, dot, and empty filenames are rejected with no residue | `TestManagedImportRejectsPathAttackFilenamesWithoutResidue` |
| Symlinked staging, root, ancestor, and canonical library directories are refused | `TestManagedImportRejectsStagingSymlinkEscape`, `TestManagedImportRejectsManagedStorageRootSymlink`, `TestManagedImportRejectsManagedStorageAncestorSymlink`, `TestManagedImportRejectsCanonicalLibrarySymlinkEscape` |
| Declared, undeclared, and false Content-Length violations of the file and batch limits leave no residue | `TestManagedImportUploadLimitViolationsLeaveNoResidue` |
| Limit enforcement while streaming | `TestManagedImportEnforcesFileLimitWithoutContentLength`, `TestManagedImportEnforcesBatchLimitWhileStreaming` |
| Cancelling an uncommitted standalone job removes its staging and records a canceled history item | `TestManagedImportStandaloneCancellationRemovesStagingAndRecordsHistory` |
| Explicit batch cancellation and single-file cancellation remove only uncommitted staging | `TestManagedImportBatchCancellationRemovesUncommittedStaging`, `TestManagedImportJobCancellationRemovesOneBatchFile` |
| Interrupted upload keeps sibling staging and stays retryable | `TestManagedImportInterruptedUploadPreservesSiblingStaging`, `TestManagedImportBatchRetriesInterruptedUploadInSameJob` |
| Inactivity expiry cleans stalled uploads, failed standalone jobs, and only expired batches | `TestInactiveCleanupCancelsStalledUploadAfterFifteenMinutes`, `TestCleanupInactiveExpiresFailedStandaloneJob`, `TestCleanupInactiveRemovesOnlyExpiredImportBatch` |
| Restart removes orphan staging | `TestCleanupRestartRemovesOrphanStaging` |
| Import History keeps the latest 20 terminal results without staged bytes | `TestImportHistoryReportsCancellationAndPrunesOldestResult`, `TestImportHistoryReportsPartialBatchWithoutStagingOrRawMetadata` |

## Restart recovery

| Condition | Test |
| --- | --- |
| Commit journal crash at every durable phase, recovered twice | `TestRecoverCommitAtEveryDurablePhase` |
| Placement gap, corrupt pending canonical Track, preexisting artwork | `TestRecoverPreparedJournalAfterPlacementGapRemovesCanonicalOrphans`, `TestRecoverCorruptPendingCanonicalTrackRollsBackWithReason`, `TestRecoverPreparedJournalPreservesPreexistingCanonicalArtwork` |
| Restart cleanup completes a pending database commit without deleting content | `TestCleanupRestartCompletesPendingDatabaseCommitWithoutDeletingContent` |
| The real `Module.Start` chain, run twice, recovers a commit journal crashed at every durable phase and leaves no visible pending state | `TestModuleStartRecoversPendingCommitJournalInOneRestartPass` |
| Track Replacement crash at every phase, including after the database commit | `TestTrackReplacementRecoversFromCrashAtEveryPhase`, `TestTrackReplacementCompletesAfterCrashFollowingDatabaseCommit` |
| Permanent Track Deletion recovery with and without the file already removed | `TestManagedTrackDeletionRecoveryCompletesPreparedDeletion` |
| Library Migration crash at every durable phase | `TestLibraryMigrationRecoversFromCrashAtEveryDurablePhase` |
| Migration restart reconciles prepared, promoted, and orphaned copies | `TestCleanupRestartReconcilesPreparedMigrationCopy`, `TestCleanupRestartRestoresPromotedCopyInterruptedMidPromotion`, `TestCleanupRestartSweepsOrphanedMigrationFiles`, `TestCleanupRestartFailsVerifiedMigrationCopyWhenPendingAudioDisappeared` |
| Repeated migration cutover is idempotent | `TestLibraryMigrationCutoverIsIdempotent` |
| Migration contract lists every phase | `TestLibraryMigrationContractCoversEveryPhase` |
| Startup scanning stays retired while deprecated scan routes remain compatible for older clients | `TestAssembledServerKeepsDeprecatedLegacyScanBehaviorForOlderClients` (`server/cmd/server`), `TestDeprecatedScanRoutesStayCompatible` (`server/internal/modules/library`) |

## Permanent deletion and legacy cleanup confinement

| Condition | Test |
| --- | --- |
| Deletion refuses outside, symlinked, broad, unresolved, traversal, and NUL-byte targets | `TestManagedTrackDeletionRejectsUnsafeSources` |
| Deletion requires the explicit application confirmation header | `TestManagedTrackDeletionRequiresExplicitApplicationConfirmation` |
| Legacy cleanup refuses sources outside the configured music paths | `TestLegacySourceCleanupRefusesSourcesOutsideMusicPaths` |
| Legacy cleanup refuses the whole selection when one source changed | `TestLegacySourceCleanupRefusesWholeSelectionWhenOneSourceChanged` |
| Legacy cleanup requires an explicit confirmation | `TestLegacySourceCleanupRequiresExplicitConfirmation` |

## Defect found by the matrix

Cancelling a standalone Import Job that was awaiting confirmation failed with
`internal_error` because the history archive cleared the staged path while the
row still carried the `awaiting_confirmation` status, violating the table CHECK
constraint. The archive now clears payloads only for committed and failed
rows; the canceled row is deleted in the same transaction. The regression test is
`TestManagedImportStandaloneCancellationRemovesStagingAndRecordsHistory`.

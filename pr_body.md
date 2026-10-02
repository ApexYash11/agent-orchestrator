## Summary
Fixed #6104: Worker not notified of recurring merge conflicts when the board shows them.

After the first conflict nudge, a clean-but-blocked poll (mergeable=MERGEABLE + mergeStateStatus=BLOCKED) never re-arms the nudge because the durable last_nudge_signature stays "conflicting". Added a provider-neutral ConflictsCleared flag that carries the already-known cleared-conflict fact out of band; the re-arm gate is now mergeabilityClearsConflict || o.ConflictsCleared, so the recurring conflict is no longer silently swallowed.

## Problem
When a PR is detected as conflicting, the worker receives a nudge to rebase. After rebasing, if the PR becomes mergeable but blocked by required review (mergeStateStatus=BLOCKED), the base branch may advance and reintroduce conflicts. The daemon's reaction system suppressed the second nudge because the pr.last_nudge_signature still held {"seen":{"merge-conflict:<url>":"conflicting"}} from the first observation, and the re-arm logic only considered mergeability changes, not the provider's explicit cleared-conflict fact.

## Fix
- Added SCMMergeabilityObservation.ConflictsCleared boolean indicating when the provider rules conflicts out, even if derived state is blocked.
- GitHub adapter sets it when mergeable == "MERGEABLE" after the conflict early-return.
- GitLab adapter sets it for mergeable/can_be_merged (CI/review/draft-blocked variants).
- Observer projection preserves the flag across persisted-state override.
- ports/pr_observations.go mirrors the flag on the legacy DTO.
- lifecycle/reactions.go updates the re-arm gate to mergeabilityClearsConflict(o.Mergeability) || o.ConflictsCleared.

Files changed:
- backend/internal/ports/scm_observations.go
- backend/internal/ports/pr_observations.go
- backend/internal/lifecycle/reactions.go
- backend/internal/adapters/scm/github/observer_provider.go
- backend/internal/adapters/scm/gitlab/observer_provider.go
- backend/internal/observe/scm/observer.go

## Validation
- New regression tests pass:
  - TestSCMObservation_MergeConflictReArmsAfterBlockedWithClearedConflicts
  - TestWiring_MergeConflictNudgeReArmsAfterBlockedWithClearedConflicts (end-to-end, real SQLite)
  - TestPRObservation_UnknownMergeabilityDoesNotReArm (bare blocked still does not re-arm — preserves #4528 dedup)
- Provider normalizer tests for GitHub + GitLab.
- Manual verification: live session o6104-1 (project o6104) shows BEFORE/AFTER proof in its chat.
- go build ./... clean; go vet clean on changed packages and consumers.
- Existing re-arm tests remain green.

## Not in this PR
- No changes to SQLite schema or migrations; no sqlc regeneration needed.
- No changes to OpenAPI spec or frontend types; the new flag is internal-only.
- No changes to the GitHub adapter's handling of UNKNOWN mergeability (still does not re-arm).
- No changes to the GitLab adapter's treatment of 
eed_rebase/checking states.

Closes #6104

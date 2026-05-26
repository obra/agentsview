# Session Quality Validation And Rollout

Phase 6 validates the deterministic scoring and Insights stack before broad
rollout. Final calibration is intentionally gated on Phases 2-5 because the
signal names, API fields, deterministic Insights views, and opt-in LLM insight
flow must be stable before weights can be judged against real archives.

## Rollout Gate

Do not promote the new prompt, context, or workflow penalties as final default
semantics until all of these are true:

- Phase 2 scorer contracts are frozen, including signal names and penalty caps.
- Phase 3 persistence/API fields are additive, nullable/backfilled, and covered
  by resync preservation tests.
- Phase 4 deterministic Insights views expose scored facts before generated
  recommendation text.
- Phase 5 LLM canned insights remain opt-in, cacheable, and separate from
  canonical health score writes.
- A local archive calibration pass has reviewed score deltas, threshold
  crossings, and known false positives.

## Validation Matrix

| Area | Evidence | Current scaffold |
| --- | --- | --- |
| Score deltas | Compare canonical score against a baseline with Coach-derived heuristics removed. Lock expected score, grade, basis, penalties, and delta. | `internal/signals/testdata/score_golden.json` and `TestComputeHealthScore_GoldenDeltas` |
| Explanation fields | Every score delta has inspectable `Basis` and `Penalties` entries. | Golden score assertions |
| Archive preservation | Full resync preserves source-missing/orphaned sessions and their computed quality signal columns. | `TestResyncAllPreservesTrashedSessionData` orphan signal assertion |
| Saved insights | Full resync preserves saved insight rows. | `TestResyncAllPreservesInsights` |
| Deterministic Insights UI | Saved quality recommendations render provenance metadata and deterministic-score disclaimer. | `frontend/e2e/insights-quality.spec.ts` |
| LLM-disabled state | Read-only or unavailable generation disables the Generate action and avoids background mutation. | `frontend/e2e/insights-quality.spec.ts` |

## Calibration Report Template

Run this after Phases 2-5 are finalized against a representative local archive.
Keep the report local unless the data has been scrubbed.

| Metric | Value | Notes |
| --- | --- | --- |
| Archive date range | TBD | Include machines/agents sampled. |
| Sessions scored | TBD | Exclude unscored low-confidence unknown sessions. |
| Median score delta | TBD | New score minus baseline without Coach-derived heuristics. |
| p90 absolute delta | TBD | Review high-delta sessions manually. |
| Newly below A/B/C thresholds | TBD | Count threshold crossings by grade. |
| Top penalty contributors | TBD | Rank by frequency and total points. |
| False-positive themes | TBD | Include examples such as terse expert prompts or planning-only code discussions. |
| Runtime impact | TBD | Compare resync/backfill duration before and after signal computation. |
| Rollout decision | TBD | Default-on, preview-only, or shadow-only. |

## Operator Notes

- New quality signal columns are additive; existing archives must not be deleted
  or recreated to adopt them.
- Full resync can recompute parser-derived signals and should preserve orphaned,
  trashed, excluded, and saved insight data before swapping databases.
- If score semantics shift materially, expose the new penalties through a
  preview or shadow path first and document the delta in release notes.
- LLM canned insights summarize deterministic aggregates only. They must not
  write canonical score, grade, signal, or penalty rows.

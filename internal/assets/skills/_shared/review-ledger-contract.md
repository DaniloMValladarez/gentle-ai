# Bounded Review Execution and Ledger Contract

This is the canonical reusable contract for orchestrators, 4R lens reviewers, refuters, scoped validators, and Judgment Day. Generated adapter prompts expand this file; role-specific prompts add only their lens or tool boundary.

## Operation Boundary

Review is explicit `review/start(target)`. The operation receives one complete immutable snapshot, is detached, read-only, and terminal after one result. A reviewer never edits code, launches a correction, starts another reviewer, or owns lifecycle routing. Return one result and terminate.

The parent orchestrator selects zero, one, or four initial lenses from deterministic risk classification. Each selected lens runs exactly one exhaustive sweep. Full 4R means four initial lens sweeps, not extra sweeps or three refuter tasks.

## Frozen Findings Ledger

Findings freeze after the initial selected-lens review. Each lens emits neutral structured claims and proof references rather than persuasive narrative:

| Field | Values |
|---|---|
| `id` | `{LENS}-{NNN}` |
| `lens` | `risk | readability | reliability | resilience | judgment-day | scoped-fix-validator` |
| `location` | `path/to/file.ext:line` or range |
| `severity` | `BLOCKER | CRITICAL | WARNING | SUGGESTION` |
| `claim` | Neutral statement of observable incorrect behavior |
| `evidence_class` | `deterministic | inferential | insufficient` |
| `proof_refs` | Concrete command, output hash, or `file:line` references |
| `status` | `open | corroborated | refuted | inconclusive | fixed | verified | info` |

Persist an explicit empty ledger when no findings exist. WARNING/SUGGESTION rows are `info`; they never drive correction or block approval.

## Evidence Routing

- Deterministic severe findings become `corroborated` with proof and never invoke a refuter.
- Inferential severe findings from every selected lens are merged into exactly ONE detached refuter operation for the transaction. The refuter receives the immutable target plus all neutral claims/proof references and returns one `corroborated | refuted | inconclusive` result per finding.
- Insufficient findings become `inconclusive` and are never auto-fixed.
- Missing, malformed, or incomplete refuter output is `inconclusive`, never implied corroboration.
- Judgment Day's two-judge agreement is its corroboration mechanism; it never launches `review-refuter`. Native state records two distinct blind execution hashes, two distinct result hashes, a confirmation/agreement hash, and exactly two judge executions before findings can freeze.

The refuter is read-only, cannot add findings or change scope, returns one complete result, and terminates. One candidate or twenty candidates consume the same single refuter-batch budget.

## Correction and Scoped Validation

Only the parent orchestrator may launch a correction actor or scoped validator, and only within native transaction counters.

Ordinary review permits at most one correction transaction composed of atomic work units. Each work unit maps exactly to frozen accepted/blocking IDs, changes only the immutable genesis path set, and records focused-test evidence, runtime evidence or justified `N/A`, and an independent rollback boundary. Work-unit count never creates another correction budget.

If correction occurred, ordinary review runs exactly one scoped fix-delta validator. It is detached and read-only, receives only the frozen ledger plus immutable fix delta, verifies original acceptance criteria/tests and correction regression evidence, and can return only `approve` or `escalate`. Later observations are non-blocking follow-ups: they do not alter findings, outcomes, scope, frozen IDs, counters, or correction. A failed original criterion escalates; it never reopens the original diff, launches another correction, or iterates.

Judgment Day replaces ordinary 4R for an explicitly selected target. It alone may run at most two fix rounds and two scoped re-judgments; it does not inherit or extend an ordinary budget.

## Independent Final Verification

Final verification is independent requirements/runtime verification. It checks actual requirements/scenarios, task completion, current test/build evidence, frozen-ledger resolution, snapshot identity, and counter coherence. A contradiction or newly failing deterministic check escalates; it cannot start another 4R, refuter, correction, or scoped-validation loop.

Only `approved | escalated` are terminal transaction states. `scope-changed | invalidated` are lifecycle validation outcomes requiring explicit action.

## Persistence and Lifecycle Gates

OpenSpec mode persists exact machine-readable artifacts:

- `openspec/changes/{change-name}/reviews/transaction.json`
- `openspec/changes/{change-name}/reviews/policy.md`
- `openspec/changes/{change-name}/reviews/ledger.json`
- `openspec/changes/{change-name}/reviews/receipt.json`
- `openspec/changes/{change-name}/reviews/chain-bundle.json`
- `openspec/changes/{change-name}/reviews/gate-context.json`

The sole authoritative append-only CAS state is `<git-common-dir>/gentle-ai/review-transactions/v1/{lineage-id}/`, derived from the canonical validated repository root and a canonical lowercase kebab-case lineage ID. Every Git subprocess uses explicit `git -C <canonical-repo>` and strips inherited repository/worktree/common-dir/index/object/alternate/namespace/shallow/graft/replacement/discovery overrides while preserving ordinary credentials and safe configuration. Linked worktrees retain shared common-dir behavior.

`transaction.json`, chain bundles, and every OpenSpec or Engram artifact are explicitly non-authoritative mirrors. Dispatchers and lifecycle gates load exact HEAD and validate every content-addressed predecessor to one legal `review/start` genesis plus semantic state invariants. Frozen severe findings cannot disappear or lose classification/outcome; pending refuter IDs require one consumed complete batch; corroborated IDs equal correction IDs; corrected candidates require scoped validation; ready/final/approved states have coherent mode counters and no pending severe work. WARNING/SUGGESTION rows remain non-blocking `info`. Missing, cyclic, reordered, regressive, hash-mismatched, semantically incomplete, or standalone terminal chains fail closed.

Writers use a non-blocking cross-platform OS lock with token/PID/host/timestamp observability and crash release. Exact retries repair a linked event or return an already-committed exact HEAD without changing budget; stale predecessors or different content remain rejected. Use `review-resume` after machine-output failure rather than rerunning review work.

Engram mode upserts the equivalent exact topics:

- `sdd/{change-name}/review/transaction`
- `sdd/{change-name}/review/policy`
- `sdd/{change-name}/review/ledger`
- `sdd/{change-name}/review/receipt`
- `sdd/{change-name}/review/chain-bundle`
- `sdd/{change-name}/review/gate-context`

Ad-hoc review uses `review/{target-slug}/{transaction|policy|ledger|receipt|chain-bundle|gate-context}`. If no artifact store exists, keep all artifacts inline for the current session and never claim durable receipt reuse.

Post-implementation/post-apply starts ordinary review only when no valid receipt exists. Pre-commit, pre-push, pre-PR, and SDD archive run one native command for the same content-bound receipt; they never hand-author request JSON, create a budget, or silently launch Judgment Day:

```bash
gentle-ai review-validate --cwd <repo> --lineage <id> --gate <post-apply|pre-commit|pre-push|pre-pr|release> --receipt <receipt.json> --bundle <chain-bundle.json> --policy <policy> --ledger <ledger.json> --evidence <verification> [gate-specific artifact flags]
```

Post-apply and pre-commit may add repeatable `--intended-untracked` inputs. Corrected lineages add `--fix-delta <snapshot-json>` whose strict `fix-diff` snapshot bytes reproduce the terminal correction identity; no-fix lineages omit it. Pre-PR derives the publication base from the push remote's advertised default-branch HEAD; `--base-ref` is only an optional expectation and cannot select the boundary. When that base advanced, add `--pre-pr-ci-attestation <signed-json>`. Release adds `--release-configuration`, `--release-generated`, `--release-provenance`, `--release-publication-boundary`, and `--release-evidence-freshness` artifacts. New external evidence uses `--external-evidence <json>`. `--request-out` may persist the canonically derived request for audit. Explicit `--request` remains a mutually exclusive compatibility mode, not the prescribed lifecycle path.

Gate context binds expected HEAD, genesis, ordered chain identity, and bundle digest. The validator derives the authoritative store root, verifies the named bundle is the exact authoritative chain, reads every persisted artifact preimage once, derives the current repository target, and binds policy, strict frozen ledger, fix delta, verify evidence, external evidence, and release semantics from those retained bytes; caller-authored store paths, transactions, trees, digests, path sets, favorable states, or chain assertions are never authoritative. Immediately before any allow it re-derives the repository target and publication refs. Missing, malformed, `scope-changed`, `invalidated`, and `escalated` results emit machine-readable denial JSON and return non-success.

At PRE-PR, the push remote's advertised default-branch HEAD is the publication boundary; a stale or unrelated caller-selected local ref is never sufficient proof. An advanced base can reuse the immutable receipt only when native Git derivation proves the original reviewed merge-base tree, unchanged delivered patch and path identities, disjoint old-base-to-new-base paths, and a conflict-free merged result. Moving refs are resolved once to immutable commits and rechecked before authorization. The exact merged-result tree requires successful CI attested by Ed25519: the issuer and public key come from `pre_pr_ci_trust` in the already receipt-bound JSON policy preimage, while `--pre-pr-ci-attestation` only names an artifact containing schema `gentle-ai.pre-pr-ci-attestation/v1`, issuer, merged tree, `success` status, and signature. The signature preimage is `schema NUL issuer NUL merged_tree NUL status NUL`. A success boolean, unsigned artifact, caller-selected key, missing trust root, overlap, conflict, ref movement, changed delivery, or any unprovable condition falls back to the existing fail-closed gate result. Successful reuse emits `base_advanced_compatible` context and never rewrites the receipt.

For clean-clone/workstation recovery, persist the full ordered content-addressed chain bundle. Explicit `review-bundle-import` MUST validate bundle digest, every event hash/predecessor/semantic transition, lineage/generation/mode, initial/final snapshot identities, terminal fix-diff semantics, and expected gate chain identity before CAS installation into the repository-derived destination. Recovery is delivered-content equivalence, not snapshot-structure equivalence: the current derived candidate and delivered path digest MUST match `final_candidate_tree` and the receipt/lineage path scope; the authoritative intended-untracked proof MUST reproduce from that candidate; and policy, ledger, evidence, fix delta, receipt, and chain bindings MUST match exactly. The terminal fix-diff kind, base, ledger IDs, and identity remain preserved for audit, but the recovery snapshot need not share its kind, base, or identity. `review-validate` never auto-imports. Tampered content, wrong path scope/artifacts, truncated or tampered chains, different lineages, arbitrary alternate stores, or unbound bundles remain untrusted and fail closed.

Release from protected `main` has a narrow fast path and does not require a reusable review receipt when all of these are proven: the tag target is the current immutable `origin/main` SHA, required CI for that exact SHA is successful, the remote has not advanced before tag push, and no new vulnerability, policy, provenance, signing, generated-artifact, or release evidence requires escalation. Local branch position and worktree dirtiness are irrelevant because they are not publication inputs. Tag the verified remote SHA explicitly; never infer it from local `HEAD`. Any failed or unprovable condition falls back to native receipt validation and fails closed on missing, changed, invalidated, or escalated state. Major releases and releases following an operational or security incident always require explicit extraordinary review even when the fast-path checks pass.

Outside that protected-`main` fast path, release validation additionally requires an immutable release tree plus content hashes for configuration, generated artifacts, provenance/signing, publication boundary, and evidence freshness. The strict publication-boundary artifact uses schema `gentle-ai.release-publication-boundary/v1` and binds `release_tree` plus `state: sealed`. The strict freshness artifact uses schema `gentle-ai.release-evidence-freshness/v1` and binds the same `release_tree`, the current verification `evidence_hash`, and `state: current`. File presence or caller-authored state cannot authorize publication. External evidence uses strict schema `gentle-ai.external-review-evidence/v1` with an `invalidating|escalating` disposition and cryptographic `evidence_hash`; success booleans are unsupported. A generic base-relationship context cannot authorize publication.

New CI, vulnerability, base, policy, provenance, or release evidence may invalidate or escalate without reopening unchanged code review. Operational incident separation remains valid only while code, configuration, generated-artifact, and provenance targets are immutable.

## User-Owned Runtime Selection

Model, provider, profile, and effort selection remain optional user choices. This review contract never enforces or silently changes those settings.

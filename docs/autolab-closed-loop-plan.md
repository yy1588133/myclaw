# MyClaw Self-Iteration Closed-Loop Implementation Plan

## 1) Goal
Build a safe self-iteration workflow where MyClaw can continuously improve source code without breaking the running production service.

## 2) Confirmed Policy Decisions
- Required checks: Strict
  - lint
  - go test ./...
  - go test -race ./...
  - go build ./...
  - smoke test (bot message round-trip and key command path)
- Merge strategy: squash
- Deployment strategy: auto deploy after approval
- Approver scope: all users who can talk to MyClaw (single-user personal mode assumption)
- Rollback strategy: user selects rollback target branch (multiple options allowed)

## 3) Core Design
- Production lane (stable): main branch and running systemd service.
- Experiment lane (isolated): autolab branches for all code changes.
- No direct edits to production lane during experiments.
- Promotion path is fixed: autolab branch -> PR -> strict checks -> user approval -> squash merge -> auto release tagging.

## 4) End-to-End Workflow
1. A new change request arrives from IM.
2. MyClaw creates autolab branch from latest main.
3. MyClaw modifies code only on that branch.
4. MyClaw runs local validation (fast checks) before opening PR.
5. MyClaw pushes branch and opens PR with change summary and risk notes.
6. CI runs strict required checks.
7. If checks fail, MyClaw fixes on the same branch and re-runs CI until green.
8. If checks pass, MyClaw asks user for merge approval.
9. On approval, PR is merged via squash.
10. Merge event triggers release tagging and release artifact generation.
11. Operator downloads selected release binary and deploys manually to production.
12. If deployment validation fails, rollback is executed from user-selected target branch option.

## 5) Mandatory Guardrails
- Never commit directly to main.
- Never deploy from non-main branches.
- Never bypass required checks.
- Never auto-approve merge or deploy without explicit user approval message.
- Every PR must include:
  - what changed
  - why it changed
  - risk impact
  - rollback options

## 6) Strict Checks Definition
CI must block merge unless all pass:
1. lint gate
2. unit and integration tests: go test ./...
3. race detection: go test -race ./...
4. build gate: go build ./...
5. smoke gate:
   - gateway or agent startup sanity
   - at least one real bot request path returns success

## 7) Approval and Deploy Rules
- Approval signal source: MyClaw conversation channel.
- Approval decision is recorded in PR comment and release/deploy log.
- After approval:
  - squash merge PR into main
  - generate release artifacts via tag + release workflows
  - perform manual production deployment from selected release binary
  - post deployment status back to conversation

## 8) Rollback Model (User-Selectable, Multi-Option)
When rollback is required, MyClaw must present options and let user choose:
- Option A: rollback to previous stable release branch or tag
- Option B: rollback to a specific stable branch selected by user
- Option C: rollback to a specific commit SHA selected by user
- Option D: rollback to one of N recommended safe branches generated from recent successful deploy history

## 9) Repository and Infra Artifacts to Implement
- Git protections:
  - protect main
  - require PR
  - require strict status checks
  - disable force push
- CI workflows:
  - pr-verify workflow (strict checks)
  - tag-main workflow (auto semantic tag on main)
  - release workflow (build binaries/checksum/GHCR)
  - rollback workflow (manual dispatch with selectable targets)
- Automation scripts (suggested):
  - scripts/autolab/start.sh
  - scripts/autolab/verify.sh
  - scripts/autolab/submit.sh
  - scripts/autolab/promote.sh
  - scripts/autolab/rollback.sh

## 10) Operational Acceptance Criteria
The closed-loop is active only when all are true:
- MyClaw can create and iterate autolab branches automatically.
- Main cannot be changed without PR and strict checks.
- Approved PR merges by squash only.
- Merge to main automatically produces release artifacts.
- Failed deploy can be rolled back using user-selected branch options.
- Production bot service remains available during branch experiments.

## 11) Single-User Security Assumption
This plan assumes personal single-user usage even across multiple IM channels.
If scope changes to multi-user or team mode later, approver identity mapping to GitHub users must be added before keeping auto deploy enabled.

## 12) GitHub Plan Constraint (Resolved)
- Repository visibility has been changed to public.
- Server-side branch protection is now enabled on main.
- Required checks currently enforced: lint, test, race, build, smoke, secret-audit.
- Required GitHub review count is set to 0 for single-user mode to avoid self-approval deadlock.

## 13) Implementation Progress (Updated 2026-02-09)
Completed:
- [x] Branch-first workflow scripts implemented and enforced in daily use.
- [x] Strict CI workflows active: pr-verify and secret-audit.
- [x] Main branch protection enabled with required checks (lint/test/race/build/smoke/secret-audit).
- [x] Merge policy enforced as squash-only.
- [x] Multiple autolab PRs merged and released without service interruption.
- [x] Release pipeline supports multi-platform binaries, checksums, and GHCR images.
- [x] Manual production deploy model documented (download release binary + restart service).
- [x] Rollback workflow_dispatch drill executed with non-main target (08c59ce), rollback branch created automatically.
- [x] Rollback drill PR established and validated (PR #9, checks green, kept unmerged).

In progress:
- [ ] None.

Next execution order:
1. Keep rollback PR #9 unmerged as emergency baseline.
2. Merge rollback PR only when explicit user approval is given.
3. Record future rollback drills in this section after each major release milestone.

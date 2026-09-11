# Native CI

`%urgit` gains a CI system that runs GitHub Actions-style workflows without GitHub. The ship is the controller. External Linux hosts execute jobs. This document describes the design. No code in this document exists yet.

The first acceptance fixture is ERPit. Its two workflows, `suite.yml` and `fixtures.yml`, define eight jobs and use `actions/checkout@v4` and `actions/cache@v4`. The design must run those eight jobs natively before GitHub Actions is switched off for that repository.

## Boundary

```text
git push / PR merge / web edit
  |
  v
%urgit
  |- refs, objects, policy (unchanged)
  |- protected-ref write gate: scry %urgit-ci before advance
  `- integration candidate materialization

%urgit-ci
  |- workflow revisions and required-workflow baseline
  |- runs, jobs, attempts, results
  |- runner enrollment and job assignment
  |- approvals, overrides, promotions
  |- credential store and CI signing key
  `- checkpoint export

runner (external Linux host, one static binary)
  |
  | runner-initiated authenticated connection
  v
%urgit-ci assignment channel
  |
  v
runner
  |- supervisor: connection, claim, VM lifecycle
  |- compiler: workflow YAML -> execution plan
  `- executor: one job in one disposable VM

runner
  |
  | signed PUT / GET issued by %urgit-ci
  v
ship-configured object storage
  |- logs, artifacts, caches
  `- keyed by repository / run / attempt / trust class
```

## Workflow model

Workflow YAML under `.github/workflows/` is authoritative. `%urgit-ci` does not define a second workflow format. The runner's compiler reads the YAML and produces a bounded execution plan. The plan lists jobs, `needs` edges, matrix expansion limits, the step list for each job, and every action reference resolved to an exact commit. `%urgit-ci` validates the plan's structure, limits, source identity, and requested permissions before it accepts the plan.

The compiler is trusted infrastructure. It is version-pinned and executes no repository script. A validated plan is a translation, not a proof. The ship checks the plan's shape. It cannot check that the translation is faithful.

`%urgit-ci` evaluates job-level expressions. These are `if:` on a job, `needs.<job>.outputs.<name>`, and matrix expansion. Evaluation runs against accepted attempt state within fixed bounds. The executor evaluates step-level expressions inside the job. An evaluation error is an error. It never becomes `false`, `skipped`, or success.

Unsupported syntax produces a diagnosed error in the run. It is never dropped silently. Each imported action is classified in one of three ways: usable unchanged, needs a native equivalent, or still GitHub-dependent. `actions/cache` needs a cache service. `actions/checkout` has GitHub API fallback behavior. The import view shows the classification before the workflow is enabled. "Imported" does not mean "verified compatible".

## Execution

The runner is one Go program with three parts. The supervisor holds the connection to `%urgit-ci`, claims assignments, and manages VM lifecycle. The compiler translates workflow YAML. The executor runs one job inside one VM. The executor derives from the Forgejo runner's single-job execution path at a pinned commit. It is not the Forgejo daemon. Upstream `act` is the fallback. GitHub's official worker is rejected because it requires GitHub's job and run services.

The supervisor opens the connection. It authenticates, reports capacity, and waits. `%urgit-ci` selects a runner and sends the assignment over that connection. No inbound port is required on the runner host. Dispatch authority stays on the ship.

Each job runs in a fresh VM booted from an approved image with resource limits. A workflow may start containers inside the VM. The host Docker socket is never shared. The guest cannot reach the supervisor's control socket, other Gall agents on the ship, or LAN addresses outside policy. The supervisor destroys the VM after the job ends, whatever the outcome. A failed teardown marks the slot as quarantined and blocks reuse. There is no fallback to a shared host.

The supervisor enforces deadlines. After a restart it reconciles orphaned VMs against `%urgit-ci`'s assignment records. A stale attempt cannot overwrite a newer one.

## Trust and credentials

A revision from an untrusted source needs approval before it runs. Approval is per revision. A repository may opt in to automatic restricted checks for untrusted revisions. A restricted check receives no credentials, cannot write to a trusted cache namespace, and cannot deploy or publish. Approval to test grants no other permission.

A job that needs a deployment or publishing credential needs a separate approval by default. A repository may define automation rules for named environments. For example, staging deploys automatically and production requires a manual approval. These rules live in `%urgit-ci` policy, not in workflow YAML. A privileged job runs in a fresh VM from a trusted image. `%urgit-ci` never redeploys blindly after a lost response.

`%urgit-ci` stores third-party credentials. It releases a credential only to an authorized attempt, scoped to that attempt, for a bounded time. An external vault is optional and not required. Credential values enter pier history when they are stored. Rotation and revocation with the provider are the operator's responsibility.

A dedicated CI signing key lives in `%urgit-ci`. The ship's networking authentication key certifies the CI key. The CI key signs each credential grant and each assignment. Verifiers hold the public certificate chain only. Neither private key leaves the ship. The signature format names the recipient, the attempt, the operation, an expiry, and a nonce. A signature is authorization. It does not encrypt.

## Protected refs

Today `%urgit` enforces two rules on a protected branch. The branch cannot be deleted. Updates must be fast-forward. A pull-request merge constructs the merge commit at merge time. None of these steps tests the commit that lands.

A CI-protected branch advances in one of two ways. Either the new tip is a tested integration candidate, or an override role records an explicit override. There is no third path.

`%urgit` materializes the candidate without moving the ref. For a divergent source, the candidate is the merge of the source head onto the current destination tip. For a fast-forward, the candidate is the source head. `%urgit-ci` runs the required checks against that exact object ID. At landing, `%urgit` scries `%urgit-ci` for eligibility. It advances the ref only when the candidate is eligible under current policy and the destination tip has not changed. `%urgit` never regenerates a commit after testing. A different timestamp or message is a different commit.

A pull request with many commits is one candidate. Candidates run checks concurrently. Landing is ordered. A candidate built on an expected predecessor stays eligible when that predecessor lands as expected. If the predecessor fails or the destination moves unexpectedly, the candidate is rebuilt and retested.

Required checks come from an approved workflow revision recorded in branch policy. They do not come from the candidate's own YAML. A pull request that edits a workflow gets an unprivileged trial run of the edited workflow. Trial results do not satisfy the required checks. A maintainer with CI-policy permission promotes the new revision. Then the required checks run again against the candidate. The approved revision covers the scripts and local actions the YAML calls, not the YAML file alone. ERPit's `suite.yml` reads its structural test list from `bin/test.sh`, so that script is part of the baseline.

A direct push, web edit, or import to a CI-protected branch is never applied. `%urgit` stages the pushed head as a candidate and answers the push with `ng <ref> staged as candidate <id>`. The candidate lands through the same checks as a pull request. An override is a separate recorded action by a branch override role. The record names who, which object ID, why, and which evidence was missing. An override is never a push flag. A Clay-bound protected branch publishes to the desk only when the candidate lands.

## Storage

Logs, artifacts, and caches go to an S3-compatible object store. `%urgit-ci` reads the endpoint, bucket, region, and credentials from the ship's `%storage` agent, the same source `%urgit` uses for Git LFS. Any store that accepts Signature Version 4 requests works. There is no separate urgit setting. If `%storage` is configured, CI uses it. If `%storage` is not configured, `%urgit-ci` refuses to enable CI on any repository and reports the missing configuration.

`%urgit-ci` signs a short-lived upload URL for each attempt and a short-lived download URL for each authorized viewer. Only handles, sizes, hashes, and completion state enter Gall state. Bounded progress messages travel over the assignment channel. Full logs upload in chunks.

CI keys use a prefix that LFS cleanup never scans. The operator may point CI at a separate bucket. A shared bucket with the CI prefix is the default.

Keys are namespaced by repository, run, attempt, and trust class. `%urgit-ci` refuses to sign a read across trust classes. An untrusted run's cache cannot be read by a trusted run. Cache poisoning is blocked at signing time, not by runner cooperation.

A store outage yields `unknown` for the affected attempt. It never yields success. Store retention and backup are operator policy, separate from the checkpoint below.

## Recovery

`%urgit-ci` writes a checkpoint on every CI-policy change and on a configured schedule. A checkpoint contains the CI-configuration projection only:

- branch CI policy and approved workflow revisions
- environment rules and override roles
- runner enrollment records with public keys
- credential references without values
- the CI certificate chain
- bounded run summaries with store handles
- the approval and override audit
- a manifest

A checkpoint excludes repository objects, LFS payloads, store bytes, credential values, private keys, live leases and approvals, and the GitHub token. Hosted repositories are recovered by Git clone or mirror. The checkpoint does not duplicate them.

`%urgit-ci` commits each checkpoint to a private urgit repository on the ship and pushes it to an independently held clone. "Checkpoint written" and "replica confirmed" are separate records. A failed push is a failed backup.

Restore is explicit. Install the desk, import the checkpoint into a paused `%urgit-ci`, review the manifest and diff, then apply. Runners re-enroll. The recovered ship certifies a new CI key. Credential references show "needs re-entry". Old approvals and leases do not become live again. Recovery writes never trigger a workflow or a webhook.

## Packaging

`%urgit-ci` is a fourth agent in the desk beside `%urgit`, `%urgit-clay`, and `%urgit-fileserver`. Its persisted state starts at `%0`. `%urgit` changes in three places: the protected-ref gate, candidate materialization, and the staged-write handoff. `%urgit` scries `%urgit-ci` for landing eligibility. `%urgit-ci` pokes `%urgit` to materialize a candidate. Git objects never leave `%urgit`. `%urgit-ci` binds its own API base under `/apps/urgit/api` and the runner assignment channel. Both agents rebind their Eyre routes on every load.

The web interface is unchanged in shape. `%urgit-fileserver` serves the same React application. The repository page gains a CI tab for runs, run detail, and candidate status. Settings gains runner enrollment and CI-policy sections. The application calls two API bases under one origin.

The runner lives at `runner/` in this repository as one Go module. `zig build` gains an optional Go step. The deliverable is one static binary, one systemd unit, and one configuration file. The configuration names the ship URL, an enrollment token, the VM image path, and the object-store endpoint. The runner installs on any Linux host with KVM. It may share a host with the ship or not. The object store defaults to a second unit on the same host and accepts any S3-compatible endpoint.

CI has one install prerequisite beyond the runner. The ship's `%storage` agent must name a reachable S3-compatible endpoint with a bucket and credentials. This is a Landscape settings step. Without it, `%urgit-ci` does not enable CI.

## Cutover

Native CI runs beside GitHub Actions on ERPit for a shadow period. Both systems report on every pull request and push. A disagreement is a native-CI defect until proven otherwise. Cutover requires, in order:

1. All eight ERPit jobs run natively end to end, including the `needs.plan.outputs.replay == 'true'` gate, label and path filters, cancellation, and fake-ship cleanup.
2. N consecutive real runs agree on both systems, including a deliberate failing candidate that fails on both.
3. The full negative-test suite passes.
4. One recovery drill from a checkpoint succeeds on a disposable ship.
5. Operator documentation is complete for install, upgrade, rollback, credential rotation, retention, runner enrollment, and override.
6. The operator records explicit cutover approval.

The supported envelope is published from what the shadow period sustained. That is maximum concurrent jobs, log and artifact size limits, retention, supported guest images, supported action types, and matrix limits. No envelope number is declared from design targets.

## Repository changes

- `desk/app/urgit-ci.hoon` — new agent.
- `desk/sur/ci.hoon`, `desk/lib/ci-*.hoon` — new types and pure rules.
- `desk/desk.bill` — adds `%urgit-ci`.
- `desk/app/urgit.hoon` — protected-ref gate, candidate materialization handler, staged-write handoff.
- `desk/sur/git.hoon` — a way to mark a protected ref as CI-protected. Either `repository` gains a field or `%urgit-ci` holds the set and `%urgit` scries it.
- `fe/src/` — CI tab, Settings sections, second API base.
- `runner/` — new Go module.
- `build.zig` — optional Go step.
- `specs/architecture.md`, `README.md` — updated.

## What does not change

The Git protocol layers, object model, pack codecs, LFS, native collaboration, GitHub integration, and webhooks are untouched. Clay projection semantics are unchanged except that a CI-protected branch publishes only on landing. Unprotected branches keep Git's normal behavior. Protected branches that are not CI-protected keep today's fast-forward rule.

## Alternatives considered

- An external CI server as controller (Woodpecker, Buildbot). Rejected. The controller would live outside `%urgit`, and `%urgit` would become a webhook source.
- Compile workflow YAML on the ship. Rejected. The YAML and expression semantics are large, and the Hoon port becomes the bottleneck.
- Containers on the host instead of VMs. Rejected for untrusted code. It is documented as a downgrade that is never taken silently.
- An off-ship secret vault as the default. Rejected. The ship already holds `%storage` credentials. Another host adds a dependency without removing trust in the ship.
- Test the pull-request head only. Rejected. It approves an untested merge result.
- Extend `%urgit` with a new state version. Rejected. CI state and forge state have different lifecycles.
- A separate runner repository. Rejected. The ship-to-runner protocol would drift across two release trains.
- A checklist cutover without shadow mode. Rejected. Synthetic fixtures miss the timing, label, cancellation, and cleanup paths that real traffic exercises.

## Open questions for upstream

- Is a fourth agent in the desk acceptable?
- Is a Go directory in this repository acceptable? The alternative is a separate repository with the drift cost above.
- Is the change to protected-ref semantics acceptable? A CI-protected branch stages a direct push instead of applying it.
- Should CI-protection be a field on `repository` or a set held by `%urgit-ci`?
- Is vendoring the Forgejo runner's execution path acceptable? Its license is MIT.
- Should the ERPit workflows live in this repository as acceptance fixtures, or only a generic fixture set?
- Should `%urgit-ci` accept its own object-store configuration, or always read `%storage`? Reading `%storage` is simpler. A separate configuration lets CI use a different store than LFS and removes the dependency on Landscape settings.

## Delivery order

1. Contracts and harness: the `%urgit` to `%urgit-ci` scry and poke interface, the ship-to-runner protocol, object-store signing, and a fake-ship plus real-runner harness. Every negative test above must be able to fail.
2. Workflow model: compiler, plan validation, job-level expressions, action classification.
3. `%urgit-ci` core: runs, jobs, attempts, results, assignment.
4. Runner: supervisor, VM lifecycle, executor adaptation.
5. Trust: approvals, credential store, CI signing key.
6. Storage and web interface.
7. Protected-ref gates, shadow period, cutover.

Each step is verified before the next starts.

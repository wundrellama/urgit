# BRIEF — CI P1: planner, scheduler, runner daemon — the installable product

You are one chair in a fleet building the same brief on identical bytes. Your
worktree, branch, ship, port, and pier are in the launch footer at the end. Read
this whole file before touching anything. Where it cites the spec, the spec
governs; where the spec is silent, the numbered derivation D<n> governs; where
neither speaks, stop and box it (§ format in §6).

## §1 — What P1 is

P0 (merged, `master` at `2647969`) built the contracts: `%urgit-ci` state, the
`%urgit` ↔ `%urgit-ci` scry/poke seam, the four daemon HTTP routes, the event
envelope, storage signing, and a harness where every negative row fails first.
Every scheduling act in P0 was a dojo poke, and the "daemon" was a shell loop.

P1 replaces the human and the shell loop. After P1, an operator who has
`%storage` configured can: enroll a runner daemon from the config file; push to
a CI-protected ref; watch the ship stage the candidate, read the workflow from
the candidate commit, plan the jobs, assign each to an enrolled daemon; watch
the daemon run each job under real `act` in an isolated sandbox and relay the
stream; and see the ship land the ref itself or refuse on the recorded verdict — **with
no dojo line and no second push between the push and the landing.** That is the product the upstream
maintainer installs. It is spec steps 2 + 3 + 4 delivered together
(CI-SANDBOX-1-B; `REFERENCE-CI-RULINGS.md` in your worktree).

Not in P1: approvals/credential store/signing key (step 5), object-store upload
and web UI (step 6), shadow period and cutover (step 7), the `microvm` sandbox
backend (P2). Each is named in the fence.

## §2 — Read order (all on your base commit; verify with `git ls-tree HEAD`)

1. `specs/native-ci.md` — sections *Workflow model*, *Execution*, *Packaging*,
   *Delivery order*. Cited below as spec §<name>.
2. `REFERENCE-CI-RULINGS.md` (untracked, planted) — the ruling map. Load-bearing
   for P1: R1-A, R2-A, R2.2-A, R2.3′-A, R3.1-A, R3.2-A, R6.2-A, CI-EMPTY-REF-1-A,
   CI-SANDBOX-1-B.
3. `desk/sur/ci.hoon` — every mold you will grow. `desk/app/urgit-ci.hoon` — the
   agent you will extend; arms by line at base: `on-load` 38, `on-peek` 73,
   `on-arvo` 135 (deadline wake at 157), `handle-action` 238 (`%set-ci-protected`
   242, `%stage-candidate` 252, `%candidate-ready` 279, `%assign` 305),
   `close-attempt` 344, `assignment-json` 445, `handle-http` 483,
   `handle-enroll` 511, `handle-assignment-poll` 547, `handle-event` 586,
   `handle-result` 618.
4. `desk/app/urgit.hoon` — `ci-gate-error` 7907, `materialize-candidate` 7956,
   `file-at-commit` 889 (`[repo commit path] → (unit octs)`), the `%ci-action`
   poke case 2248, `on-peek` `[%x %repository @ ~]` 9119.
5. `desk/lib/ci-event.hoon`, `desk/lib/ci-storage.hoon`, `desk/lib/ci-candidate.hoon`.
6. `.scratch/ci-p0/` — the P0 harness (26 scripts; `boot.sh`, `prelude.sh`,
   `setup.sh`, `h*.sh`, `mutants.sh`, `negatives.sh`) and
   `.scratch/p0-live-table.md`. Your harness EXTENDS this; it does not replace it.
7. `.scratch/spike-001a-act-README.md` (untracked, planted) — the verified
   `act` recipe and `--json` schema. ERPit's real workflows, read-only, at
   `/var/home/michael/workspace/urbit/erpit/.github/workflows/{suite,fixtures}.yml`
   with `act --list` output for both in `.scratch/act-list-erpit.txt` (planted).

Build/tooling facts: `zig build` (zig 0.15.2) assembles the desk;
`zig build -Ddesk=<pier>/urgit` installs it. **Go is not on this host at
dispatch time** — `brew install go` (1.27.1 bottled) is your first command; say
so in your record. `act` 0.2.89 on PATH; image `catthehacker/ubuntu:act-latest`
cached (`--pull=false`). This host's default Docker socket is ROOTFUL; a
rootless daemon exists or can be started per-user (`dockerd-rootless-setuptool.sh`
— check `docker context ls` and `docker info --format '{{.SecurityOptions}}'`
for `name=rootless`). This host's `grep` is ugrep: use `grep -F` for `++  name`
and `+$  name` patterns.

## §3 — Derivations (the spine; cite these in your record)

**D1 — The planner is `act --list` on the candidate checkout, run by the DAEMON,
validated and stored by the SHIP.** Spec §Workflow model: "The controller runs a
pinned `act --list` adapter against the candidate's workflow files … validates
the plan shape." R2.1-A was folded into R2.3′-A: the planner IS `act --list`.
But the ship cannot run a binary; the daemon can. So: the ship's first
assignment for a new candidate is a **plan assignment** (`kind=%plan`); the
daemon checks out the candidate, runs `act --list --json`-equivalent (`act -l`
prints a table; parse it — columns `Stage Job-ID Job-name Workflow-name
Workflow-file Events`, verified in `.scratch/act-list-erpit.txt`) for every
workflow file under `.github/workflows/`, and POSTs the plan to a new route
`POST /apps/urgit/api/ci/attempt/<id>/plan`. The ship validates: every job id
unique per workflow, every `Stage` a natural number, workflow files exist in the
candidate's tree (the ship reads the tree — D2), and stores `plan=(list job)` on
the candidate. A plan the ship cannot validate is `%plan-invalid` with the
reason; the candidate is `%failed` with that reason (R1-A: diagnosed, never
dropped). `act --list` exits non-zero on a workflow act cannot parse; that is
`%plan-invalid` too, with act's stderr as the reason.
*Cite: spec §Workflow model ¶1–2; R2.3′-A; R1-A.*

**D2 — `%urgit` gains ONE new peek so `%urgit-ci` can read a file at a commit:**
`[%x %ci-file @ @ @ ~]` → `(unit octs)`, path segments `repo`, `oid` (40-hex),
and the file path as one `(scot %t …)`-encoded cord (slashes inside). It calls
`file-at-commit` (889). `%urgit-ci` uses it to confirm each workflow file the
plan names exists at the candidate OID (D1) and to read `.github/workflows/*`
names — for which `%urgit` gains a sibling `[%x %ci-tree @ @ @ ~]` →
`(unit (list path))` listing entries under a directory at a commit (via
`flatten-commit:git-tree`, keys filtered by prefix). Both peeks are
`our`-only. **`%urgit`'s whole P1 touch is enumerated in §6** (these peeks, D9, D11, D14, D16).
*Cite: spec §Packaging "`%urgit` scries `%urgit-ci` … `%urgit-ci` pokes `%urgit`" — reads flow the other way here, as peeks; R6.1-A.*

**D3 — Job-level `needs` and `if` are evaluated by the SHIP, bounded, from
recorded outputs.** Spec §Workflow model: "job-level `if`, `needs`, matrix, and
concurrency are evaluated on the ship … bounded … An expression the ship cannot
evaluate is a diagnosed refusal, never `false`, never `skip`, never green"
(R2.2-A). P1 implements the subset ERPit uses, verified on its two workflows:
`needs: <id>` and `needs: [<id>, …]`; `if: needs.<id>.outputs.<name> == '<lit>'`
(single equality against a string literal); `if:` absent = run. `act --list`
does not print `needs`/`if`, so the daemon's plan POST also carries, per job,
the `needs` list and a **compiled** condition. R2.2-A puts the compiler
OUTSIDE the ship: "the external compiler supplies structured, versioned
expressions; the ship does not parse raw GitHub expression syntax." So the
daemon's YAML walk (Go) emits `cond` as one of exactly two shapes —
`{"v":1,"kind":"output-eq","job":"plan","output":"suite","literal":"true"}`
for the supported form, or `{"v":1,"kind":"unsupported","raw":"<the string>"}`
for anything else — and never sends a raw string as the condition. The ship
evaluates `output-eq` structurally and refuses `unsupported` with `raw`
quoted. **No string parsing of expression syntax on the ship, ever.** A
`cond` with `v != 1` or an unknown `kind` is `%plan-invalid` (`unknown
condition version`). The ship evaluates when
every job in `needs` has a terminal attempt: all `%passed` and the `if` holds →
schedule; any `needs` job `%failed`/`%unknown` → the dependent is `%skipped`
(recorded, not silent); `if` references an output that was never set →
`%skipped` with reason `output not set`, never a crash; a `cond` of kind
`unsupported` → the JOB is `%plan-invalid` with `raw` quoted, and the
candidate `%failed`. **Matrix** (`strategy.matrix`) is absent from ERPit's
workflows (the suite says so at `suite.yml:13`); a job carrying one is
`%plan-invalid` with reason `matrix unsupported in P1`. **Workflow-level
`concurrency:`** IS present in both ERPit workflows (`suite.yml:46`,
`fixtures.yml:90`); it is a scheduling hint about cancelling superseded runs,
not a job predicate; `act` ignores it and so does the ship in P1 — the ship
already serializes landing (R4.1-A) and a superseded candidate simply never
lands. The daemon's YAML walk does not send it; the ship records nothing for
it. Step-level `if:` (eight in ERPit) is act's, not the ship's — never parse it.
*Cite: spec §Workflow model ¶2; R2.2-A; R4.1-A.*

**D4 — Candidate lifecycle and scheduling are automatic and live in
`%urgit-ci`.** Today `%stage-candidate` records and stops; `%candidate-ready`
records and stops; `%assign` is a dojo poke. P1: on `%candidate-ready` the ship
immediately creates the plan assignment (D1) for the least-loaded enrolled
daemon that has reported capacity (D6), answering any waiting long-poll. On a
valid plan, the ship creates a **job assignment** (`kind=%job`, naming the
workflow file and job id) for every job with no `needs`, then on each job
attempt's terminal close re-evaluates D3 for every dependent and assigns what
became runnable. **Jobs are keyed by `[workflow job]`, never by job id alone:**
ERPit has a `plan` job in BOTH `suite.yml` and `fixtures.yml`, and
`needs.plan.outputs.suite` resolves inside `suite.yml` while
`needs.plan.outputs.replay` resolves inside `fixtures.yml`. `needs` never
crosses a workflow file. Candidate verdict: `%passed` when every planned job is
`%passed` or `%skipped`-by-`if` (an `if` that is false is a skip that does not
fail the candidate — that is GitHub's semantic and ERPit's `plan` gate depends
on it); `%failed` on the first `%failed` job; `%unknown` on any
`%infrastructure-error` with no other verdict. On `%passed` the ship pokes
`%land-candidate` (D14) at once; the push lands with no further client act. The existing `%assign` poke
stays for the harness and for an operator re-run; it now takes
`kind` and `job` so a single job can be re-driven.
*Cite: spec §Execution ¶3 "`%urgit-ci` selects a daemon and sends the assignment"; R2-A.*

**D5 — `%stage-candidate` and `%materialize` collapse into one automatic step.**
`%urgit` already pokes `%stage-candidate` from the receive gate; P1 makes
`%urgit-ci` poke `%materialize-candidate` back immediately on stage (today that
poke is behind the `%materialize` dojo action). `%materialize` stays as the
manual re-trigger.
*Cite: spec §Protected refs ¶2; R4.1-A.*

**D6 — Daemon capacity and selection.** Enrollment (`POST /daemon/enroll`)
gains an optional `capacity` (natural, default 1) and `sandbox` (string,
`docker-rootless` in P1) in the body; the daemon mold gains `capacity=@ud`,
`sandbox=@t`, `running=(set attempt-id)`. The long-poll answers one assignment
per request; a daemon with `running` at `capacity` is not selected. A daemon
whose `last-seen` is older than `~m5` is not selected (its `running` attempts
hit their deadlines normally). Selection: fewest `running`, ties by oldest
`enrolled`.
*Cite: spec §Execution ¶3 "reports capacity, and waits".*

**D7 — The runner daemon (`runner/`) is one Go module, one static binary.**
Layout is yours; the contract is not: `runner/cmd/urgit-runner/main.go`;
`runner/internal/{ship,sandbox,act,relay,plan}`; `runner/urgit-runner.toml.example`;
`runner/urgit-runner.service`; `runner/README.md`. Config (TOML) names:
`ship_url`, `enroll_token` (consumed once; the daemon persists `daemon_id` +
`bearer` to a state file named in config, mode 0600), `sandbox = "docker-rootless"`,
`docker_host` (the rootless socket), `act_binary`, `act_image` (the `-P
ubuntu-latest=` mapping), `capacity`, `work_dir`, `state_file`. The daemon
loop: enroll if no state file → long-poll `GET /daemon/<id>/assignment` with
`x-ci-bearer` → on `%plan`: sandbox, checkout, `act -l` per workflow, POST plan
→ on `%job`: sandbox, checkout, `act push -W <file> -j <job> -P … --network
<isolated> --json`, relay every stdout JSON line to `POST /attempt/<id>/event`
as the P0 harness did (the docker-host banner is not JSON and is dropped — P0
already answers `400` to it; do not send it), then `POST /attempt/<id>/result`
with the `jobResult` the stream carried, or **no result POST at all** when act
exited without a `jobResult` line (D8) → destroy sandbox → poll again. Checkout:
`git clone --no-checkout <ship_url>/git/<repo> && git fetch origin <oid> && git
checkout <oid>` — the candidate OID is reachable because `materialize-candidate`
exposes it on `refs/ci/candidate/<candidate-id>` (D9). The daemon **never
touches the ship session, never holds `%storage` credentials, never decides
anything** — every branch in its code that chooses between outcomes must be
traceable to a ship-sent field or an act-emitted line.

Three more sentences the spec owes the daemon: (a) `runner/urgit-runner.service`
is a **systemd** unit (`Type=simple`, `Restart=on-failure`, runs as an
unprivileged user, `EnvironmentFile=` optional) and the README shows the
three-command install; (b) the **enrollment token** is minted on the ship
(`:urgit-ci|mint-enroll-token`, P0), pasted once into the config, and the
daemon deletes it from its own memory after the enroll call succeeds — it
never writes the raw token to the state file; (c) on startup with a state
file, before polling, the daemon **reconciles orphans**: it lists its own
leftover sandboxes (containers/networks/volumes tagged `ci-<attempt>`), asks
the ship `GET /attempt/<id>` for each (new read route, bearer-authenticated,
answers the attempt's status), destroys every sandbox whose attempt is
terminal or unknown to the ship, and resumes none — a job the daemon was
running when it died is the deadline's to close (D8), never re-run by the
daemon. Spec: "After a restart it reconciles orphaned VMs against
`%urgit-ci`'s assignment records. A stale attempt cannot overwrite a newer
one." — the ship enforces the second sentence by refusing events and results
on a closed attempt (P0 already answers `409`).
*Cite: spec §Execution ¶1–2, ¶5; spec §Packaging ¶3; R6.2-A.*

**D8 — `infrastructure-error` is the ship's inference from a missing result,
never the daemon's claim.** Spec §Execution ¶2: "If `act` exits without a
`jobResult` event, the daemon reports `infrastructure-error`. It never infers
success." P0 ruled the mechanism: the daemon POSTs no result; the deadline
timer (`on-arvo` 157) closes the attempt `%infrastructure-error`. P1 shortens
the wait: the daemon POSTs `/attempt/<id>/abandon` with a reason string (act
exit code, sandbox failure, teardown failure) and the ship closes the attempt
`%infrastructure-error` with that reason immediately. A daemon that dies
mid-job still falls to the deadline. `abandon` on an attempt that already has a
result → `409`.
*Cite: spec §Execution ¶2; P0 D8.*

**D9 — Candidate objects reachable for clone: `materialize-candidate` also sets
`refs/ci/candidate/<candidate-id>` to the candidate OID; the ref is deleted when
the candidate reaches a terminal status.** P0's fable harness proved
`upload-pack` serves only ref-reachable objects and used `%set-ref` by hand for
H5; P1 makes it the contract. `refs/ci/*` is never CI-protected, never
fast-forward-protected, and hidden from `[%x %repository @ ~]`'s ref list
(filter by prefix in `public-repository-json`; **that filter is the third and
last `%urgit` touch**). Deletion goes through the existing ref-update path.
*Cite: spec §Protected refs ¶2 "%urgit materializes … the object store"; R4.1-A.*

**D10 — The sandbox interface is VM-first; P1 ships `docker-rootless`.**
`runner/internal/sandbox/sandbox.go`:
```go
type Spec struct { Image string; CPUs int; MemoryMiB int; DiskMiB int; Network string }
type Sandbox interface {
    Prepare(ctx, Spec) (Handle, error)            // boots or creates; Image and limits honoured or ignored, never errors on unsupported
    Copy(ctx, Handle, hostDir, guestDir) error    // checkout enters by COPY — never a bind mount
    Run(ctx, Handle, argv, env) (io.ReadCloser, <-chan int, error)  // stdout stream + exit code; nothing else comes back
    Destroy(ctx, Handle) error                    // fallible; caller quarantines the slot on error
}
```
`docker-rootless`: `Prepare` creates a per-attempt Docker network on the
rootless daemon (`docker network create ci-<attempt>`) and a per-attempt work
volume; `Copy` is `docker cp` into a helper container bound to that volume;
`Run` starts a container from `act_image`'s runner base with the rootless
socket mounted (the socket belongs to the rootless daemon, never the host's)
and runs `act` inside it with `--network ci-<attempt>`; `Destroy` removes
container, volume, network, and returns the first error.

**Network policy in P1 (rule it once, do not fork on it):** "isolated" means
*isolated from other attempts and from the host's own services*, not
air-gapped. The per-attempt network is a user-defined bridge with NAT egress
(Docker's default; NOT `--internal`), because ERPit's jobs fetch
`actions/cache@v4` and the urbit toolchain from the internet, fake ships bind
Ames inside the container (the spike proved `--network host` breaks that and
bridge fixes it), and the checkout (D7) clones from the ship's HTTP endpoint,
which the sandbox reaches through NAT like any other address. What the sandbox
must NOT reach: the daemon's own state file and config (they are on the host
filesystem, never mounted — `Copy` is the only path in), the host's rootful
Docker socket (never mounted), and other attempts' networks (separate bridges;
Docker's default `enable_icc` is per-network). Egress allow-listing is a P2
item alongside the VM backend; the README states this plainly. The clone works
because the fixture repo is `publicRead` (P0 recipe); a private repo in P1
gets the same result as any anonymous clone — `can-read` refuses — and that
is `%plan-invalid` with the clone's stderr. Private-repo checkout credentials
are step 5's problem (a per-attempt read token the ship mints), not P1's; say
so in the README's limitations list. `microvm`: a
constructor that returns `errors.New("microvm sandbox is not available in this
release")` and the config keys `image_path`, `cpus`, `memory_mib`, `disk_mib`
already parsed into `Spec`. A quarantined slot: the daemon decrements its
advertised capacity by one, logs the handle, and never reuses it; capacity 0
means the daemon stops polling and exits non-zero after logging. Startup banner
and README both say `sandbox: docker-rootless (container isolation; microvm
backend pending)`.
*Cite: CI-SANDBOX-1-B and its five constraints; R3.1-A as the P2 gate.*

**D11 — `%set-ci-protected` requires an existing tip.** `%urgit`'s existing
`[%x %repository @ ~]` peek (9119) is gated on `public-read`, so it cannot
serve a private repo. `%urgit` gains a third `our`-only peek
`[%x %ci-ref @ @ ~]` → `(unit [tip=oid:git linked=?])` (repo, ref) beside
D2's pair; `linked` is whether the repository is bound to a Clay desk.
`%urgit-ci` scries it before adding to `ci-protected`; absent → refuse with
`'ref has no tip; push a commit before CI-protecting it'`; `linked=%.y` →
refuse with the CI-LINKED-DESK-P1 reason (D14). Un-protect never checks. **D2 + D11 together are the three `our`-only peeks on `%urgit`; §6 lists
the rest of the touch (D9, D14, D16).**
*Cite: CI-EMPTY-REF-1-A.*

**D12 — State: `state-0` grows in place; no `state-1`.** AGENTS.md's state-0
rule holds for `%urgit-ci` (greenfield, P0 said so at `sur/ci.hoon:4–5`).
Growth: `candidate` gains `plan=(unit (list job))` and `verdict-reason=(unit @t)`;
`daemon` gains D6's three fields; `assignment` gains `kind=?(%plan %job)`,
`workflow=(unit @t)`, `job=(unit @t)`; `attempt` gains `kind`, `workflow`, `job`,
`reason=(unit @t)`; new `+$  job  [id=@t workflow=@t stage=@ud needs=(list @t)
cond=(unit cond)]` with `+$  cond  $%([%output-eq job=@t output=@t literal=@t] [%unsupported raw=@t])` (versioned at the wire, `v:1`, by the daemon; the ship's mold is the v1 shape); `attempt-status` gains `%skipped`; `action` gains
`[%abandon …]`'s ship-side twin if you need one, and `%assign` takes `kind`/`job`.
`on-load` still nukes-and-revives cleanly (`|nuke %urgit-ci` then
`|revive` on the harness ship is the test). **`%urgit`'s state is untouched.**

**D13 — Deadlines per kind.** Plan attempts default `~m5`; job attempts keep
`~h1`. Both remain overridable through `%assign`.

**D14 — Landing is `%urgit`'s act, automatic, atomic.** Spec §Protected refs:
"A CI-protected branch advances in one of two ways. Either the new tip is a
tested integration candidate, or an override role records an explicit
override." P0 landed passed candidates with a SECOND client push
(`.scratch/ci-p0/h8.sh:17`); that was a harness convenience, not the
contract, and §1's "no dojo line between push and landing" cannot hold
without a landing act. So `%urgit`'s `%ci-action` poke case (2248) gains a
second variant beside `%materialize-candidate`:
`[%land-candidate repo=@t ref=@t candidate=oid:git expected=oid:git]`.
Inside that ONE Gall event `%urgit`: re-scries `%urgit-ci` eligibility for
`[repo ref candidate]` (the D2 P0 peek, under the `%gu` guard); reads the
ref's current tip and refuses unless it equals `expected`; then lands
through the receive path's two existing arms for a PLAIN repository:
`apply-receive` (7850 — compares each command's old tip and produces the
updated repository value) followed by `accept-receive` (4829 — persists it,
dispatches webhooks). Build a one-command receive command list
`[old=`expected new=`candidate ref]` and pass it through both; do not copy
either arm. The existing `%set-ref` (3955) is NOT the landing path: it
checks object existence only. On success `%urgit` pokes `%urgit-ci`
`[%landed candidate-id]`; on refusal `[%land-refused candidate-id reason=@t]`.
`%urgit-ci` pokes `%land-candidate` the moment a candidate reaches `%passed`,
with `expected = base` of the candidate. A stale `expected` (the ref moved
since staging) leaves the candidate `%passed` and unlanded with
`verdict-reason` `'destination moved; rebase and push again'` — row P17.

**CI-LINKED-DESK-P1 (ratified): a repository bound to a Clay desk cannot
be CI-protected in P1.** The receive path for a linked-desk repo does NOT
write the ref in `handle-receive-pack`: it computes a Clay delta inline
(8635→), parks the push in `pending-clay`, and the ref write happens later
in `on-arvo`'s `/clay-report` case (9707; write at 9726) with no re-check.
That asynchronous completion is not one Gall event, and a landing that
re-checks eligibility there is a refactor of the receive tail that P1 does
not do. So D11's precondition grows a second clause: `%set-ci-protected`
also scries `[%x %ci-ref-linked @ @ ~]` → `?` (fold it into the `ci-ref`
peek: return `(unit [tip=oid:git linked=?])` instead of a bare `(unit
oid:git)` — still three peeks) and refuses a linked repo with
`'CI protection is not available for desk-linked repositories in this
release'`. `land-candidate` asserts the same (a repo bound AFTER protection
is refused at land time with the same reason and the candidate stays
`%passed` unlanded; binding a CI-protected repo to a desk is not blocked in
P1 — say so in the README). Row P19 covers the refusal with a mutant. The
linked-desk landing path is P2's second item; astra's four-step plan
(shared helper, `clay-push` CI reply target, re-check in the completion
event, three rows) is recorded as its derivation.

**This is the fourth `%urgit` touch** (three peeks, one filter, ref
set/clear, and this arm) and the LAST; the arm is named `land-candidate`,
sits beside `materialize-candidate` (7956), and is short because it calls
`apply-receive` + `accept-receive` rather than reimplementing them. If
`accept-receive`'s Eyre-response argument cannot be satisfied without an
`eyre-id`, give it a `(unit @ta)` and answer nothing when `~` — that
signature change is inside the fence as part of this arm.
*Cite: spec §Protected refs ¶1–2; R4.1-A; R4.3-A (a direct push is never
applied — landing is the ship's act, not the client's).*

**D15 — CI-BASELINE-P1 (ratified exception): in P1 the candidate's own
workflow files are the required evidence.** Spec §Protected refs ¶4 and
R4.2-A say required checks come from an approved workflow revision recorded
in branch policy, and a candidate that edits a workflow gets only a trial
run. That mechanism (approved revision, trial/required distinction,
promotion) is step 5 trust work and is NOT built in P1. P1 plans from the
candidate OID (D1/D2) and its jobs' results ARE the candidate's verdict.
What P1 must do so step 5 can close the gap: the stored `plan` records the
OID the workflow files were read from (it is the candidate OID; store it
explicitly as `plan-oid`), so a later baseline can pin an OID to policy and
compare. The README's limitations list states this exception in one
sentence. A chair that reads R4.2-A as binding in P1 is reading the
superseding ruling wrong — the ruling is planted in
`REFERENCE-CI-RULINGS.md` as CI-BASELINE-P1.
*Cite: CI-BASELINE-P1; R4.2-A (deferred to step 5, not overturned).*

**D16 — CI-TRUST-P1 (ratified definition): in P1 every staged candidate is
`%trusted`, because `can-write` is the admission rule.** Spec §Trust ¶1 and
R3.2-A require approval per untrusted revision. In P1 the only path to the
CI gate is a Smart HTTP push that passed `can-write`
(`desk/lib/git-access.hoon:74`: owner, a listed writer, or a `%write` group
member); there are no pull requests, forks, or anonymous contributors in the
write path, so an untrusted revision cannot reach `%stage-candidate`.
`%trusted` in P0's `%assign` (`urgit-ci.hoon:318–323`) is therefore correct
by construction, not by omission. So step 5 can classify later, the stage
poke gains the pusher: `[%stage-candidate … actor=@p]` where `actor` is the
authenticated session ship or, for a write-token push, the repository owner
(a token is the owner's delegation; record `via=%token`). `candidate` gains
`actor=@p` and `via=?(%session %token)`. Unauthenticated pushes never reach
the gate today (`can-write` refuses first) — that is P18's negative row.
The README's limitations list states that PR/fork trust classification and
approval arrive with step 5.
*Cite: CI-TRUST-P1; R3.2-A (its default holds — no untrusted revision runs;
none can exist yet); CI-SANDBOX-1-B "no untrusted code runs in P1 without a
human click" — satisfied because none is admitted.*

## §4 — Stages (each builds; each has its gate)

S1. `sur/ci.hoon` D12 molds; `mar/ci-action.hoon` follows. `zig build`.
S2. `%urgit`: the three `our`-only peeks (D2, D11) + the `refs/ci/` filter (D9)
    + `materialize-candidate` sets/clears the ref (D9) + the `land-candidate`
    arm and `%land-candidate` poke case (D14) + `actor`/`via` on the stage
    poke (D16). **Nothing else in `urgit.hoon` changes.** `+urgit!git-migration-vector` and
    `+urgit!git-access-vector` still pass.
S3. `%urgit-ci`: D11 precondition; D5 auto-materialize; D4 auto-plan-assign on
    ready; the `/plan` and `/abandon` routes; D1 validation; D3 evaluator as a
    pure arm in `lib/ci-plan.hoon` with `+urgit!ci-plan-vector` covering: linear
    chain, fan-out, `if` true/false/unset-output, an unsupported `if`, a
    duplicate job id, a `needs` on a missing job — every negative asserts the
    exact reason text; D4 scheduling + verdict; D6 selection; D13 deadlines.
    `zig build`, install on your ship, `|nuke`/`|revive` round-trips.
S4. `runner/`: D7 + D10 + D8. `go build ./...` produces one static binary
    (`CGO_ENABLED=0`). `go test ./...` covers: the `act -l` table parser on the
    planted ERPit output; the YAML `needs`/`if` walk on both ERPit workflows;
    the event relay against a recorded `--json` stream (the spike log); the
    sandbox interface with a fake backend proving the daemon never reads the
    sandbox filesystem after `Run`. `zig build` gains an optional `-Drunner`
    step that runs `go build` when Go is present and says so when it is not.
S5. Harness (§5) and the live table. S6. `runner/README.md` (install on any
    Linux host; the four config keys the spec names; the sandbox disclosure;
    the `%storage` prerequisite) written to the register of `README.md` at
    the repo root — ste-writing rules: no semicolons, no contractions, no
    passive with a known actor, ≤25 words per sentence.

## §5 — Harness: the P0 rows, then the product rows (binding)

Boot exactly as `.scratch/ci-p0/boot.sh` does on the ship in your footer. The
P0 rows H1–H15 and the mutant phase **must still pass on your tree** — run
`.scratch/ci-p0/battery.sh` first and paste its tail; a P0 row that goes red
is a P1 regression you fix before anything else.

Then the product table, with real act and the real daemon binary — no shell
relay, no dojo poke between push and verdict:

| Row | Expect |
|---|---|
| P1 | `%set-ci-protected` on a ref with no tip → refused `'ref has no tip; …'`; same poke after the seed push → accepted |
| P2 | daemon starts with `enroll_token` in config → enrolls, writes state file 0600, banner shows `sandbox: docker-rootless (…pending)`; restart with state file → no re-enroll, polls |
| P3 | push to CI-protected `master` → staged; **within 10 s, with no poke,** candidate materialized, `refs/ci/candidate/<id>` set, plan assignment delivered to the daemon |
| P4 | daemon runs `act -l`, POSTs plan; ship stores `plan` = the jobs of `fixture-pass.yml` + a two-job `fixture-chain.yml` you add to `desk/tests/ci/` (`a` emits `go=true`; `b` `needs: a`, `if: needs.a.outputs.go == 'true'`) |
| P5 | job `a` assigned, run, passed; job `b` assigned ONLY after `a` closes; `b` passed; candidate `%passed`; **the ship lands the ref itself** (D14) — `master` = candidate OID with no second push; the original push's `ng` was the last client act |
| P6 | `fixture-chain-off.yml` (`a` emits `go=false`) → `b` `%skipped` with reason; candidate `%passed`; ship lands it |
| P7 | `fixture-fail.yml` → job `%failed`, candidate `%failed`, push refused |
| P8 | a workflow with `matrix:` → `%plan-invalid` reason `matrix unsupported in P1`; candidate `%failed` |
| P9 | a workflow with `if: github.event_name == 'push'` → `%plan-invalid` with the expression quoted |
| P10 | kill act mid-job (SIGKILL the act process inside the sandbox) → daemon POSTs `abandon` → attempt `%infrastructure-error` with reason within 5 s, no deadline wait; candidate `%unknown`; push refused |
| P11 | kill the DAEMON mid-job → no abandon; attempt closes `%infrastructure-error` at the deadline (`~s20` via `%assign` override) |
| P12 | `Destroy` made to fail (rename `docker` on PATH mid-teardown, or a fake backend) → slot quarantined, capacity decremented, logged; next assignment still runs on the remaining slot |
| P13 | two daemons enrolled, one at capacity → the assignment goes to the other; `~m5` stale daemon never selected |
| P14 | `|nuke %urgit-ci` then `|revive` mid-candidate: state-0 is wiped, so the daemon's bearer hash is gone; the daemon's next poll answers `401`, the daemon logs `enrollment lost; re-enroll with a fresh token` and exits non-zero. Record exactly what the operator sees on both sides. This is the P1 behaviour by construction (greenfield state-0, no migration); a survivable nuke is not a P1 goal |
| P15 | **ERPit for real:** clone ERPit at its current master into a fixture repo on your ship, CI-protect `master`, push a commit → plan = `suite.yml` (`plan`, `structural`, `suite`) + `fixtures.yml` (`pins`, `plan`, `replay`, `erasure`, `duo`); `suite` and `replay`/`erasure`/`duo` gated on their `plan` outputs; every job runs under the daemon; **all eight green; candidate `%passed`.** This is the row the maintainer will reproduce. Budget ~25 min of act time; three fake ships boot inside the sandbox. |
| P16 | `refs/ci/candidate/*` absent from the repository API's ref list; absent after the candidate closes |
| P17 | stale destination: stage candidate X on `master`, land an unrelated candidate Y first (or `%set-ref` master by hand as the operator override) → X passes CI, `%land-candidate` refused, X stays `%passed` with `verdict-reason` `'destination moved; rebase and push again'`; `master` = Y |
| P18 | a push with no credentials and a push with a wrong write token → refused by `can-write` before the CI gate; no candidate staged; `%urgit-ci` state unchanged (mutant: skip `can-write` → a candidate IS staged from an anonymous push — RED) |
| P19 | bind a second fixture repo to a desk (`%bind-desk`, `sur/git.hoon:454`), then `%set-ci-protected` on it → refused `'CI protection is not available for desk-linked repositories in this release'`; then CI-protect a plain repo, bind it AFTER, push → staged, passes, `%land-candidate` refused with the same reason, candidate `%passed` unlanded (mutant: `linked` check → `%.n` — protection accepted and a Clay push parks in `pending-clay` — RED) |

Negative rows (P1, P7–P12, P14, P17, P18, P19) get the P0 mutant treatment: one-line
sabotage per row in `mutants.sh`, RED then GREEN, both pasted. The P0 rule
holds: **anything you type that a script did not is a Deviations entry and a
script fix.**

Foreground: every existing `+urgit!*-vector` plus `ci-plan-vector`;
`cd fe && npm test` (untouched; proves it); `go test ./...`.

## §6 — Fence and boxes

`%urgit` (`desk/app/urgit.hoon`): three `our`-only peeks (D2 ×2, D11), the
`refs/ci/` filter, `materialize-candidate` setting/clearing the ref (D9), the
`land-candidate` arm + `%land-candidate` case (D14), and the `actor`/`via`
fields on the `%stage-candidate` poke it already sends (D16). **Nothing
else.** `desk/sur/git.hoon`, `desk/mar/git-action.hoon`, `fe/`:
untouched. No approvals, no credential store, no signing key, no store upload,
no web UI, no `microvm` implementation, no `state-1`, no linked-desk
landing (CI-LINKED-DESK-P1 refuses it). No changes to ERPit. **`handle-receive-pack`'s
tail (8635→), `pending-clay`, `clay-push`, and the `/clay-report` case are
untouched.**

If the spec and a derivation disagree, or a derivation is unbuildable, write
`QUESTIONS-CI-P1.md` with `## §<n>` headings — what you read, what you tried,
what you would do under each answer — commit it, and stop. Do not guess past a
box. A box before S1 is a brief defect and costs you nothing.

## §7 — Record and commits

One commit per stage, `ci-p1: S<n> — …`. **This checkout's `.git/info/exclude`
ignores `.scratch/`; the P0 harness is tracked because it was added with
`git add -f`. Do the same for every file you commit under `.scratch/`
(`.scratch/ci-p1/*.sh`, `.scratch/p1-live-table.md`); `.scratch/tmp/` stays
ignored.** Report at
`.scratch/p1-live-table.md` (committed): the P0 battery tail, the P1 table with
observed columns verbatim, the mutant pairs, foreground tails, `go test`
output, ERPit P15 timing, a Deviations section listing every derivation you
built as written and every one you departed from with the citation that made
you, and the shutdown by `/proc/<pid>/cmdline`-verified PID. Ship down, pier
retained, sandbox daemon left running, no other pier touched. Do not merge, do
not push, do not rebase.

## §8 — Harness resources (fixed per chair; do not improvise)

Tooling: `/var/home/michael/workspace/urbit/bin/urbit` (4.6), pill
`/var/home/michael/workspace/urbit/pills/brass-408k-1.pill`, `zig` 0.15.2,
`act` 0.2.89, `go` via `brew install go` (first command). Boot in a herdr pane
you create and close by parsed id, after the shell prompt appears — the P0
`boot.sh` does exactly this; reuse it with your footer's values. **Do NOT boot
`~zod ~nec ~bud ~wes ~bel ~bus ~syt ~dur ~wep ~ser ~ryx ~wyd ~tem ~mul ~dev
~sev ~lyt`** — other work owns them. Fake galaxies never take `-p`.

**Rootless Docker is per-chair too.** The host's `docker` context is rootful.
Run `dockerd-rootless.sh` yourself with a data root and a socket under your
worktree's `.scratch/tmp/` (astra's P0 record at `urgit-ci-p0-astra/.scratch/
p0-live-table.md:225–229` is the recipe; the state dir under
`/run/user/1000/<short-name>/` keeps the Unix path short), verify
`docker --host unix://<your sock> info --format '{{.SecurityOptions}}'` shows
`name=rootless`, and copy `catthehacker/ubuntu:act-latest` into it
(`docker save | docker --host … load`). The daemon's `docker_host` config
points at THAT socket. Never reconfigure the host daemon; never share a socket
between chairs.

| Chair | Galaxy | HTTP | Pier | Rootless sock dir |
|---|---|---|---|---|
| opus | `~ryp` | 8346 | `/var/home/michael/piers/urgit-ci-p1-ryp` | `/run/user/1000/ci-p1-opus/` |
| astra | `~syx` | 8347 | `/var/home/michael/piers/urgit-ci-p1-syx` | `/run/user/1000/ci-p1-astra/` |

Reports: `.scratch/p1-live-table.md` (committed). Logs, act streams, sandbox
state: `.scratch/tmp/` (ignored). Piers stay on disk at the end.

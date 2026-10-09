# urgit CI — retained capacity from the launcher to the daemon's state file, and the bounded host adapter

This is the model source stage 01 implements on top of the accepted launcher
core (`LIFECYCLE.md`). That document stays the model of the core's durable
records, quarantine authority (R1–R8) and terminal ownership; this one covers
what the core hands to its callers and to its host: who owns a reservation
once `Prepare` fails, how the daemon counts and persists what it may not
reuse, who may release it, and how every host command and filesystem
operation the root adapter performs is bounded, owned and reported.

**What the stage's tests establish, and what they do not.** They run as an
ordinary user on fresh private fixtures: the real launcher core and wire
server on a private unix socket, the real `Microvm` and `Docker` backends
(the latter over a nonexecuting command recorder), the real daemon, the real
state file, the real host adapter over nonexecuting command recorders and a
private cgroup-filesystem model, and — for pipe, exit, signal and
process-group behaviour only — inert re-executed children of the test binary
itself. They establish the ownership, accounting, bounding and reporting
rules below as source behaviour. They do **not** establish real cgroup v2
delegation or jailer behaviour, real `ip`/`nft` semantics or network
isolation, SELinux, installation, systemd's handling of the unit, guest
execution, a real filesystem's publication times, power-loss durability, or
any recovery on a real host. §10 lists those next gates.

## 1. Identities

| Thing | Identity | Where |
|---|---|---|
| attempt | the ship's attempt id `A` | `ship.Assignment.Attempt` |
| sandbox (both backends) | handle `ci-<A>` — the daemon's name and the operator's CLI name | `sandbox.Handle.ID` |
| microvm reservation | launcher record `V = IDFor(prefix, A)` **and its incarnation token** (§11.1; a record written before tokens: its `(cid, created)`); owner `(uid, daemon id)` | `Handle.VM`, `Handle.Incarnation` (+ `CID`, `Created`); `launcher.Record`, `launcher.Ref` |
| docker-rootless sandbox | network `ci-<A>`, volumes `ci-<A>-work` and `ci-<A>-work-tools`, container `ci-<A>`, labels `urgit-ci=1`, `urgit-ci-daemon=<daemon id>` | `Handle.Network/Volume/Container` |
| daemon retention | one `state.Quarantine`: handle, backend, attempt, and the backend identity above | `internal/state` |
| reserve request | a request token `R` (32 lowercase hex, random) the daemon chooses for one reserve and records before sending it, bound to its owner and attempt; the launcher admits it at most once, ever (§11.10) | `sandbox.Spec.Request`/`Handle.Request`; `state.Quarantine.Request` (an unsettled admission); `launcher.Record.Request`; the launcher's `requests/<R>.json` |

A launcher id names every incarnation of one attempt's reservation; the
incarnation token tells them apart (§11.1: `(cid, created)` repeats across an
empty restart, so it is no identity). The launcher holds at most one record per id at a
time (`Service.vms`), so a record with the same id and another incarnation
proves the earlier incarnation was released.

Retention identity (`Quarantine.Same`): two retentions are one when their
backend, handle and — for microvm — record and incarnation agree. A retention
written before this stage (no backend) or one whose launcher identity was
never learnt (`vm` empty: the reservation's outcome was unknown) matches every
retention of the same handle and is completed in place, never counted twice:
the launcher cannot hold two records for one attempt at once, and a Docker
handle names the same objects every time. Since settled-admission ruling 01,
an unsettled admission — a retention that names a reserve request and no
record — is its request and nothing else: never the same as another
request's admission of its handle, nor as a retention of the reservation it
may have made, which follows its own disposition (§11.10).

## 2. Authority

| Action | Who | Where |
|---|---|---|
| create a launcher record | the owner's `reserve` | `Service.Reserve` |
| admit a reserve request | the launcher, at most once per request token, ever: refused once it was admitted, settled or is being settled (§11.10) | `Service.admitLocked` (`admissibleLocked`), the request ledger |
| settle a reserve request | its owner, for its attempt: the launcher answers admitted (the reservation, held), released (on its evidence) or closed (never admitted, closed durably before the answer; or admitted and never acknowledged); a refusal settles nothing (§11.10) | `Service.Settle`; the wire's `settle`; `Microvm.Settle` |
| release a launcher record | a successful teardown that began before its obligation's deadline: owner `destroy`, create rollback, the deadline reaper, shutdown | `Service.teardown` |
| retry the cleanup of a quarantined launcher record (never releases) | the operator (`urgit-vm-launcher recover`, root, `serve` stopped); automatically: a recovery effort after a missed obligation, the owed cleanup of an interrupted teardown | `Service.RetryCleanup`, `Service.teardown` (`retaining`) — §8 |
| release a **quarantined** launcher record | only the operator's explicit release of the exact inspected incarnation and revision, once nothing is held (root, `serve` stopped); it performs no cleanup | `Service.Release` — §8 |
| destroy, create, connect or stop a named incarnation | its owner only, naming the incarnation (its token, §11.1); another incarnation of the id is never touched, and a request naming none is refused | `Service.DestroyOf`/`CreateOf`/`ConnectOf`/`StopOf` with a `launcher.Ref`; the wire's `incarnation` |
| record a daemon retention | the daemon, for any sandbox whose release it cannot prove | `Daemon.retain` |
| retry the cleanup of a docker-rootless retention (never releases) | the operator (`urgit-runner -recover`, the runner's user, the daemon stopped) | `cmd/urgit-runner` — §8.4 |
| release a daemon retention | only the operator's explicit release of the exact selected retention (`urgit-runner -recover`, the runner's user, the daemon stopped), once proven released: a microvm one by the launcher's release, an unsettled admission by its request's settlement, a Docker one by the absence of its objects | `cmd/urgit-runner` — §8.4 |
| prove a microvm retention released | that release, asking the launcher at release time: its durable evidence of the exact incarnation's release (§11.8), never its absence from the list; for an unsettled admission, the settlement of its request, closed or released (§11.10). *(Corrected for settled-admission ruling 01: this row still named the list, which late-accounting ruling 01 had replaced by the evidence.)* | `recovery.inspect` over `launcherReleased` (`launcher.Client.Released`); `recovery.settleAdmission` over `launcherSettle` (`launcher.Client.Settle`) |
| prove a Docker retention released | that release: a look at release time finds none of its objects (only Docker's exact not-found reply about that very object proves it absent, §11.4; a failed look proves nothing) | `recovery.inspect` over `Docker.Leftovers` |
| end a held orphan's withholding | the backend no longer listing it (the launcher released it), or a reconcile pass destroying it | `Daemon.Reconcile` |

Neither side releases on the other's word alone. The daemon never clears a
launcher quarantine (it holds no such authority: its socket peer has no retry
or release operation, which exist only as the root CLI), and the launcher
never clears a daemon retention. The one cross-check runs in the safe
direction: the daemon-side release requires the launcher's proof — its
evidence of a release, or its settlement of a request — and it cannot make
the launcher release anything. A Docker retention is released
only once an inspection at release time finds none of its objects (recovery
ruling A; P3 took the operator's word).

The retry and the release on both sides need their daemon stopped: each takes
an exclusive lock the running process holds (inspection does not: it reads).
That limitation is kept and stated, not worked around; no live administration
protocol is added (QUESTIONS-SOURCE-01 §1: an operational disruption,
unratified).

## 3. Failure and uncertainty through `Prepare` (microvm)

Every reserve names a request token the daemon recorded durably before
sending it (§11.10). `Microvm.Prepare` runs one private launcher connection per attempt (no shared
connection, so one attempt's long `create` never delays another's
`destroy`). Every call is bounded by the attempt's context (`Client.Bind`):
a call that does not answer in time is abandoned, its connection closed, and
its outcome treated as unknown.

| Step fails | What the launcher holds | `Prepare` answers |
|---|---|---|
| dial / hello | nothing of this attempt | plain error |
| `reserve` refused (budget, invalid, exists, fenced, expired, closed, settled) | nothing new by this delivery — but a refusal is kept nowhere, so a replayed or delayed copy of the request could still be admitted | its request is settled first (§11.10): closed → the refusal, verbatim, a plain error; a copy admitted meanwhile → rolled back by its incarnation; no conclusive answer → retained by its request |
| `reserve` not acknowledged but kept charged (R8: its record may be durable and could not be withdrawn) | an unacknowledged `preparing` record; the reply names it (`retained`, `id`, `incarnation`, `cid`, `created`) | rollback of that exact incarnation |
| `reserve` transport failure (no answer) | unknown | a fresh connection settles exactly its request (§11.10): admitted → rolled back by its incarnation; released or closed → a plain error, nothing left; no conclusive answer → retained by its request (`vm` empty, `request` set: an unsettled admission). Never a list: before settled-admission ruling 01, the owner's list decided, and an empty one was taken for nothing reserved |
| `create`, the helper connection, `READY` | whatever the rollback leaves | rollback (`destroy` of the exact incarnation on a fresh connection) |
| rollback `destroy` succeeds (or finds the incarnation gone) | nothing | plain error, slot free |
| rollback `destroy` fails, is refused quarantined, halts, is unconfirmed, or cannot be answered | the record, charged | `*sandbox.RetainedError` carrying the exact handle (`VM`, `CID`, `Created`, `Attempt`) — never a Docker-shaped or empty handle |

`Docker.Prepare` answers the same way for its own objects: a failed cleanup
after a partial create returns `*sandbox.RetainedError` with the
Docker-shaped handle, where it used to discard the cleanup's error and keep
the slot.

## 4. Daemon capacity

`C` is the configured capacity. The daemon keeps three sets:

* **retained** `R` — durable in the state file; each withholds one slot
  until the operator's release (§8.4);
* **held** `H` — backend records of this daemon from a previous life that are
  neither retained nor in flight: an attempt the ship still calls running,
  one whose status could not be read, or one the ship names another daemon's
  while the launcher charges it to this one; in memory, re-derived from the
  backend at every start and reconcile pass (Run repeats it);
* **in flight** `F` — attempts this process is running.

Since settled-admission ruling 01 (§11.10), `R` also holds the **unsettled
admissions**: each reserve request, recorded before it is sent, until the
launcher settles it. An admission whose Prepare still runs in this process is
that attempt's slot in `F`, counted once; from its Prepare's end it withholds
a slot of its own until settled. Its settlement — at once, or at a reconcile
— returns the slot by itself, so an admission is not a retention only the
operator releases.

Advertised capacity (`x-ci-capacity`, the banner) is `C − |R|`: the ship
already counts the attempts it believes run here, held orphans included, and
does not know about retentions. Local admission waits until
`C − |R| − |H| − |F| > 0`. `ExitNoCapacity` follows only from `C − |R| ≤ 0`.
`ExitNoCapacity` counts only the retentions the operator releases: every
slot withheld by admissions keeps the daemon running, and reconciling, until
they are settled (§11.10). No overcommit: a local slot is taken only when none of the three occupies
it. No double charge: an identity is retained once, and a held orphan whose
later destroy fails moves from `H` to `R` — counted once. Whether the ship's
own accounting agrees for an orphan it does not count here (wrong owner,
given up) is not proven by local withholding: such an assignment queues for
a local slot. That trade is QUESTIONS-SOURCE-01 §4, **unratified**; no
controller change is made.

| Event | Effect |
|---|---|
| `Prepare` → `RetainedError` | retain its handle (durable); the attempt's slot passes to the retention |
| post-run `Destroy` fails | retain the handle; same |
| a retention already recorded for that identity | logged, not counted again; the attempt's slot is freed |
| start: state file loaded | `R` = its entries, deduplicated by identity (duplicates in older files collapse) |
| reconcile: an orphan already retained | nothing (no second `destroy`, no second charge) |
| reconcile: terminal/unknown attempt, `destroy` succeeds | nothing held |
| reconcile: `destroy` fails | retained |
| reconcile: running on the ship, status unreadable (ship unreachable or an error answer), or another daemon's per the ship while the launcher charges this one | held (a repeated pass holds it once; advertised unchanged) |
| reconcile: a held orphan the ship later calls terminal | destroyed: its slot freed; or, the destroy failing, retained — moved from held, counted once |
| a later pass no longer finds a held orphan | its slot is freed |
| a reserve request recorded (§11.10) | an admission: withheld from its Prepare's end, unless settled by then; not durable → no reserve is sent |
| its answer lost or refused, settled closed or released | the admission removed; its slot returns once that is saved |
| settled admitted | its reservation held in the admission's place; the orphan logic resolves it |
| no conclusive answer | the admission stays, withholding its slot; every reconcile settles it again |

Evidence: `internal/daemon/accounting_test.go` `TestHeldOrphansAcrossTheShipsAnswers`
(each ship answer, reconciled twice; later terminal, both ways), `matrix_test.go`,
`retained_test.go`; controls G01–G03, G06, G07, G10–G14.

## 5. The daemon's state file

`state.Save` publishes by write-temp, fsync, rename, fsync-directory and
reports the failing step and whether the rename may have happened. The
daemon takes `state_file.lock` (`flock`, exclusive, non-blocking) for its
whole life; the operator's retry and release (`-recover`) take the same
lock, so the two never rewrite each other's file.

A retention is recorded in memory first — capacity is withheld from that
instant — then saved. If the save fails:

* the retention stays withheld in memory;
* the daemon is **not durable**: every loop retries the save and logs
  `STATE NOT DURABLE`, and each assignment offered meanwhile is abandoned
  with that reason — no new work starts on accounting a crash could lose;
  an assignment that was waiting for a slot faces the same rule once it has
  one, before any reservation (the post-acquire gate); an attempt already
  running goes on to its end;
* any save error counts, whatever the file may hold now: a save whose rename
  happened and whose directory fsync then failed is proven by the next
  successful save only;
* a crash before a successful save loses only the file entry, not the
  resource: the backend still lists it, and the next start's reconcile
  retains it again, durably, once.

Evidence: `TestUnsavedRetentionStopsNewWorkUntilDurable`,
`TestEffectThenErrorSaveIsNotTrusted`, `TestUnsavedRetentionComesBackAfterACrash`,
`TestQueuedAssignmentFacesTheDurabilityRule`; controls G04, G05, G08, G09,
G15. The middle course — heartbeat and running work kept, new work
abandoned — is QUESTIONS-SOURCE-01 §5, **unratified**.

The operator's release and retry report their outcome only after their own
save is durable; an uncertain save is reported as such (exit status 2), with
the file possibly holding either version. A released retention moves to the
file's `released` list, kept as evidence.

## 6. Lifetimes and stale callers

* **Attempt**: claim → slot → `Prepare` (retained or not) → run → `Destroy`
  (released or retained) → the slot is freed or passes to the retention.
* **Daemon restart**: lock, load `R` (its unsettled admissions withheld from
  the start), reconcile — settling each admission first (§11.10) — the
  backend's records against `R`, then run. Repeating this any number of times charges each retention
  once.
* **Launcher restart**: no daemon connection outlives an operation, so the
  next call reaches the new process; a call cut by the restart has an unknown
  outcome and is resolved by the exact-incarnation rollback or a retention;
  a reserve cut so is settled by its request (§11.10).
* **Delayed stale caller**: every daemon-side `create`, `connect`, `stop` and
  `destroy` names the incarnation it means, by its token (§11.1), which no
  restart, clock or cursor repeats. The launcher answers `destroy` of a
  released incarnation as done (idempotent), refuses the others (`ErrStale`),
  and never acts on another incarnation of the same id; a request naming no
  incarnation is refused.

## 7. The host adapter (`cmd/urgit-vm-launcher`)

### 7.1 Commands and processes

Every host command runs through `realHost.command(ctx, owned, name, args…)`
(`runCommand` in production):

* **owned process group** (every command but the jailer): the command leads a
  new process group. On its context's end the whole group is killed while the
  leader is still unreaped (so the group id cannot have been reused); after a
  normal exit, any group member still alive is killed before the leader is
  reaped. The call returns within `killGrace` after the deadline even if a
  process ignores `SIGKILL` (uninterruptible sleep): it then reports the pid
  as possibly still acting, and a reaper goroutine collects it later.
* **the jailer** is not a group leader: `--daemonize` calls `setsid()` in the
  jailer itself, which a group leader cannot do. On its context's end the
  jailer is killed by its own pid (Go's pidfd) and by the group it may have
  created by `setsid`; after a normal exit nothing is signalled, because that
  group is the VMM's.
* **inherited pipes**: `WaitDelay` bounds the wait for output a descendant
  keeps open; such an exit is reported as possibly acted.
* **outcome**: a command that could not be started acted not at all
  (`started == false`: the step may wrap `ErrNoEffect`); every other failure
  — non-zero exit, timeout, kill, held pipe — is "may have acted".

### 7.2 Deadlines: the cleanup obligation runs from its trigger

Rider 04 (line 14): within `CleanupBound` (B = 120 s) after exit,
cancellation or job timeout, every owned resource is stopped and cleaned, or
explicitly quarantined with its capacity withheld and the failure visible.
The core keeps **one deadline per obligation**, `D = T + B`, where `T` is the
earliest relevant trigger, decided in one place (`Service.obligationLocked`)
and never restarted at a teardown's entry or per step:

| Trigger | `T` | Noticed by |
|---|---|---|
| the owner's destroy | its request (read before any wait) | `Destroy`/`DestroyOf` |
| the job's deadline | the reservation's `deadline_unix` — a trigger of every releasing teardown, whoever notices it | the reaper's pass; the create's own steps (their context ends at it) and start (cut at `T + B/4`); an owner's destroy or the stop asked for after it |
| the launcher's stop | `BeginStop`'s call (else `Shutdown`'s, when alone) | `serve`, on SIGTERM/SIGINT |
| a create's failure | the failure, or the request that asked for the rollback | `Create` |
| a halted teardown | the deadline it began with (`entry.owed`), for its retry | the owner's retry, the reaper |
| discovery at restart | the recovery pass (for an interrupted create, its deadline if earlier; an interrupted teardown is quarantined already, its owed cleanup one bounded effort) | `Reap`'s first pass, at once |

The operator's cleanup retry of an incident (§8.2) is no trigger: it is
one bounded effort from its own request, and the incident keeps the
obligation it was quarantined under — its trigger, its deadline and
whether it was missed.

`D` is carried through every hand-over:

* **a wait for an operation in progress**: a destroy waits for the token — a
  create is asked to roll back, a connect is itself bounded (5 s, and the
  job deadline) — and its teardown still ends by the request's `D`;
* **an in-flight start**: `startBound` (30 s) bounds it, and it is cut `B/4`
  after the earliest trigger of a rollback (the job's deadline sets its cut
  when the start begins; a destroy's or the stop's replaces it when
  earlier). A start cut there leaves a VMM of unknown pid: the rollback
  quarantines it ("vmm start outcome unknown") by `D`, charged and visible,
  never reported stopped;
* **a late start result**: a start that ends after its rollback was asked
  for, or at or past the job's deadline, is not published as running; its
  VMM, known now, is rolled back by `D`;
* **a create's rollback** keeps the create's token, as a teardown's, and
  runs from the trigger that asked for it, never from when the create
  noticed;
* **the reaper**: each pass's teardowns run side by side, and no pass waits
  for another's (`Reap`; `ReapOnce` waits for its own);
* **`Shutdown`**: its drain (non-teardown operations only) spends the stop's
  allowance; its teardowns run side by side, each by `D`. Alone (without
  `BeginStop`) it cuts no create short, as before;
* **a halted teardown's retry**: the same `D`.

Within a teardown, what is left of `D` when it begins is split:

| Phase | Ends at (`L` = `D` − the teardown's start) |
|---|---|
| VMM `TERM`, then waiting for it | start + L/4 |
| VMM `KILL`, then waiting for it | start + L/2 |
| removals (network, cgroup, disk, jail) and their commands | `D` |

The core passes `D` to the host through `Bounder` (`Bound(ctx) Host`); a
host without it (the test models) is called directly. A removal `D` cuts
short is a failed removal: quarantined. A VMM that survives TERM and KILL is
quarantined naming its pid, never reported stopped.

**A missed obligation is an incident (recovery ruling A).** A teardown that
begins at or after its `D` — the launcher was not running at the trigger (a
crash, then the restart's recovery pass), a halted teardown retried late, or
`Shutdown` alone whose drain outlived the allowance — is a bounded recovery
effort on a quarantined incident: one allowance from its own start, never
release, whatever the cleanup achieves. Its durable form is the `stopping`
record, which carries the obligation it began under (`cleanup_trigger`,
`cleanup_due`) and loads quarantined, then the quarantined record. The
record is quarantined, its incident records the trigger, the missed deadline
and the attempt ("late recovery"), its report and reason say `LATE`, and its
capacity stays charged until the operator's separate release (§8). Its
caller gets `ErrQuarantined`, never a release: an owner's destroy asked for
after the deadline is answered so, and its daemon keeps the slot. A
successful late cleanup never returns capacity and never makes the
obligation met. A restart's recovery of an interrupted teardown whose
`cleanup_due` has passed records it as missed too. This replaces the
stage's earlier, unratified proposal that a LATE teardown release on
success, which ruling A withdrew.

**Four limits, not one:**

| Limit | Bounds | Relation to `D` |
|---|---|---|
| the job's execution deadline (`deadline_unix`) | the attempt: no host effect begins at or after it, no connect past it, the reaper reaps it | also a trigger |
| source-command bounds (`startBound`, the pid wait, `Connect`'s 5 s, each command's own) | single host operations | never extend `D` |
| the daemon-side client waits (`rollbackBound` for a Prepare's rollback, `teardownBound` for a post-run destroy: each B from the daemon's own request) | how long the daemon waits for the launcher's answer | not an obligation, and never later than it: an answer not in by then leaves the sandbox retained, charged |
| the unit's `TimeoutStopSec=180` (`KillMode=mixed`: the stop signal goes to the launcher alone) | systemd's outer backstop | not the obligation, and no extra execution or cleanup time |

**Not bounded by `D` in source** (explicit, unqualified): the durable
publications of a teardown's outcome — the `stopping`, `quarantined` or
withdrawn record's writes, fsyncs and directory fsyncs — and kernel-side
effects past a signal. A hung filesystem can publish an outcome late. That
is a real-host qualification item (§10), not something `TimeoutStopSec`
proves, and ruling A does not decide it (QUESTIONS-SOURCE-01 §3, case 2).

`serve`'s stop, in order: the socket closes; the reaper stops starting
teardowns (one in progress keeps its `D`); `BeginStop` refuses new
operations, fixes the stop's trigger and asks every create to roll back for
it (its cancellable step cut at once, its start within `B/4`); `Shutdown`
drains what remains and tears everything down side by side by `D`.

Evidence (nonexecuting host models with barriers, measured from the trigger
to the settled outcome): `internal/launcher/trigger_test.go` — a start
pending when the owner destroys, when the job's deadline passes (with a
reaper pass or none), when the stop drains it while another VM runs, a start
that finishes late; the rollback's host calls carrying the request's
deadline; teardowns asked for after the deadline; `Shutdown` alone; a retry
after a halt; a reaped teardown late in its allowance; passes that do not
wait; a destroy that waits. `budget_test.go`, `stop_test.go`. Controls
T01–T11, N06–N15, N17. The missed obligation: `recovery_test.go`
`TestMissedObligationStaysChargedAfterLateCleanup`,
`TestCleanupAttemptsNeverRelease`; `stop_test.go`
`TestLateTeardownsAreReported`; the adapter's
`TestServeLogsALateIncident`; controls X01, X01D, X01C, X04, X38, X39. N16
and A20 guarded the late release, which no longer exists; they are retired
(`controls/new.py` `RETIRED`).

### 7.3 Ownership-scoped create and cleanup

* **Jail**: `PrepareDisk` refuses (`ErrNoEffect`) when the attempt's jail
  directory already exists; the core then records no disk holding, and a
  teardown removes the jail only when the record may hold it (`HasDisk`, a
  disk path, or a VMM).
* **cgroup**: the launcher's leaf
  `<cgroup_service>/<cgroup_parent>/<id>` holds the limits and the VMM.
  Firecracker v1.17's jailer, with `--cgroup-version 2`, `--parent-cgroup`
  set to this leaf and no `--cgroup` arguments, creates no child cgroup and
  moves the VMM into the leaf itself. `cgroupPaths` retains a legacy second
  return value `<leaf>/<id>`, unused when starting the VMM; no behaviour is
  changed by this documentation correction. `CreateCgroup` refuses a
  preexisting leaf (`ErrNoEffect`); `RemoveCgroup` refuses while any cgroup
  of the subtree has processes, then removes any children and the leaf,
  deepest first.
  C1 (P4-VM-STAGE-A-SOURCE-01): every one of them lies in the unit's own
  service cgroup, which systemd delegates to it (`Delegate=yes`;
  `cgroup_service`). `serve` places itself first (`placeCgroups`): it runs in
  `<service>/supervisor` (`DelegateSubgroup=`, or its own move), the service
  cgroup holds no process and enables cpu, memory and pids, and so does the
  jobs cgroup `<service>/<cgroup_parent>`; anything else refuses to serve.
  `check` refuses a layout outside the delegated subtree and, while the
  service runs, a service cgroup systemd does not mark delegated
  (`trusted.delegate`) or one lacking a controller; create, start and removal
  act only on a job cgroup of the delegated subtree, the recovery pass
  included.
* **network**: `CreateNetwork` first proves that none of the attempt's
  objects exists — the namespace `<id>`, the veth `vh<index>`, the chain
  `vm-<index>`, any rule naming `"vh<index>"` or the namespace address — and
  refuses with `ErrNoEffect`, issuing no mutating command, if one does. The
  per-VM chain is created with `nft create chain` (fails if it exists) and no
  `File exists` is tolerated. The host masquerade names no interface:
  `ip saddr <namespace address> oifname != "vh*" masquerade`, so it holds on
  whichever interface the routing table picks (N2, Stage B review); the
  retired `egress_interface` setting is refused. D1 (P4-VM-STAGE-A-SOURCE-01):
  traffic from a VM's veth to the host's own addresses takes the input hook,
  which the forward chain never sees. The launcher's table gets a shared
  `input` chain on that hook that drops the veth range `10.113.0.0/16`; each
  VM's veth jumps first (`insert`) to its own `in-<index>`, created new,
  holding exactly one exception per exact literal destination (`tcp`/`udp`,
  one IPv4 address, one port) and a drop. Every destination, and every
  ceiling entry in `check`, must be such an exact entry or a named one.
  Named destinations (CI-P4-NET-1, pinned names): `tcp:archive.ubuntu.com:80`
  names one DNS name in the shared grammar (`internal/netname`). The create
  resolves each granted name on the host before it changes anything, and
  refuses without effect if any answer is not a public IPv4 address (private,
  shared, loopback, link-local, documentation, multicast and reserved ranges,
  and the host's own addresses), if there is no IPv4 answer, or if there are
  more than `pindns.MaxAddrs`. Each pinned address gets a forward accept at
  the entry's proto and port; a name never gets an input-hook exception. The
  launcher then serves a responder (`internal/pindns`) on the VM's gateway
  inside the VM's namespace. It answers the granted names from the pinned
  table, refuses every other query, and forwards nothing. The guest gets
  `urgit.dns=<gateway>` on its kernel command line and writes it as its only
  nameserver; a VM with no named destination keeps an empty resolver. If a
  CDN moves a name while the job runs, connections to the new address fail;
  access never widens. A pinned address may serve other sites as well (a
  shared CDN address): the grant is the address and port, not the name. A
  launcher restart ends every responder; a VM still running then fails its
  lookups closed. The `create` answer carries what was pinned (`pinned`:
  each granted name with its addresses, in name order; absent for a VM
  granted no name), held in memory only. The runner checks that it names
  exactly the granted names, each with 1 to 64 distinct IPv4 addresses,
  and rolls the VM back otherwise (CI-P4-NET-1, pinned addresses shown per
  run). The create lists the table back
  and fails unless the containment is exactly that; `serve` refuses to start
  while a networked VM that may still run lacks it; `RemoveNetwork` stops the
  VM's responder first (its sockets would keep the namespace alive), deletes
  each rule naming the veth in the chain the listing shows it in, then
  `vm-<index>` and `in-<index>`. The host's own firewall is not touched: an
  exception only withholds the launcher's drop.
* The core honours `ErrNoEffect` from every create step (the holding is
  undone), so a refused step never leads to removing what it did not create.
* **Unrelated sentinels**: removals act only on the record's id and index:
  exact-token rule matching, the id's cgroup subtree, the id's jail
  directory.
* `listen` unlinks the socket path only when it holds a socket.

### 7.4 Visibility

`Service.SetReport` names the function told the `Outcome` of every
teardown the launcher starts on its own — the reaper's, the restart's
recovery — that did not end in a release: a halt (its `stopping`
record not durable: a known VMM stopped, nothing removed, still charged, its
obligation's deadline kept for the retry), a quarantine, or an unconfirmed
withdrawal. A late teardown is always one of them (`Outcome.Late`): under
ruling A it never releases. `serve` logs each (`TEARDOWN HALTED …`,
`QUARANTINED …` — a late one with its `LATE` reason, naming the operator's
`urgit-vm-launcher recover` — and `RELEASE NOT CONFIRMED …`); a
`Shutdown`'s failures are its returned error, which `serve` logs. A halted record's reason exists only in memory,
because its record could not be written: the log line and the owner's
`list`/`inspect` (in-memory records) show it; the read-only `list` subcommand
shows the durable record.

## 8. Incidents: cleanup and release are separate (recovery ruling A)

The user's ruling (2026-09-23, `.scratch/source-stage-01/orchestrator/recovery-ruling-01/`,
ratified policy): "I want A with usable recovery tooling: automatic cleanup
that preserves quarantine, an inspectable result, and a separate explicit
release". A quarantine is an incident; cleanup (automatic or the operator's
retry) mitigates its hazard and never releases it; release is a distinct
operator action, after resolution, on the exact incarnation the operator
inspected. This section is the source's implementation of that ruling; its
acceptance (independent review) and its real-host qualification are pending.

### 8.1 The incident (launcher record)

A record carries `incident` from the moment a teardown ends it quarantined:

| Field | Meaning |
|---|---|
| `trigger` | what asked for the cleanup (the owner's destroy, the deadline, the stop, a create's failure, an interrupted teardown) — the teardown's reason at the time |
| `due_unix` | the obligation's deadline `D` (§7.2) |
| `missed` | the cleanup began after `D`: the obligation was not met; recovery never makes it met |
| `attempts` | each cleanup attempt since, newest last (the 8 most recent kept; `count` says how many there were): when, by whom (`teardown`, `late recovery`, `recovery`, `operator retry`; in the evidence, a last `operator release`), and what it left — `resolved` (nothing held) or `unresolved` with the holdings and problems |

The incident is opened once, by the record's first quarantine, with the
obligation that teardown began under. Every later attempt only adds to its
history, and none rewrites the trigger, the deadline or `missed`. Every
text in it is bounded (`maxDetailBytes`, `maxLeftEntries`), so its growth
fits the room every admitted record keeps (`recordGrowthBytes`, 128 KiB),
and every publication of it — and its evidence — reloads. `rev` counts every
durable change of a record; a release names the revision the operator
inspected. `label` is the job it ran — the ship's repository, workflow and
job, sent by the daemon at `reserve`, bounded and printable — shown for
selection, never trusted for identity.

Evidence: `recovery_test.go` `TestInspectionShowsTheIncident`,
`TestMissedObligationStaysChargedAfterLateCleanup` (a retry after it keeps
the original obligation), `TestIncidentHistoryStaysWithinTheRecordsRoom`;
daemon `TestTheJobLabelReachesTheLauncherAndTheRetention`. Controls X03,
X04, X17, X29, X29P, X31, X40.

### 8.2 Cleanup: automatic, or the operator's retry — never a release

| Attempt | When | Bound |
|---|---|---|
| the teardown | owner destroy, deadline, stop, rollback, begun in time | its obligation's `D`; released or quarantined |
| late recovery | the same teardown, begun at or after its `D` (§7.2) | one allowance from its start; quarantined, never released |
| recovery | once at open, for a teardown found interrupted (`stopping`) | one allowance; quarantined, never released (missed when its `cleanup_due` had passed) |
| operator retry | `urgit-vm-launcher recover` → retry, on the exact incarnation, refused while any operation holds the record (it never waits: an operation in progress may end in an outcome nobody inspected, and a create is never asked to roll back) | one allowance from the request; one effort per request, no loop; quarantined, never released |

Each stops only execution whose ownership is established and removes only
what the record holds: a known pid is signalled only while the host
verifies it as the id's VMM (`Liveness` Running: `--id <id>`), and its exit
is observed before any removal; a pid whose state cannot be verified is
never signalled and never taken for gone (§11.2). With an unknown pid (a
start cut short), automatic cleanup removes nothing. The operator's retry
alone looks for it: a host that can (`VMMFinder`; the adapter scans `/proc`
and classifies every process by `Liveness`) either finds none by a complete
scan — the holding is resolved, with that finding and its time in the
attempt — or finds the id's VMM, stops it as above and then removes. An
incomplete scan (an unlistable proc root, a look cut short, a process it
cannot classify), a host that cannot look, a survivor, a failed removal:
unresolved, visible in the attempt, and nothing is removed from under a VMM
that may run (the VMMs an incomplete scan did verify are still stopped).
The record stays quarantined and charged after every attempt, whatever it
achieved.

Evidence: `TestCleanupAttemptsNeverRelease`,
`TestUnknownCustodyIsResolvedOnlyByTheRetrysScan`,
`TestUncertainStartIsQuarantinedUntilTheOperatorClears` (a found VMM that
survives stays held), `TestRecoveryLeavesOtherRecordsAlone`; the adapter's
`TestFindVMMsLooksForTheExactID` and `TestRecoverStopsAVMMOfUnknownPidItFinds`
(the real `/proc` scan finds and stops this test binary's own inert child,
and leaves an unrelated one alive). Controls Q05, Q10, V05, V17, Q11, Q11b
(re-mapped), X02, X02R, X02C, X09, X10, X11, X11S, X26, X26X; §11.2 lists
the correction's.

### 8.3 Release: explicit, exact, after resolution

`Service.Release(sel)` releases the quarantined record `sel` names — id,
incarnation token (a record written before tokens: its cid and created;
§11.1) and the revision inspected — and nothing else:

* refused if another incarnation holds the id now, if the revision changed
  since the inspection (a retry, a recovery ran meanwhile: inspect again),
  while any operation holds the record (a cleanup in progress), if the
  record is not quarantined, or while any holding remains (a VMM — known
  or of unknown pid — a disk, a jail, a cgroup, a network): release is
  never proof that something is gone, and it never cleans up;
* refused, too, while a cleanup is still publishing its outcome — the
  record quarantined again in memory at its new revision: its withdrawal
  would race that publication, which would put the file back;
* its durable transition: the incident's evidence first — the record, state
  `released`, with a final `operator release` entry, written to
  `released/<id>.<token>.json` (a record without a token:
  `<id>.<cid>.<created>.json`) through the one publication path
  (`publishAt`: write-temp, fsync, rename, fsync-dir). An existing file there
  is never overwritten: this incarnation's own evidence (a release whose
  withdrawal did not finish) is kept, and anything else refuses the
  release. Then the record is withdrawn from `attempts/` (unlink,
  fsync-dir). Reported released only then; any failure keeps the charge (an
  unlinked record whose directory fsync failed stays charged until a
  retried release — or, since late-accounting ruling 01, the reaper's next
  pass — confirms it: §11.8);
* the evidence records its disposition, *operator release*, and — once the
  withdrawal is confirmed — that confirmation's instant (§11.8);
* the evidence is kept: nothing deletes an archived incident (no retention
  policy is invented). Since late-accounting ruling 01, every release keeps
  one, a timely cleanup's too (§11.8).

Evidence: `TestReleaseNamesTheExactInspectedIncarnation`,
`TestReleaseIsRefusedWhileACleanupRuns` (while the retry removes; while it
publishes its outcome), `TestReleaseIsDurableAndKeepsItsEvidence` (each
failing step; something else at the evidence's path),
`TestIncidentSurvivesARestartAndALostAnswer`,
`TestUnknownCustodyIsResolvedOnlyByTheRetrysScan` (release is no proof).
Controls X05, X05L, X05R, X06, X07, X08, X12, X12F, X13, X14, X15, X16.

### 8.4 Operator entry points

* **Launcher (root)**: `urgit-vm-launcher recover -config <toml>` lists the
  incidents, numbered, each with its job label, attempt, owner daemon,
  exact incarnation (id and token; cid, created), whether its obligation was missed,
  what it still holds and its charge; the operator picks a number, sees the
  inspection (every attempt, holdings stopped/removed versus unresolved,
  the charge withheld, and why release is refused or allowed) and chooses
  `retry` (cleanup only) or `release` (confirmed by typing `release`).
  Scripts pass `-select <id>/<incarnation>/<cid>/<created>/<rev>` (the
  incarnation `-` for a record written before tokens) and `-action
  inspect|retry|release`: the same checks, no shortcut; an inspection of a
  selection the record has moved past says so. Listing and inspection read
  the state directory (`ReadState`, `InspectRecord`) and work while `serve`
  runs — an interrupted or in-progress teardown reads as an incident; retry
  and release open the core under its lock, so `serve` is stopped first,
  and that stop tears down every VM the launcher runs (an explicit
  limitation; no live administration protocol is added). `clear` only
  names `recover` and exits 2.
* **Runner (its user)**: `urgit-runner -config <toml> -recover` lists the
  retentions the same way (label, attempt, backend, exact identity, why,
  since). Inspection of a microvm one shows the launcher's record of the
  exact incarnation — its incident — or that the launcher no longer holds
  it (the launcher's socket, so `serve` runs); of an unsettled admission, the
  record its request admitted if the launcher's list holds one — a presence,
  never proof of an absence: its inspection settles nothing (§11.10); of a
  Docker one, which of its
  objects exist. Retry: a Docker retention's objects are removed by name
  (the runner's own; the attempt is recorded in the retention, which
  stays); a microvm retention's retry is the launcher operator's, and the
  tool says so and names it; an unsettled admission has nothing to clean
  up. Release: only once proven — the launcher's evidence of the exact
  incarnation's release (§11.8), an unsettled admission's settlement, closed
  or released (§11.10), or none of the Docker objects left at release time
  (*corrected for settled-admission ruling 01: this said the list, which
  late-accounting ruling 01 had replaced by the evidence*); an entry recorded
  without its request, never (§11.10; QUESTIONS-SOURCE-01 §11) — then the retention moves to `released` in the state file
  and is reported released only after a durable save. A selection
  (`<handle>/<backend>/<vm>/<incarnation>/<cid>/<created>/<request>/<at>/<rev>`,
  the request an admission's) names one
  retention exactly: another incarnation of the same handle is another
  selection, and a retry moves the revision. The daemon is stopped (the
  state file's lock); the slot returns at its next start. `-clear-quarantine`
  only names `-recover` and exits 2.

Evidence: the launcher's `recover_test.go`
`TestRecoverSessionListsSelectsRetriesAndReleases` (the interactive session:
selection by number, inspection, a refused release, a retry, a release
not confirmed while releasable, the confirmed release; the other incident
untouched), `TestRecoverScriptedHoldsTheSelectionExact`, and `main_test.go`
`TestClearWaitsForTheStateAndUsesTheCore` (refused beside `serve`); the
runner's `main_test.go` `TestRecoverReleasesAMicrovmRetentionOnlyOnTheLaunchersProof`,
`TestRecoverOfAnOlderEntryAsksTheLauncherByAttempt`,
`TestRecoverRetriesAndReleasesADockerRetention`,
`TestRecoverSessionSelectsByNumber`, `TestRecoverSelectsTheExactIncarnation`;
the sandbox's `TestDockerLeftoversAreExact`. Controls X05C, X06C, X08C, X18,
X18R, R01–R04 (re-mapped), X20–X24, X22I, X25, X25K, X37; §11 lists the
correction's.

### 8.5 Accounting across both sides

The launcher charges its record until its release; the daemon withholds
its slot until its own release, which follows the launcher's. So a
partial or failed cleanup never frees a slot on either side, and a
retention is counted once (§4). A selection names an exact incarnation or
Docker identity and a revision: replay, a stale inspection or a later
incarnation of the same attempt releases nothing. The daemon's slot for a
late incident is withheld exactly as for any quarantine: the launcher
answers its destroy quarantined (§7.2), so the daemon retains it. After
both releases, the daemon's next start offers the slot again; its own later
saves keep the released entry.

Evidence: `TestRecoverReleasesAMicrovmRetentionOnlyOnTheLaunchersProof`
(the runner's release refused until the launcher's; a new incarnation of
the attempt does not block it and stays the launcher's), the daemon's
`TestReleasedRetentionFreesItsSlotAndStaysEvidence` and
`TestMissedObligationStaysChargedAfterLateCleanup` (an owner's late destroy
answered quarantined). Controls R02, R03, X01D, X38, X41, X42.

## 9. Code map

| Concern | Where |
|---|---|
| retained handle | `internal/sandbox/sandbox.go` `RetainedError`, `Describe`, `Handle.Created/Attempt`; `microvm.go` `Prepare`, `dial`/`conn`, `rollback`, `recoverReservation`, `destroyExact`, `list`; `docker.go` `Prepare`, `abandon`, `command`/`docker` |
| incarnation (§11.1) | `internal/launcher/ref.go` `newIncarnation`, `validIncarnation`, `Ref` (`Named`, `Names`, `String`), `Record.Ref`, `ReserveReply.Ref`; `service.go` `admitLocked` (the token), `Record.Incarnation`, `ReserveReply.Incarnation`/`Created`, `CreateOf`/`ConnectOf`/`StopOf`/`DestroyOf` (`create`/`connect`/`stop`/`destroy` with the reference's predicate), `ownedOfLocked`, `ErrRetained`; `incident.go` `ErrStale`, `Selection.Incarnation`, `Record.Selection`, `Selection.Ref`, `selectLocked`; `store.go` `validateRecord` (the token), `archivePath`/`archive`; `proto.go` `Request.Incarnation/CID/Created`, `Reply.Incarnation/Retained/Created`, `Server.mutate` (a mutation that names no incarnation is refused), `refRequest`, `Client.Create`/`Connect`/`Stop`/`DestroyOf` (a `Ref`), `ReplyError`, `Retained`, `Client.Bind`; `internal/sandbox/sandbox.go` `Handle.Incarnation`; `microvm.go` `incs`, `destroyExact`; `internal/state/state.go` `Quarantine.Incarnation`, `Same`, `CompletedBy`, `Selection`, `Find`; `internal/daemon/daemon.go` `retain` (the retention's token); `cmd/urgit-runner/recover.go` `inspect` (`Ref.Names`); `cmd/urgit-vm-launcher/recover.go` `lookup` |
| daemon accounting | `internal/daemon/daemon.go` `retain`, `retainedAs`, `hold`/`unhold`, `acquire`, `releaseSlot`, `freeLocked`, `remainingCapacity`, `Reconcile`, `persistLocked`, `retrySave`, `Close`, `Run` (the poll-time and post-acquire gates, the poll with a copy of the client); `run.go` `handle`, `jobLabel` |
| durable state | `internal/state/state.go` `Save`, `SaveError`, `Acquire`/`Lock.Release`, `Quarantine.Same`, `Quarantine.CompletedBy`, `State.Retain`, `State.Dedupe`; ruling A: `Quarantine.Label`/`Rev`/`Attempts`/`ReleasedAt`, `State.Released`, `Quarantine.Selection`, `State.Find`, `State.Record`, `State.Release` |
| bounded core | `internal/launcher/service.go` `Bounder`, `hostWith`, `startBound`, `obligationLocked`, `cancelCreateLocked`, `teardown`, `halt`, `stopVMM`, `provision`, `BeginStop`, `Reap`/`ReapOnce`/`reapPass`, `Shutdown`, `SetReport`/`Outcome`/`reportOutcome` |
| incidents (ruling A) | `internal/launcher/incident.go` `Incident`, `CleanupAttempt`, `VMMFinder`, `Selection`/`ParseSelection`, `Inspection`, `InspectRecord`, `inspectLocked`, `selectLocked`, `Incidents`, `InspectIncident`, `RetryCleanup`, `Release`, `withAttempt`, `remaining`; `service.go` `teardown` (`releasing`/`retaining`/`retrying`, `incidentSeed`, the late recovery), `quarantine`, `findVMM`, `refusalLocked`, `Record.Label`/`CleanupTrigger`/`CleanupDue`/`Incident`/`Rev`, `ReserveRequest.Label`; `store.go` `publishAt`, `archive`/`archiveDir`/`archivePath`, `normalized`, `boundText`/`boundLabel`, `recordGrowthBytes`, `validateRecord`; `proto.go` `Request.Label` |
| operator tooling (ruling A) | `cmd/urgit-vm-launcher/recover.go` `incidents`, `lookup`, `act`, `printSummary`, `printInspection`, `recoverScripted`, `recoverInteractive`; `main.go` `recover`/`clear` subcommands, `realHost.FindVMMs`, `logOutcome`; `cmd/urgit-runner/recover.go` `recovery` (`inspect`, `act`, `scripted`, `interactive`), `dockerObjects`, `handleOf`; `main.go` `-recover`/`-select`/`-action`, `launcherRecords`, `microvm`; `internal/sandbox/docker.go` `Leftovers`, `absentAnswer`; `sandbox.go` `Spec.Label`/`Handle.Label`; `microvm.go` `labels` |
| host adapter | `cmd/urgit-vm-launcher/main.go` `runCommand`, `realHost.Bound`, `cgroupPaths`, `cgroupFS`, `CreateNetwork` preflight, `Kill`, `listen`, `serve`, `logOutcome` |
| records certification (§11.6) | `internal/launcher/service.go` `openService` (the records directory's own fsync), `Service.uncertified`, `Problems`, `unsafeLocked`, `absenceLocked`, `ownedLocked`, `destroy`, `List`, `recertify`, `reapPass`; `proto.go` `Server.handle` (`list`); `cmd/urgit-vm-launcher/main.go` `serve` (`RECORDS NOT CERTIFIED`) |
| late accounting (§11.8) | `internal/launcher/accounting.go` `ErrUnproven`, `Release` (`Late`), `accounting`, `authorizeLocked`, `finishAccounting`, `accountingPending`, `Released`, `releasedAny`, `timely`, `evidence`/`evidencePath`/`readEvidence`/`evidenceOf`, `ReadEvidence`, `Evidence`; `service.go` `Record.Release`, `entry.acct`/`released`/`disposition`, `teardown` (the release authorized), `destroy`/`DestroyOfRelease` (an incarnation not held answered from its evidence), `reportOutcome`/`Outcome.LateAccounting`, `openService` (a stopping record its evidence backs loaded pending), `reapPass`, `Shutdown`; `incident.go` `Release`, `RetryCleanup`, `inspectLocked`, `Incidents`; `store.go` `readState` (`Snapshot.Authorized`), `archive`, `rearchive`, `encodeRecord`/`validateRecord` (a disposition only with the evidence); the evidence namespace (§11.9): `store.go` `archive` (its certification), `evidenceDir`, `syncFile`, `accounting.go` `readEvidence`/`evidenceOf`/`Evidence` (only through a real `released/`), `service.go` `openService` (a reopen's evidence not taken as durable); `proto.go` `released`, `Reply.Release`/`LateAccounting`, `Client.Released`/`DestroyOfRelease`, `WireProtocol` 3; `internal/sandbox/sandbox.go` `ReleaseProver`; `microvm.go` `ProveReleased`; `internal/daemon/daemon.go` `Reconcile` (a held orphan freed only on proof); `cmd/urgit-runner/recover.go` `inspect` (the launcher's evidence), `describeRelease`; `main.go` `launcherReleased`; `cmd/urgit-vm-launcher/recover.go` `absentWhy`; `main.go` `list` (late bookkeeping reported), `logOutcome` |
| inventory (§11.7) | `internal/launcher/service.go` `absenceLocked` (the directory certified and no `Problem`), `List` (refused, never short), `All` (a presence), `ownedLocked`, `destroy` (a gone incarnation), `Service.fence`, `Problems`, `unsafeLocked`, `openService` (fenced, never refused); `incident.go` `selectLocked`; `store.go` `readState` (classified, nothing deleted); `proto.go` `Server.handle` (`list`: a refusal is an error reply); `cmd/urgit-vm-launcher/recover.go` `lookup`, `recoverInteractive`; `main.go` `serve` (the unaccounted entries' summary); `cmd/urgit-runner/main.go` `reconcileAtStart`; `internal/launcher/launchertest` `Server.StateDir` (tests only) |
| custody (§11.2) | `internal/launcher/ref.go` `Liveness`; `service.go` `Host.Liveness`, `stopVMM`, `findVMM`, `liveLocked`, `Record.Liveness`; `cmd/urgit-vm-launcher/main.go` `realHost.Liveness`, `FindVMMs`, `Kill`, `StartVM` (the pid wait) |
| the obligation at completion (§11.3) | `internal/launcher/service.go` `teardown` (the completion check, `missedWhen`), `finishAccounting` (its late bookkeeping, §11.8), `reportOutcome`, `Outcome`; `cmd/urgit-vm-launcher/main.go` `logOutcome` |
| Docker absence (§11.4) | `internal/sandbox/docker.go` `absent`, `absentAnswer`, `Leftovers`, `Destroy`, `removeContainer`, `NewDockerCommand`; `cmd/urgit-runner/recover.go` `handleOf` (an older entry's names) |
| settled admission (§11.10) | `internal/launcher/settle.go` `ErrSettled`, `SettledAdmitted`/`SettledReleased`/`SettledClosed`, `Settlement`, `NewRequest`/`ValidRequest`, `requestNote` (`check`, `encodeNote`, `decodeNote`), `store.requestsDir`/`notePath`/`readNote`/`keepNote`, `Service.admissibleLocked`/`heldRequestLocked`/`Settle`/`settleUnheld`; `service.go` `Record.Request`, `ReserveRequest.Request`/`ReserveReply.Request`, `Service.requests`/`settling`, `admitLocked` (at most once), `Reserve` (the ledger entry before the record); `store.go` `publishBytes`, `validateRecord` (the request token); `proto.go` `WireProtocol` 4, `Request.Request`, `Reply.Settled`/`SettledWhy`/`Request`, `settle`, a reserve without a request refused, `Client.Reserve` (a fresh token), `Client.Settle`; `launchertest/proxy.go` (tests only); `internal/sandbox/sandbox.go` `Spec.Request`/`Handle.Request`, `Settlement`, `Settler`, `NewRequest`; `microvm.go` `Prepare` (a lost or refused answer settled), `recoverReservation`, `Settle`; `internal/state/state.go` `Quarantine.Request`, `Admission`, `Same`, `Selection`/`Find` (9 fields); `internal/daemon/settle.go` `withheldLocked`, `recountLocked`, `exhausted`, `newAdmission`, `admit`, `removeAdmissionLocked`, `resolveAdmission`, `settleAdmissions`; `daemon.go` `covering`, `New` (admissions withheld from the start), `Reconcile` (settles first), `Run` (`exhausted`), `freeLocked`, `describeRetention`; `run.go` `handle` (the admission before the reserve); `cmd/urgit-runner/recover.go` `inspect` (read only), `settleAdmission`, `identityOf`, `act` (retry refused; release on the settlement); `main.go` `launcherSettle` |
| the state kept to the end (§11.11) | `internal/launcher/service.go` `Service.unheld`, `drainAllLocked` (`Close`), `Shutdown` (its last wait, after the teardowns, within its deadline), `recertify`; `settle.go` `Settle` (counted, refused once closing) |
| legacy recovery from Urgit (§11.12) | `internal/state/state.go` `State.Format`, `CurrentFormat`, `Legacy`, `Quarantine.Legacy`/`Command`/`Evidence`, `State.Refused`, `Refusal`, `RefusalOf` (review 07; by the command's atom since review 08), `ReleaseOf`, `SameCommand` (review 08), `LegacyShape`, `IsLegacy`, `markLegacy`, `Load` (an earlier runner's file marked), `Upgraded`, `save` (the format stamped); `internal/sandbox/sandbox.go` `Inventory`, `Inventorier`; `microvm.go` `Inventory` (hello and the authoritative list, nothing else); `internal/sig/sig.go` `FormatUV`, `CanonicalUV` (review 08); `recovery.go` `RecoveryVersion`, `RecoveryTag`, `RecoveryMessage`, `VerifyRecovery`; `internal/ship/client.go` `Client.Recovery` (`x-ci-recovery`); `recovery.go` `RecoveryCommand`, `RecoveryAnswer`, `RetentionReport`, `PollWork`, `PostRetentions`, `PostRecoveryAnswer`, `ErrNoRecovery`; `internal/daemon/recovery.go` `commandKey` (review 08), `judgeLegacy`, `legacyDigest`, `kindOf`, `retentionReport`, `report`, `startRecovery`, `verifyRecovery`, `recover`, `decisionLocked`, `refuseLocked`, `inspected`, `findLocked`, `releaseLocked`, `refusalsDueLocked`, `settleLocked`, `isFinal`, `queueAnswerLocked`, `flushAnswers`; `daemon.go` `New` (an earlier runner's file saved at once; `Client.Recovery`), `Run` (the report, the answers, `PollWork`), `persistLocked` (every refusal due recorded, then answered); `settle.go` `exhausted` (a legacy retention does not stop the daemon); `cmd/urgit-runner/recover.go` `fromUrgit`, `inspect`, `identityOf`, `printView`, `printRetention`; the controller: `desk/sur/ci.hoon` `recovery-entry`, `recovery-report`, `recovery-status`, `recovery-command`, `state-0` (`recovery-reports`, `recovery-commands`), `action` (`%request-legacy-release`); `desk/lib/ci-recovery.hoon`; `desk/app/urgit-ci.hoon` `handle-http` (its three routes), `handle-assignment-poll` (the command handed over), `handle-action`/`parse-web-action` (`%request-legacy-release`), `daemon-bearer`, `handle-retention-report`, `handle-recovery-answer`, `handle-runner-recovery`, `recovery-command-json`, `recovery-json`, `runner-json` (`retentions`); `desk/gen/ci-recovery-vector.hoon`; the panel: `fe/src/runnerRecovery.js`, `runnerRecoveryView.js`, `runnerRecoveryComponent.js`, `components/RunnerRecovery.jsx`, `components/Runners.jsx`, `runners.js` (`retentionsOf`, `retentionsText`), `api.js` (`ci.recovery`, a refusal's `status`) |
| assignment identity (§11.13) | `internal/daemon/daemon.go` `uvKey`, `claim`/`release`/`claimed` (one claim per atom), `verifyAssignment` (bound by atom; named in the ship's spelling once verified), `Run` (a claimed attempt's delivery ignored first), `Reconcile` (its own sandbox by exact name; the ship asked in its spelling); `recovery.go` `judgeLegacy` (a launcher record by atom), `retentionReport` (the attempt idle, and named, by atom), `releaseLocked` (by atom); `internal/sig/sig.go` `CanonicalUV` |
| execution ledger (§11.14) | `internal/state/ledger.go` `Ledger` (`Open`: made once, never made again; `Has`; `Take`/`Finish`: created exclusively, made durable, never replaced), `State.Ledger`; `internal/daemon/replay.go` `historyOf`, `namedLocked`, `takeExecution`, `finishExecution`, `givenBackLocked`; `daemon.go` `New` (the ledger opened before enrollment, and named in the state file), `Run` (a delivery checked against the ledger; the record taken before `handle`, the finish after it) |
| history transition (§11.15) | `internal/daemon/history.go` `historyOperation`, `epochFloor`, `historySelection`, `historyEvidenceOf`, `parseTransitionEvidence`, `completeLocked`, `recordedTransitionLocked`, `pausedLocked`, `epochLocked`, `advertisedCapacity`, `fenced`, `historyReport`, `confirmHistory`, `recordTransitionLocked`; `daemon.go` `Daemon.began`, `New` (the pause, or the epoch, logged), `Run` (the poll's capacity and `Paused`; every verified assignment through `fenced` before the ledger is asked); `recovery.go` `verifyRecovery` (two operations), `recover` (a transition to `confirmHistory`), `decisionLocked` (a transition's record), `refusalsDueLocked` (`notTransitioned`), `retentionReport` (`History`); `internal/state/state.go` `State.Transition`, `Transition`; `internal/ship/client.go` `Client.Paused` (an explicit 0); `recovery.go` `HistoryReport`, `TransitionReport`, `RetentionReport.History`; the controller: `desk/lib/ci-recovery.hoon` `confirm-history`, `history-entry`, `epoch-at`, `epoch-nonce`, `transition-evidence`; `desk/sur/ci.hoon` `action` (`%request-history-transition`); `desk/app/urgit-ci.hoon` `handle-action`/`parse-web-action` (`%request-history-transition`), `assignment-json` (the epoch nonce), the assignment poll (an explicit 0 taken), `select-resolver`, `handle-recovery-answer` (the audit's kind), `handle-runner-recovery` (`history`), `runner-json` (`history`); `desk/gen/ci-provenance-vector.hoon` (the epoch case); the panel: `fe/src/runnerHistory.js`, `runnerHistoryView.js`, `runnerRecoveryView.js` (the history first; the transition's confirmation), `runnerRecoveryComponent.js` (`panelReducer`, `transition`), `runners.js` (`historyOf`), `components/Runners.jsx` (the paused marker, `Transition…`) |
| transition records and verified images (§11.16) | `internal/state/state.go` `Transition` (`UnmarshalJSON`, `MarshalJSON`, `Record`, `Problem`), `SupersededTransition`, `State.Superseded`, `Load` refusing a file that names a field twice (`namedTwice`, `nameTwice`, `members`, `keptAsHeld`); `internal/daemon/history.go` `recordedTransitionLocked`, `transitionRecordLocked`, `historyReport` (`InvalidTransition`), `recordTransitionLocked` (a superseded record kept); `recovery.go` `decisionLocked` (only a transition answers); `daemon.go` `New` (a record that is none logged); `internal/ship/recovery.go` `InvalidTransitionReport`; `internal/sandbox/microvm.go` `verifiedImage`, `verifyImage` (a value), `verified` (the start's checks, before every Prepare), `Prepare` (its own image), `ImageDigest`/`Name` (the last verified, under the lock); the panel: `fe/src/runnerHistory.js` (`historyRow`'s `invalid`), `runnerHistoryView.js` |
| unit | `launcher/urgit-vm-launcher.service` `TimeoutStopSec`, `KillMode` |

## 10. Not run here — the next gates

Source verification is not qualification. Still required on a real host,
with the operator's approval: cgroup v2 placement and delegation for the
launcher's slice under systemd (`Delegate=`), the jailer's actual cgroup
layout and `--daemonize` behaviour with this command runner, `ip`/`nft`
semantics of the preflight and removals (including listing formats) and
real isolation, SELinux labels on the jail, the unit's stop behaviour under
systemd, installation, a real VM's boot/connect/teardown timing within the
bounds above, the durable publications' duration on the installed
filesystem (not bounded by the obligation's deadline in source, §7.2), and
guest execution. Recovery (§8) was never run on a real host either: no
`urgit-vm-launcher recover` or `urgit-runner -recover` against a real state
directory, launcher, Docker daemon or retained resource; no real `/proc`
scan for a real VMM (only this test binary's own inert children); no live
cleanup, retry or release. None of those was executed.

The correction after independent review 01 (§11) was verified the same way
and adds its own next gates: Docker's not-found replies (§11.4) are the
grammar of Docker's CLI and daemon as written, never asked of a live daemon
of the deployed version — a reply that differs is refused (the retention
stays), never taken for absence; `/proc` under the host's real mount options
(`hidepid`, another pid namespace) and a real jailer's and Firecracker's
command lines (§11.2); the kernel's random generator at admission (§11.1: a
failure refuses the reservation); and the durable publications' real
duration, which decides when a release is reported published late (§11.3);
and a real filesystem's directory fsync failing at an open (§11.6: tested only
with injected failures before the syscall, over fresh service objects); and
the entries a real filesystem, or an operator, leaves unreadable, malformed
or foreign in the records directory (§11.7: tested only with private fixture
files spoiled between two lives); and the release evidence's durability
across a real power loss, and `released/`'s growth on the installed
filesystem (§11.8: its durable images tested by fresh services over the same
private files, its failures injected before or after the syscall); and the
evidence namespace's barriers on a real filesystem — the state directory's
fsync certifying `released/`'s link, and `released/`'s and the evidence's
own (§11.9: tested only with failures injected before the syscall).

Settled-admission ruling 01 (§11.10) adds its own: the request ledger's
durability across a real power loss and its growth on the installed
filesystem (tested with failures injected before or after the syscall, over
fresh services on the same private files); a real launcher's and daemon's
crash at each durable image (tested by fresh objects over the same files);
and delayed, lost and replayed requests on a real socket (tested through an
in-process proxy that holds, loses, drops the answer to, or lets a test
replay exactly one request).

Independent review 05's correction (§11.11) adds its own: the service's
end, with a settlement in progress, on a real filesystem that hangs, and
serve's real stop under systemd (tested only with one syscall of private
files held, a second service over them, and serve's stop order reproduced
in process on a private socket).

The legacy recovery (§11.12) adds its own. Its Hoon — `%urgit-ci`'s new
state, routes and action, `ci-recovery.hoon` and `ci-recovery-vector.hoon` —
was never compiled, and its routes never ran on a ship. That qualification is
a fake ship: build the desk (`zig build -Ddesk=…`) and commit it over a
populated `%urgit-ci` of each supported shape, which it migrates in place
(contract §8c; state-migration ruling 01), and run `+ci-migration-vector`,
which must end `%.y`. Then exercise the report, answer, panel and action
routes with a scripted daemon, and run `+ci-recovery-vector`, which must end
`%.y`. The panel was never rendered by
React or built by vite: this worktree has no `node_modules`, and
`fe/src/runnerRecoveryRender.test.js` runs once they are installed. The path end
to end — the panel, the ship, a daemon of this version with a legacy
retention, the launcher, and back — never ran on a qualified host.
QUESTIONS-SOURCE-01 §12 is decided (state-migration ruling 01): an upgrade
keeps `%urgit-ci`'s state, its runners' enrollment included. No agent on a
ship was migrated, and the migration's own qualification is held (contract
§8c).

The history transition (§11.15) adds its own. Its Hoon was never compiled
or run: the epoch (`epoch-at`, `epoch-nonce`), the assignment signed with
it, the action, the poll's explicit capacity 0 and the resolver's. The
stage's static check reads them from the source text only (RECORD).
`+ci-provenance-vector` must end `%.y` with its epoch case, whose bytes
`internal/sig/epoch_test.go` pins. The panel's history section was never
rendered by React. No runner of an earlier version was upgraded to this one
on a real host, and no transition was confirmed from a real ship.

Independent review 12's correction (§11.16) adds its own: a state file
damaged by a real disk fault or a power loss, rather than written by a
test, and two Prepares at once on a real host, each booting the image it
verified. Neither ran.

P4-VM-STAGE-A-SOURCE-01's C1 and D1 (§7.3) add their own. For C1: systemd's
real delegation (`trusted.delegate`, `DelegateSubgroup=`, the controllers it
enables) and the kernel's cgroup v2 rules, tested only on a private model of
both; the jailer's `--parent-cgroup` inside a delegated subtree; and the
stop: a job's VMM is now inside the unit's cgroup, so after the launcher's
own bounded teardown and its exit, `KillMode=mixed` SIGKILLs anything left
there, a VMM a quarantined record still holds included. For D1, source only
while Stage B is held: real nft listings and the kernel's handling of the
input hook (tested only on a private nft model), and the host firewall's own
input decisions, which an exception never overrides.

## 11. Identity, proof and deadlines across callers (the correction after independent review 01)

Independent review 01 (`.scratch/source-stage-01/independent-reviews/integration-01/REVIEW.md`)
found four places where the source took an observation for a proof, or a
tuple for an exact identity when it was not one: a process read error taken
for a VMM's death (R1), an incarnation tuple that repeats across an empty
restart (R2), a cleanup that finishes after its obligation and still releases
(R3), and a Docker error about another object taken for the selected
object's absence (R4). This section is the model the correction implements.
It was written before the code; §9 maps it to the code. Every subsection
states the authority, the live and durable ownership, what is an
observation and what a proof, the ambiguous effects, the stale callers, what
a crash and a reopen do, and the charge and its release. None of it adds an
authority, a deadline, an allowance or a live administration channel.

### 11.1 The incarnation token (R2)

**What was wrong.** A record's id is derived from its attempt (it names the
host objects: jail, cgroup, network namespace); its vsock `cid` comes from a
cursor that an empty reopen restarts at 3; `created` is the admission time in
whole seconds. So after an attempt's reservation is released, the service
reopened with no records, and the same attempt admitted again within that
second, the new reservation has exactly the old one's `(id, cid, created)`,
and a delayed destroy of the old one destroys the new one.

**The token.** Admission gives every reservation an **incarnation token**:
128 bits from the kernel's random generator (`crypto/rand`; 32 lowercase
hex characters), fixed for the record's life. It is published with the
record's first publication, so it is durable from before the reservation is
acknowledged, and it is carried wherever the reservation is named. It depends
on no clock, cursor or allocation file. An empty restart, a clock that repeats
a second or steps back, or a state directory created anew cannot make it
repeat; a repeat is a 2^-128 event per comparison. A counter was not chosen: it
repeats after its state directory is recreated or restored, and it needs a
durable publication of its own before every acknowledgement. Those failure
modes are exactly what the token avoids. Should the random source ever fail,
admission refuses: nothing is charged or written.

`created` keeps its meaning (the admission time, in seconds, for display and
ordering), and `cid` keeps its (the vsock address, unique among the records
held). Neither is an identity any more; neither field's meaning changes.

**Exact reference.** `launcher.Ref{ID, Incarnation, CID, Created}` names one
incarnation. `Ref.Names(record)` is the one predicate:

| the reference | the record | names it when |
|---|---|---|
| has a token | has a token | the id and the token are equal (and the cid and creation time, where the reference gives them, agree) |
| has none | has none (written before tokens) | the id, cid and creation time are equal (the old predicate, kept for such records only) |
| has a token | has none, or the reverse | never |

So a caller that cannot name a token reaches only a record written before
tokens, never a newer incarnation. A loaded record without a token is kept as
it is: nothing is migrated, and no token is guessed for it.

**Every caller and consumer** carries the token:

| Where | What changes |
|---|---|
| admission (`Service.Reserve`, `admitLocked`) | `Record.Incarnation` from `newIncarnation()`; `ReserveReply.Incarnation`; a retained answer (R8) names it too |
| the record (`store.go`) | persisted as `incarnation`; `validateRecord` accepts 32 lowercase hex characters or none |
| the wire (`proto.go`) | `Reply.Incarnation` (reserve, retained); records in `list`/`inspect` carry it; `Request.Incarnation` with `cid`/`created` names the incarnation of `create`, `connect`, `stop` and `destroy`; the protocol is 2 (`WireProtocol`), so a runner and a launcher of protocol 1 — one that sent bare ids, one that would ignore a token — refuse each other at hello |
| the server | `create`, `connect`, `stop`, `destroy` act only on the incarnation the request names (`Service.CreateOf`, `ConnectOf`, `StopOf`, `DestroyOf` with a `Ref`); **a request that names none is refused** (`ErrInvalid`); the bare-id `destroy` fallback is gone. `inspect` and `list` only read, and stay by id |
| a named incarnation that is gone | `destroy` answers done (idempotent); `create`, `connect` and `stop` answer `ErrStale`; nothing is touched either way |
| the client | `Reserve` and `Retained` answer the token (`ReserveReply.Ref()`); `Create`, `Connect`, `Stop` and `DestroyOf` take a `Ref` |
| the sandbox (`microvm.go`) | `Handle.Incarnation`; tracked with the handle; the rollback, the lost-answer recovery (from the list), `Destroy`, `HandleFor` and `Orphans` name it; `Describe` shows it |
| the daemon | `state.Quarantine.Incarnation`, written by `retention()`; `Same` and `CompletedBy` compare and complete it; `Find`'s selection includes it |
| the launcher operator | `Selection` gains the token; its text is `<id>/<token>/<cid>/<created>/<rev>`; `selectLocked` uses `Ref.Names`; the listing shows it |
| the runner operator | the retention's selection includes the token; the release's proof is the launcher's list holding no record the retention's `Ref` names |
| the evidence | `released/<id>.<token>.json` (a record without a token: `<id>.<cid>.<created>.json`); an existing file stands in only if it is this `Ref`'s own evidence |

**Stale callers.** A delayed owner request names an old token: `destroy`
answers done, and every other mutation is refused, all without touching the
new incarnation. The reaper holds entry pointers and skips released ones
(`gone`), as before. The operator's selection names the token and the
revision. A retained handle names the token in the state file. The
service's bare `Create`, `Connect`, `Stop` and `Destroy(owner, id)` stay as
the in-process form the tests drive; they act on whatever the id names now,
and no production caller uses them: the wire — the owners' one production
entry — and the sandbox use the `…Of` forms.

**Crash and reopen.** The token is in the record file and reloads with it.
The cid cursor may restart, and that no longer matters to identity.

**Charge and release.** Unchanged: a stale caller releases nothing and
charges nothing.

**Evidence.** `TestIncarnationTokenSurvivesAnEmptyRestart` (the same second,
and a clock stepped back: a stale destroy by the token or by the repeated
tuple releases nothing, and a stale create, stop or connect is refused, the
new incarnation untouched), `TestStaleDestroyNeverReachesALaterIncarnation`
(the wire: by token, by tuple, and a request that names none refused),
`TestALegacyRecordIsNamedOnlyByItsTuple`, `TestEvidenceIsKeptPerIncarnation`,
`TestReleaseIsDurableAndKeepsItsEvidence/another incarnation's evidence at
its path`, `TestRetainedReserveIsNamedOnTheWire`,
`TestARetainedReservationKeepsItsTokenAcrossARestart` (a failed first
publication, then a reopen), `TestAProtocolOneLauncherIsRefused`,
`TestRetentionIdentityIsTheIncarnationToken`,
`TestTeardownFailureRetainsTheExactIncarnation`,
`TestRecoverScriptedHoldsTheSelectionExact`, and review 01's own probe (its
documented adaptation). Controls N01, N02, S01, R03, X06, X06C, X15, X22,
X22I and Y01–Y32.

### 11.2 Process custody: verified, gone, or unknown (R1)

**What was wrong.** `Alive` answered a boolean, and every failure to read a
process's command line became "not this VM's". The retry's scan `FindVMMs`
then returned a clean empty list, the retry took it for proof that the VMM
of unknown pid was gone, cleared the holding and removed its disk.

**Three answers.** What a host can say about a pid (`launcher.Liveness`,
`Host.Liveness(id, pid)`) is one of:

- **Running**: verified as this id's VMM. Its command line carries exactly
  `--id <id>` (the jailer before it execs, and Firecracker after, both do).
- **Gone**: verified not running as it. Either there is no such process (the
  read finds the process's entry missing from a proc root that is there: it
  exited), or the process's readable command line does not mention this id
  at all (a foreign process that took the pid, a zombie, a kernel thread).
- **Unknown**: not verifiable. The command line could not be read for any
  other reason (`EACCES`, `EIO`, not a file), the proc root itself is not
  there to say that the process is missing (not mounted, misconfigured), or
  the command line mentions this id but not as `--id <id>`, which is neither
  provably this VM's VMM nor provably another's. Unknown carries its
  reason.

**Callers.**

| Caller | Running | Gone | Unknown |
|---|---|---|---|
| `stopVMM` (a known pid; teardown, halt) | signalled (TERM, then KILL), waited for | the holding clears | never signalled, never proof: at the stop's bound it is a problem ("its state cannot be verified: …"), and nothing is removed |
| `Kill` (adapter; `Stop`) | signals the pinned process | nothing to do | an error, and no signal: ownership is not verified |
| `FindVMMs` (the retry's scan) | listed | skipped | the scan is incomplete: an error naming the uncertain entries (bounded) |
| `findVMM` (core, the retry) | stopped as above | — | unresolved: nothing removed; any Running pids it found are still stopped (their ownership is verified) |
| `StartVM`'s pid wait | the start's pid | keeps waiting | keeps waiting: the start stays uncertain (a VMM of unknown pid) |
| inspect/list | `alive: true` | `alive: false` | `alive: false`, with `liveness: unknown` and its reason |

A scan is complete only if the proc root was listed, every numeric entry was
classified Running or Gone, and the look was not cut short. A vanished entry
(its process exited while the scan read it) is Gone. An unlistable proc root,
an interrupted look, or any Unknown entry makes the scan incomplete. So a
per-entry read error can never become proof of absence.

**Authority and ownership.** An unverified pid is never signalled; ownership
verification and liveness are one classification, not two guesses. Custody
that cannot be verified is retained: the record keeps its charge and its
holding, and removal waits for a complete, verified answer. The operator
sees the reason in the attempt's detail.

**Crash and reopen.** Nothing persisted changes: the holding (`has_vmm`,
`pid`) is durable as before, and the answer is taken afresh each time.

**Evidence boundary.** The adapter's tests use a private proc-shaped tree and
this test binary's own inert children; no other pid is read or signalled.

**Evidence.** The adapter's `TestFindVMMsNeverTakesAnUnreadableEntryForAbsence`
(a command line it may not read, a non-file, an ambiguous mention, a look
cut short; the complete scan as control), `TestFindVMMsLooksForTheExactID`,
`TestKillNeverSignalsAnUnverifiedProcess` (this test binary's own inert
child: unverifiable, not signalled; foreign, not signalled; verified,
ended), `TestAMissingProcRootProvesNoProcessGone`,
`TestStartVMAnswersOnlyAVerifiedPid`,
`TestRecoverRetryKeepsCustodyOnAnIncompleteScan` (the operator's retry over
the real scan); the core's `TestAnUnverifiableVMMIsNeverTakenForGone` and
`TestAnIncompleteScanStopsWhatItVerifiedAndResolvesNothing`; review 01's
own probe (byte-identical). Controls A14, X09, X26X and Y40–Y51.

### 11.3 The obligation at completion (R3)

**What was wrong.** Lateness was judged when a teardown began. A cleanup that
began in time and returned from its last host effect after the obligation's
deadline `D` still released.

**The rule.** A releasing teardown's outcome is decided after its last host
effect has returned and before anything is withdrawn: at that point, if the
service clock (the one that decided `D`) is at or after `D`, the obligation
was missed, however the teardown began. The teardown then ends as a late
recovery does:

- quarantined and charged;
- its incident marked `missed`, its attempt noting that the cleanup finished
  after the obligation's deadline;
- its caller answered quarantined (`ErrQuarantined`, never a release);
- reported LATE.

A successful cleanup mitigated the hazard; it does not undo the breach, and
only the operator's separate release returns the capacity (ruling A). The
cases:

- **Begun in time, finished late.** Missed.
- **A host effect that returns late after its context was cancelled at `D`.**
  Missed: the check follows the return, whatever bounded it.
- **A result exactly at `D`.** Missed: the deadline is not after the result,
  as at entry.
- **The quarantine's own publication fails.** The durable `stopping` holds it,
  as for every quarantine.
- **A restart.** An interrupted teardown's recovery never releases, and it
  records the obligation as missed if its `cleanup_due` has passed.

**Publication.** The release's own durable withdrawal follows the decision,
and its duration is not bounded in source (§7.2's disclosed limit): no
context or unit timeout makes every filesystem syscall interruptible. In the
current source, a withdrawal that completes at or after `D` does not undo a
release decided before `D` — its cleanup had finished before `D`, and
nothing was left running or held past it. **Late-accounting ruling 01
decided that such a release stands** (§11.8): its capacity returns once its
accounting is durably confirmed, even late, with its evidence kept first,
and its late bookkeeping is kept in that evidence and reported apart — by
every release, an owner's destroy too (`Outcome` kind `released`,
`LateAccounting`; serve logs `RELEASED, ACCOUNTING CONFIRMED LATE`; the
destroy's reply says `late_accounting`). This is kept distinct from a
cleanup that itself finished late, which ruling A decides (above). A
release whose accounting is not confirmed stays charged, pending, until a
retry confirms it (`finishAccounting`, which keeps the obligation's
deadline for that: `accounting.due`). §11.5 traced the case while §8 was
held; §11.8 is the source now.

**Authority, stale callers, crash and charge.** Nothing new decides: the
service clock that set `D` decides at completion, and only the operator's
release frees what the check quarantines. The check reads the obligation
the teardown carries (`by`), which no caller can change. A crash after the
removals and before the outcome is published leaves the durable
`stopping`, which a restart loads quarantined; its recovery never releases,
as for every interrupted teardown. The charge stays until the operator's
release. A release published late returned the capacity that was decided,
in time, to return.

**Evidence.** `TestCleanupFinishingLateIsAMissedObligation` (after the
deadline, exactly at it, and — the control — just before it),
`TestALateReturnAfterCancellationIsAMissedObligation`,
`TestReapedCleanupFinishingLateIsReportedLate`,
`TestALateFinishWhosePublicationFailsStaysHeld`,
`TestAReleasePublishedLateIsSaid`, `TestAReleaseConfirmedLateIsSaid`, the
command's `TestServeLogsAReleasePublishedLate`, and review 01's own probe
(its documented adaptation). The restart case is ruling A's
`TestCleanupAttemptsNeverRelease` (the recovery a restart owes, in time and
past its deadline). Controls Y60–Y69B.

### 11.4 Docker absence: the exact object's answer (R4)

**What was wrong.** `absentAnswer` took any reply containing "no such
<kind>" as the object's absence, so "No such volume: <another volume>" and
"No such volume driver: …" certified the selected volume absent.

**The rule.** A look at one object (`docker inspect --type container
<name>`, `docker volume inspect <name>`, `docker network inspect <name>`)
proves its presence when it answers. It proves its absence only when the
whole reply — after the runner's own `docker <subcommand>: ` prefix — is
exactly one of Docker's not-found replies for that kind and that name:

- `Error: No such <kind>: <name>` (the CLI's);
- `Error response from daemon: No such <kind>: <name>` (the daemon's, passed through);
- for a volume, `Error response from daemon: get <name>: no such volume`;
- for a network, `Error response from daemon: network <name> not found`.

Anything else is a failed look and proves nothing absent: another object's
name, a missing driver, a transport or API error, several lines, or a warning
before the reply. The retention stays until a look succeeds.

No reply proves an object without a name absent (a handle that names none
of a kind).

**Callers.** `Leftovers` looks at every object of the handle, and any failed
look is its error. The runner's release is refused unless a `Leftovers`
taken at release time is error-free and empty; its retry records what
`Leftovers` left. `Destroy` — whose nil is what frees the daemon's slot
after a teardown (`run.go`), a reconcile or a partial `Prepare` — counts a
removal that failed as done only on the same exact reply about that object
(`absent`): the container (`rm -f`, and the wait on a removal another party
has in progress), each volume, the network. A listing of the network's
containers that failed is not kept by itself: Docker refuses to remove a
network while any container is attached, so the network's own removal, or
its exact absence, is the proof that none is. A Docker entry written before
retentions named their objects (the handle only) is looked at, retried and
released by the names every Docker sandbox of its handle has — the network
and container the handle, the volume its `-work` (`handleOf`) — never by
empty names.

**Authority, ownership, crash and charge.** The runner owns its Docker
objects (the owner label); nothing here widens what it removes. A failed or
foreign reply keeps the slot retained, durable in the state file across a
restart, until the operator's retry and release (§8.4); nothing frees it
automatically. There is no Docker-side incarnation: the handle names the
same objects every time, and a retention's selection carries its revision.

**Evidence.** `TestDockerAbsenceIsTheExactObjectsReply` (each kind's replies,
and another object's, another kind's, a driver's, extra words or lines, no
daemon, a nameless object), `TestDockerLeftoversNeedTheExactObjectsReply`,
`TestDockerLeftoversAreExact`, `TestDockerDestroyNeedsTheExactObjectsReply`,
`TestDockerRemovalInProgressNeedsTheExactReply`; the daemon's
`TestDockerTeardownFreesTheSlotOnlyOnTheExactObjectsReply` and the runner's
`TestRecoverReleasesADockerRetentionOnlyOnTheExactObjectsReply` and
`TestRecoverLooksAtAnOlderDockerEntryByItsHandlesNames`, all through the
real backend over a nonexecuting Docker recorder
(`sandbox.NewDockerCommand`); review 01's own probe (byte-identical).
Controls X25, X25K, Y31 and Y70–Y78.

### 11.5 Timely cleanup, late publication: the trace behind QUESTIONS-SOURCE-01 §8 (held until late-accounting ruling 01)

*Superseded by §11.8.* This is the trace as it stood while §8 was held, kept
as it was. Where late-accounting ruling 01 changed the source, §11.8 says so:
- a release's evidence is written first;
- a record file that could not be withdrawn is pending accounting, not an
  incident;
- the late bookkeeping is durable in the evidence and reported for an
  owner's destroy too;
- an incarnation not held is answered done only from its evidence, and the
  runner's release takes only that evidence as its proof.

This traces, in source, the case QUESTIONS-SOURCE-01 §8 asks the operator to
decide, as orchestrator triage 08 requires
(`.scratch/source-stage-01/orchestrator/opus-question-triage-08.md`, sha256
`9d3afb7f…00f4`). It describes the current candidate behaviour. It is not a
ratified rule, and §8 stays held for the operator's separate decision.

**Three instants, never folded together.** A releasing teardown carries its
obligation's deadline `D` (`by`, decided from the original trigger by
`obligationLocked`). The end of the host cleanup is the instant its last
host effect returned: `cleaned`, read once from the service clock after the
removals. The end of the publication is the instant the record's
withdrawal returned (`published`), or, when its directory fsync failed, the
instant a later retry confirmed it (`confirmed`, in `finishWithdrawal`).
The outcome follows from them in this order:

| cleanup ended | publication ended | outcome | the launcher's capacity |
|---|---|---|---|
| at or after `D` | — (nothing is withdrawn) | a missed obligation (ruling A, §11.3): quarantined, incident `missed`, the caller answered quarantined, reported LATE | charged until the operator's release |
| before `D` | before `D` | released | returned |
| before `D` | at or after `D` | released — **the candidate §8 holds**; the reason states all three instants; a teardown the launcher started is reported `released`, `Late` | returned |
| before `D` | not confirmed (the unlink done, its directory fsync failed) | an unconfirmed withdrawal, not a quarantine: `withdrawn`, the caller answered `ErrNotDurable`, reported `unconfirmed` | charged until a retry confirms it |
| before `D` | confirmed later, at or after `D` | released on that confirmation — the candidate again; the reason states when it was confirmed | returned then |

The first row is decided before anything is withdrawn, and nothing that
happens later changes it: a late physical cleanup is never re-labelled
timely bookkeeping.

**Effect-then-error.** After a failed unlink, `store.remove` looks the file
up again. If it is still there, the withdrawal failed and the teardown
quarantines ("cleanup succeeded but the record could not be withdrawn"); the
record keeps its evidence. If it is gone, the directory's fsync decides
(rows 2 to 5). Within the process, a visible unlink is never taken for
durable absence: until the directory's fsync succeeds, the entry stays
charged (`withdrawn`).

**A lost reply, and the runner's consumer.** The launcher's outcome does not
depend on its reply reaching the daemon. The daemon frees its slot only on
a destroy answered done (`run.go`). Every other answer retains the sandbox
(`Daemon.retain`, durable in its state file) until the operator's
`urgit-runner -recover` release, which needs the launcher's list to lack the
exact incarnation (§8.4): a lost reply, its own client wait (`teardownBound`)
running out, `ErrNotDurable` or `ErrQuarantined`. So an unconfirmed launcher
release never frees the daemon's slot. A release answered done — timely or,
under the candidate, published late — frees the slot on both sides. The
daemon's reconcile at start lists the launcher's records of this daemon
(`Orphans`): one the launcher still holds is held or retained, counted once
(§4), and a released one is not listed.

**Restart before confirmation.** The unconfirmed state (`withdrawn`, `pubBy`)
is in memory only. A reopened launcher first certifies the records
directory's own contents — an fsync of `attempts/` before it reads anything
(§11.6):

- If the fsync succeeds, what the directory shows is durable. The file is
  absent (the record is released, its capacity free), or the unlink was lost
  before it was durable (a power loss) and the earlier `stopping` record loads
  quarantined (`interruptedTeardown`), charged, an incident for the operator.
- If it fails, the reopened launcher is fenced. It answers no absence as a
  release — not a replayed destroy, not an inspection, not the owners' list
  — and admits nothing, while it still enforces the records it could read.
  Its reaper retries the fsync, and the fence lifts once one succeeds.

Until triage 08's restart proof, the open certified only the directories
above the records, so a reopened launcher took a visible, non-durable absence
for a release, before any later publication's fsync could make it durable.
That is fixed (§11.6).

**What survives a restart.**

| Fact | Where it is kept | Survives a restart |
|---|---|---|
| an incident's trigger, `D`, and whether it was missed | the record (`cleanup_trigger`, `cleanup_due`, `incident`) | yes, durable |
| a release's `D`, its cleanup's end and its publication's or confirmation's instant | the released record's reason, in the service's `History` | no: memory only |
| `withdrawn`, `pubBy`, `pubLate`, `late`, `owed` | the entry | no: memory only |
| whether the records directory is certified | the service (`uncertified`), reported by `Problems` | no: every open certifies it again (§11.6) |
| the outcome of a teardown the launcher started (`quarantined`, `unconfirmed`, `released` with `Late`, `halted`) | `Outcome`, then serve's log line | only as far as the log sink keeps serve's output: a diagnostic, never a publication barrier |
| an owner's destroy: done, `ErrQuarantined`, `ErrNotDurable` | the wire reply; serve's per-request log line (`ok=…`) | the daemon's retention survives in its state file. The launcher keeps no durable trace of a release published late on an owner's destroy: the reply says done, with no late flag |
| the operator's release of an incident | `released/<id>.<token>.json`, written before the withdrawal | yes, durable |

**Ordinary teardown and incident cleanup stay apart.** Rows 2 to 5 apply only
to a releasing teardown — an owner's destroy, the deadline, a rollback, the
stop — that met its obligation. A quarantined incident's cleanup, automatic
(`retaining`) or the operator's retry (`retrying`), never releases,
whatever its timing. Only the operator's separate release does, with no
obligation deadline of its own and its evidence published first. An
unconfirmed withdrawal of that release is finished by a retried release, or
by the reaper, with no `pubBy`.

**Evidence boundary.** The rows are tested on the service's own clock, set by
the test, with fault-injected filesystem calls: `completion_test.go`, whose
`TestAReleasePublishedLateIsSaid` and `TestAReleaseConfirmedLateIsSaid` check
each instant in the reason; `TestReleaseWaitsForADurableWithdrawal` (an
effect-then-error unlink, an unconfirmed withdrawal); and the daemon's
`TestDestroyFailureQuarantines` and `TestTeardownFailureRetainsTheExactIncarnation`
(any error from a destroy retains). No kernel or filesystem stall was
measured. The host's removals are nonexecuting models and recorders, so
§7.3's idempotent absence proofs hold at source level only. The restart
window this trace first found is closed (§11.6); no test simulates a power
loss.

### 11.6 Every open certifies the records directory (triage 08's restart proof)

**What was wrong.** Orchestrator triage 08's restart proof
(`.scratch/source-stage-01/orchestrator/triage08-restart-proof/ADDENDUM.md`,
sha256 `029927d9…01f4`, with its probe `review_absent_test.go`, sha256
`2eed67ce…e392`) reproduced what §11.5 had disclosed. A withdrawal whose
unlink succeeded and whose directory fsync failed is kept charged in its
first life, but a reopened service took the visible absence for a release —
no fence, no charge. The open certified the state directory's parent and the
state directory (the links), never `attempts/`'s own contents, and a later
publication's fsync is too late for the readers before it. This is a
durability defect of its own, whichever answer QUESTIONS-SOURCE-01 §8
receives.

**The rule.** Every open fsyncs `attempts/` after certifying the links and
before reading the records (`openService`). Its success makes every presence
and absence it shows durable. Its failure does not fail the open; it fences
the service (`Service.uncertified`):

- `Problems` reports the records directory (`uncertified`), and serve logs
  `RECORDS NOT CERTIFIED`;
- nothing is admitted or booted (`unsafeLocked`: `ErrNotDurable`);
- no absence is answered as a release. An id that is not held (`ownedLocked`
  → `absenceLocked`: a replayed destroy, an inspection, a create, connect or
  stop) and a gone incarnation of a held id (`destroy`) are answered
  `ErrNotDurable`, never done or unknown;
- the owners' list, which the daemon's reconcile and the runner's release
  take as proof, is refused, direct and on the wire (`Service.List`; until
  independent review 02's correction, `Service.Certified` on the wire only:
  §11.7);
- the records it could read are loaded and still enforced. Their deadlines
  are reaped and their owners' destroys run: a known VMM is stopped even when
  nothing can be published (a halt, with nothing removed and the record still
  charged);
- each reaper pass retries the fsync (`recertify`). The fence lifts once one
  succeeds; a failed retry keeps it.

**Authority, proof and charge.**

- No authority is added: the fence only withholds answers and admissions,
  and enforcement of what is held continues.
- A visible absence is an observation; the fsync of its directory is the
  proof.
- A replayed destroy, or a stale destroy of an earlier incarnation, is
  refused while uncertified, never answered done.
- Nothing persists the fence: every open certifies again.
- A record the fenced service cannot see is not charged by it. But no
  capacity is admitted against it, and no consumer is told it is released,
  until the directory is certified. Its owner's retention, the daemon's,
  stays until then.

**Consumers.**

- The daemon's reconcile (`Orphans`, which asks for the list): a refused list
  is its error, and it frees no held slot (`TestARefusedListFreesNoHeldSlot`).
- Its lost-reserve recovery (`recoverReservation`): a refused list retains
  the attempt without an identity. (Since settled-admission ruling 01 it asks
  for no list: an unanswered or refused settlement retains the attempt by its
  request, §11.10.)
- The runner's release (`urgit-runner -recover`): a refused list is no proof
  (`TestRecoverReleasesNothingOnARefusedList`).
- The launcher's own read-only listings (`list`, and `recover` without an
  action) read the state directory beside a running serve without
  certifying it: they show what is visible and release nothing.

**Evidence.**

- `TestReopenCertifiesTheRecordsDirectory`, four subtests:
  - the barrier still failing: the service is fenced; a replayed destroy, an
    inspection, a reservation and the list are refused; a failed retry keeps
    the fence; the known guest's VMM is still stopped;
  - repaired at the reopen: normal;
  - repaired later: the reaper lifts the fence;
  - a stale destroy of an earlier incarnation: refused.
- `TestPublicationIsWriteSyncRenameSyncDir`: the open's three
  certifications, in order.
- The daemon's and the runner's consumer tests above.
- Triage 08's own probe, byte-identical.
- Controls Y80–Y90 and Y80R.

The tests use fresh service objects over the same visible private files, with
the records directory's fsync failing on demand before its syscall: not a
power loss, and not a real launcher process's restart.

### 11.7 An inventory is authoritative only when it is complete (independent review 02)

**What was wrong.** Independent review 02
(`.scratch/source-stage-01/independent-reviews/integration-02/REVIEW.md`,
sha256 `fee0a22a…ae4a`) found a record the loader cannot classify —
malformed, unreadable, unsupported, of another identity — correctly reported
in `Problems` and omitted from the loaded map, while every absence answer
still took that omission for a release:

- the wire list answered success without it;
- an exact `DestroyOf` answered done while the model VMM and the record's
  file remained;
- the runner's actual release path, over the real wire, durably released its
  retention.

§11.6 had closed one cause of false absence, an uncertified directory, and
taken it for the whole. A certified directory proves that what it shows is
durable, not that every entry it shows was understood.

**Five facts, never folded into "not in the map".**

| Fact | What establishes it | Where |
|---|---|---|
| the durable namespace: what the records directory shows is durable | its fsync at open, or a later successful retry | §11.6; `uncertified` |
| complete classification: every entry is a record this launcher loaded, or one of its own unfinished publication temps (never authoritative); and no two loaded records share a cid or a network index (a duplicate: both are loaded and enforced, but the directory is not what this launcher wrote) | the loader at open, reporting no `Problem` | `readState`; `s.fence` |
| where a record can be | the store publishes a record of id X only at `attempts/X.json` (through `X.json.tmp`), and the loader loads it only from there, with content that names X | `store.publishAt`; `validateRecord` |
| exact owner and incarnation | the loaded record's owner and token (§11.1) | `ownedLocked`; `Ref.Names` |
| quiescence | the service is the directory's only writer while it holds the lock; after the open its memory is the inventory, and every publication or withdrawal it makes updates both | `lockState`; the map |

**The rule.** An absence is proof of a release only when the inventory is
**authoritative**: the namespace certified and the classification complete.
While any entry is unaccounted for (a `Problem` of any kind) or the directory
is uncertified, the service answers no absence as proof, for any owner, id
or incarnation:

- the owners' list, direct (`Service.List`) and on the wire, is refused
  (`ErrUnsafeState`, or `ErrNotDurable` for the directory);
- an id that is not loaded is not "no such vm": its inspection, create,
  connect and stop are refused, and its destroy — unknown, or stale (a gone
  incarnation of a held id) — is refused, never answered done
  (`absenceLocked`);
- an operator selection whose record is not loaded is refused the same way
  (`selectLocked`), and the launcher's CLI does not tell the operator it was
  released (`lookup`);
- nothing is admitted or booted (`unsafeLocked`, as before).

A partial inventory is not narrowed by id. An entry that cannot be read may
be the very record asked about: its id is then known only from its name, and
the identity a malformed or foreign-identity entry claims is not to be
believed. So the refusal never depends on which entries are unaccounted for.

Only a readable, loaded record is enforced and answered for. Its owner's
destroy, stop, connect and inspection, the reaper's deadline, the stop's
teardowns, and the operator's retry and release of a loaded incident all
proceed. `All()` and serve's startup log show the loaded records — a
presence, never an absence.

**Nothing is destroyed to get there.** The loader leaves every unaccounted
entry exactly as found: `Problems` reports it and serve logs `UNSAFE STATE`.
Nothing deletes, rewrites or ignores it. Only the operator resolves it, with
the launcher stopped, and the next open classifies again.

**Why the open is not refused.** A refusal to open a partial inventory would
prove no absence either, but it would leave the loaded records unenforced —
their VMMs, their deadlines, their owners' destroys — until the operator's
repair. The open fences instead (control Z10: independent review 02's own
probe accepts a refusal; this correction's test does not).

**Consumers.** Each takes a refused answer as no proof. Each is tested
through the real core, wire and client over a partial inventory, the
launcher restarted over the same private files, with a control whose
inventory is complete:

- the daemon's reconcile (`Microvm.Orphans`, the list): the reconcile's
  error, no held slot freed (`TestAPartialInventoryFreesNoHeldSlot`);
- the Microvm's settling of a lost reserve answer (`recoverReservation`):
  the attempt retained without a launcher identity, never taken for nothing
  reserved (`TestALostReserveOnAPartialInventoryIsRetained`; since
  settled-admission ruling 01, by its request, whose settlement a partial
  inventory refuses as it refused the list: §11.10);
- the runner's release (`urgit-runner -recover`): refused, its retention
  kept (`TestRecoverReleasesNothingOnAPartialInventory`);
- the runner's start: its first reconcile's refused list starts nothing,
  and is not reported as an enrollment lost (`reconcileAtStart`: only the
  ship's 401 is one; `TestStartOnAPartialInventoryIsNoEnrollmentLoss`).
  Before this correction any first-reconcile error told the operator to
  re-enroll — advice that, taken, would leave the launcher's records of the
  old daemon identity behind;
- a stale caller's destroy: refused; its daemon retains;
- the launcher's CLI: `recover`'s inspection does not call a missing
  incident released, and its session does not call the directory free of
  incidents (`TestRecoverDoesNotCallAnUnaccountedIncidentReleased`); `list`
  prints the loaded records and fails on the unaccounted ones, as before.

`hello`'s budget reports the loaded records' use, a presence. While fenced it
may understate the use, and nothing takes its complement for free capacity:
the launcher admits nothing while fenced, and the runner's capacity is its
own configuration.

**Authority, crash and charge.**

- No authority is added.
- The fence is computed at every open and held in memory. It lifts only by
  an open that classifies every entry, after the operator's resolution —
  never by a reaper pass, and never by a healed barrier (§11.6's retry lifts
  only the directory's own fence).
- A record the service cannot read is not charged by it. But no capacity is
  admitted against it, and no consumer is told it is released.
- Quiescence is the lock's: the operator changes the records directory only
  with serve stopped (§8.4). An entry placed there while serve runs is not
  seen until the next open, which classifies it.

**Healthy controls kept.** With an authoritative inventory, a definite
absence is answered as before: an unknown id is `ErrUnknown` and its destroy
done; a stale destroy of a gone incarnation is done (idempotent); the list
is complete.

**Evidence.**

- `TestAPartialInventoryProvesNoAbsence` covers eight partial inventories:
  the target's own record truncated, of an unsupported format, naming
  another identity, not a regular file, or unreadable; another record
  truncated; a foreign entry; and two loaded records sharing a cid. In each:
  - the entry is reported;
  - the list is refused, direct and on the wire;
  - an absent id's destroy, inspection, create, stop and connect, a stale
    destroy, and an operator's selection, retry and release are refused;
  - no reservation is admitted;
  - the open succeeds, and a known guest's destroy still stops its VMM;
  - after a reaper pass, the entry is unaltered and the list still refused.

  One more case fails the records directory's barrier as well (§11.6). Its
  retry succeeds, and the partial inventory stays fenced. The control is an
  authoritative inventory: the list complete, before and after a reaper
  pass; a definite absence answered done and unknown; a stale destroy done;
  a released selection unknown.
- The consumers' tests above, each with its control.
- Independent review 02's two probes, byte-identical (the replay's
  `review02` run).
- Controls Z01–Z18, with Z01R, Z01C, Z01D, Z01N, Z01S, Z01T, Z02E, Z03R and
  Z04R. They include each unsafe fix the review names: suppressing
  Problems (Z11), deleting the bad record (Z12, Z13), returning an empty list
  (Z02E, Z03, Z14) and abandoning known guests (Z09, Z10). They also cover
  the transitions: a reaper pass (Z17) or a healed barrier (Z18) that
  lifts the fence.
- Y89N and Y90D: the daemon's and the runner's own guards, on a partial
  inventory. Y85 is re-anchored to `List`.

Adaptations:

- three accepted `List` calls in `service_test.go` take its new error (the
  inventory is authoritative in each);
- `launchertest.Server` gained `StateDir`, so a consumer's test can spoil a
  record between two lives.

The fixtures are private files spoiled between two lives, over the
nonexecuting host model. There is no real filesystem fault and no real
launcher process.

### 11.8 On-time cleanup, then durable accounting (late-accounting ruling 01)

This model was written before the code that implements it.

**The ruling.** Late-accounting ruling 01
(`.scratch/source-stage-01/orchestrator/late-accounting-ruling-01/RULING.md`,
sha256 `92977b69…1fb5`) ratifies QUESTIONS-SOURCE-01 §8, which is no longer
held.

- When physical cleanup is verified on time, capacity may return
  automatically once its accounting is durably confirmed — even if that
  confirmation comes late. Late bookkeeping is reported separately, its audit
  evidence kept, and the accounting is never claimed to have been on time.
- Automatic return requires durable, restart-usable evidence of:
  - the exact incarnation;
  - the original obligation;
  - the verified on-time physical completion;
  - the authorized accounting disposition.
- Capacity is withheld while confirmation is pending or uncertain.
- Nothing is authority to release on its own: not a missing record, not an
  incomplete inventory, not a successful directory barrier alone, and not
  timing kept in memory.
- A restart must not turn a pending, uncertain or withdrawn transition into
  capacity it has not earned.
- A (ruling A) is unchanged for late, failed, uncertain or already-quarantined
  cleanup.
- Nothing is added: no grace interval, retry budget, blocking allowance or
  new authority.

**Three instants, two proofs.**

| | What it is | Decides |
|---|---|---|
| D | the original obligation's deadline: the earliest trigger plus the allowance (§7.2), unchanged | the reference for both checks |
| T_c | the verified end of the physical cleanup: the last host effect returned, and nothing is left | on time iff T_c < D and nothing is left (the completion check, §11.3); otherwise A: quarantined |
| T_a | the accounting's durable confirmation: the record's withdrawal confirmed by the fsync of `attempts/` (or by a later retry, or by a reopen's certification of it) | late bookkeeping iff T_a ≥ D |

- **Proof 1, the release evidence E**, is written before any withdrawal: the
  record as released, with its **disposition**:
  - its exact incarnation and owner;
  - the obligation (its trigger and D);
  - T_c, and that nothing is held;
  - the authorized disposition: *released on timely cleanup*;
  - its accounting: *pending*.

  E is kept through the one publication path in `released/`, the evidence
  store of §8.3, which now holds every release, automatic or the operator's.
- **Proof 2, the accounting confirmation**, is the record's withdrawal made
  durable.

Only after both does capacity return: the entry is dropped (its charge given
back), and the owner's destroy is answered done. Then E is updated with T_a,
and with *late* when T_a ≥ D: the audit evidence of late bookkeeping.

**The steps, their durable images, and a crash at each.**

| Step | On disk after it | A reopen then |
|---|---|---|
| 1. `stopping` published | the record, `stopping` | quarantines it (`interruptedTeardown`). A: the cleanup's timing was only in memory |
| 2. the cleanup; T_c read | the same | the same |
| 3. E published (accounting pending) | the record `stopping`, and E | finds the record backed by E. It is loaded as a release whose accounting is pending, not quarantined, and the first reaper pass withdraws it: capacity returns, and the late bookkeeping (T_a after the restart) is reported |
| 4. the record unlinked | E; the record gone, maybe not durably | if the record is gone once `attempts/` is certified: released, answered from E. Its confirmation instant is unrecorded, so it counts as late. If the unlink was lost: as after step 3 |
| 5. `attempts/` fsynced: T_a | E; the record gone durably | the same as after step 4 |
| 6. E updated (T_a; late or not) | E, confirmed | released |

**Pending accounting.** It is charged, visible and retried; it is never a
quarantine and never a release. Each of these failures leaves the release
authorized, or authorizable, with its capacity withheld:

- E is not written (`notWritten`): pending. Its retry writes E from this
  life's T_c. A restart before that finds only the `stopping` record and
  quarantines it, because timing kept in memory is no authority.
- E is written, but `released/`'s fsync fails (uncertain): pending, and the
  retry certifies it.
- The unlink fails and the file remains: pending, and the retry unlinks it.
  **Changed by the ruling:** before it, this quarantined the record. Once E is
  durable, an incident for a committed release would contradict its evidence,
  and the ruling returns capacity for on-time cleanup when its accounting is
  confirmed, even late.
- The unlink acts, but its directory's fsync fails: pending, as before (§11.6).
- E's update after the confirmation fails: capacity has returned, because the
  accounting was confirmed. E stays *pending* on disk, and is read as late:
  truthful, and claiming nothing on time.

The retry runs at the triggers that already existed: the owner's destroy,
each reaper pass, the stop, and — for the operator's release — a retried
release. There is no new budget, grace or allowance. Meanwhile the record
stays listed (`stopping`, its reason naming what is pending), inspectable and
charged. After the physical deadline, it may stay so under stuck storage;
nothing is invented.

**What A still decides, unchanged.**

- A cleanup that began late, or finished at or after D, or left anything, or
  whose outcome is uncertain, is quarantined and charged: an incident.
- An already-quarantined incident is never reclassified or released
  automatically, whatever evidence exists. Only the operator's explicit
  release (§8.3) returns it, with its own evidence (disposition *operator
  release*).

**Absence is no authority; evidence is.** Every answer that returns capacity
downstream rests on E, never on an absence:

- **The destroy of an incarnation that is not loaded** — an unknown id, a
  stale incarnation of a held id, or a replay after a lost reply. It is
  answered done only when:
  - the inventory is authoritative (§§11.6, 11.7);
  - E exists for that exact incarnation and that owner.

  That is an authorized, committed release whose acknowledgment was lost.
  Otherwise it is refused: `ErrUnproven` ("no durable evidence of its
  release"), or the fence's error. Before the ruling, an authoritative
  absence was answered done.
- **A waiter whose record another operation released** (`errGone`): done,
  since every release drops its record only after E is durable.
- **The in-process bare `Destroy(owner, id)`** (tests only; no production
  caller): done when E exists for some incarnation of that id and owner.
- **`released`, a new query** (`Service.Released`; the wire op `released`)
  answers one of four things for an exact incarnation:
  - released: E, with its disposition, T_c, T_a and whether it was late;
  - pending: its accounting is not confirmed;
  - held;
  - no evidence.

  The runner's release and the daemon's reconcile ask it.
- **Inspect, create, connect and stop** of an id that is not loaded are still
  `ErrUnknown` on an authoritative inventory. Their consumers take them as no
  release.

**Restart.**

- The open certifies `attempts/` (§11.6) and not `released/`. *Refined
  during the implementation:* the model first had the open certify
  `released/` too, but no answer needs it.
  - A record is unlinked only after its E's publication has returned
    `written`, which includes `released/`'s fsync. So E is durable whenever
    its record is absent.
  - A `stopping` record backed by a visible E stays charged until its
    retry's `archive` finds E and fsyncs `released/` again, before any
    unlink. *Corrected by §11.9:* the code as submitted loaded such an E as
    already durable and skipped `archive`, and the first bullet's
    `written` never certified `released/`'s own link in the state
    directory.
- The loader looks up E for every `stopping` record:
  - backed by E (its exact incarnation, disposition *timely cleanup*): loaded
    as a release whose accounting is pending, finished by the first reaper
    pass;
  - no E: quarantined, as before.
- E with no record: released, answered from E. E keeps what it recorded; if
  its accounting still reads *pending* — the update after the confirmation
  failed, or a crash came between the two — then the confirmation instant
  was never recorded, and the release counts as late bookkeeping
  (`Release.Late`). *Refined during the implementation:* the model first had
  the open finalize such an E. Finding one would take reading every evidence
  file at every open, and `released/` grows with every job. The operator's
  `list` reads them all instead, on demand.
- E for a record loaded quarantined: nothing changes.
- An E that is unreadable, or names another incarnation, backs nothing.

**Late bookkeeping, reported apart.**

- An `Outcome` of kind `released` with `LateAccounting`, now also for an
  owner's destroy (before, only for the launcher's own teardowns). serve logs
  `RELEASED, ACCOUNTING CONFIRMED LATE`.
- The destroy reply's `late_accounting`.
- E's instants and its `late_accounting`.
- The operator's `urgit-vm-launcher list`, which logs every release in
  `released/` whose accounting was late or is still pending.
- The runner's inspection, which shows the launcher's evidence.

**Consumers (wire protocol 3).** A launcher that answers done only on
evidence, and a runner that asks for evidence, are updated together: each
refuses the other's older protocol at hello.

- **The daemon's teardown.** Done frees the slot, as before. `ErrNotDurable`
  (pending) and `ErrUnproven` retain it.
- **The daemon's reconcile.** A held orphan the launcher's list no longer shows
  is freed only when `released` proves its exact incarnation's release
  (`sandbox.ReleaseProver`, which the Microvm implements). Otherwise it stays
  held. A backend that cannot prove (Docker) keeps its own list's answer.
- **The runner's release of a microvm retention.** It is proven only by
  `released` for its exact incarnation. *(From here to the end of the next
  item: the reading settled-admission ruling 01 did not select, kept as it
  was. Since that ruling, an identity-less retention is an unsettled
  admission, released only on its request's settlement, and a lost reserve is
  settled by its request, never by a list: §11.10.)* An identity-less retention — a
  reserve whose answer was lost while the launcher could not be asked — is
  matched by its attempt, as before: an unratified proposal,
  QUESTIONS-SOURCE-01 §9. No physical resource can exist without a create,
  which names the token its owner never learnt. The absence of every
  reservation of the attempt, on an authoritative inventory, answers whether
  one had been admitted by the time of that look. The look is a snapshot:
  it proves nothing about an admission still in flight. *Clarified after
  independent review 03:* what settles such an admission is connected to
  this release. The release takes effect at the daemon's next start, whose
  first step, before any job, is a reconcile: it lists the launcher's
  records, and holds or destroys any it does not know. Every later
  reconcile pass does the same. Meanwhile the launcher charges an admitted
  reservation from its admission, and reaps it at its deadline.
- **The Microvm's lost reserve** (`recoverReservation`). Unchanged. Its look
  is such a snapshot too, and an admission after it is settled the same way:
  charged by the launcher, found by the daemon's next reconcile pass as an
  orphan, and reaped at its deadline if it is never created. This reading
  is also §9's.

**Evidence growth.** `released/` gains one file per release, and nothing
deletes from it (§8.3), so it grows with the jobs run. This is disclosed, and
its retention is QUESTIONS-SOURCE-01 §10.

**Expectations that depend on the ruling are kept apart.** Every test
expectation that the ruling changes is listed in RECORD ("Policy-dependent
expectations"), with the expectation before the ruling, the one after, and
why. The original bytes are kept in `accepted/` and `submissions/03/`. The
reviewers' probes are replayed unchanged. One that failed only because of
the ruling would be kept as historical evidence and said so — none is
expected to.

**Evidence.**

- `accounting_test.go` covers the cases the ruling's verification names:
  - timely cleanup with late confirmed accounting, and its in-time control;
  - an owner's late bookkeeping reported apart;
  - no withdrawal before the evidence is durable, including a failure after
    an effect;
  - late or failed cleanup leaving no evidence;
  - each durable image across a restart;
  - a still-failing barrier against a repaired one;
  - a lost reply against a purported withdrawal;
  - stale and incarnation-confused calls;
  - an incident never released by evidence;
  - protocol 2 refused.
- The consumers' tests:
  - the daemon's held orphan, freed only on proof;
  - the Microvm's proof;
  - the runner's release, on evidence only, with a late bookkeeping shown;
  - the operator's views.
- The ten policy-dependent expectations P1–P10, recorded in RECORD.
- Every reviewer's probe, replayed byte-identical: all pass.
- The L series: 21 controls between L01 and L22, one per new guard or
  consumer.
- The re-anchored controls, whose seams the implementation moved: round
  three's V06, Q06, Q07, Q02, Q02b, V17, Q11 and Q11b; the stage's N01, N02,
  T09, X12, X12F, X14, Y19, Y65–Y69B, Y84, Z05, Y83, Z04, Z04R and Z09.
- After the first trial, some controls depended on the ruling's own
  defense in depth, and were adjusted:
  - The runner's release of a known identity is refused by the evidence
    too, so R02 and Y30 now decide only the refusal's guidance.
  - Y90 and Y90D are seen through an identity-less retention, whose only
    proof the list still is. (Retired by settled-admission ruling 01: §11.10.)
  - Independent review 02's consumer probe fails only with both the fence
    and the evidence requirement off, so Z01C is that two-edit mutation.

The tests use fresh service objects over the same private files, and inject
their faults before or after the syscall: not a power loss, and not a real
launcher process.

### 11.9 The evidence namespace is certified before it authorizes anything (independent review 03)

This model was written before the code that implements it.

**What was wrong.** Independent review 03
(`.scratch/source-stage-01/independent-reviews/integration-03/REVIEW.md`,
sha256 `6f813186…e8c6`, with its probe `review_archive_parent_test.go`,
sha256 `727b52a9…1e4a`) found R3-1. `archive` made `released/` and fsynced
its parent — the state directory — only when its mkdir succeeded. When that
fsync failed, the release stayed pending, as it should. The retry's mkdir
then met EEXIST and skipped the barrier. It published the evidence in a
directory whose own link was never certified, withdrew the record and
returned the capacity. EEXIST is an observation, not a durability
certificate.

Tracing the whole namespace for this correction found the same fault in two
more places:

- **The reopen.** A `stopping` record backed by a visible E was loaded with
  E taken as durable, so its first retry withdrew the record without
  certifying E. §11.8 said that retry's `archive` would fsync `released/`
  again, but the code skipped `archive` for these entries. E may have been
  visible only: published by an earlier life whose `released/` fsync failed.
- **A `released/` that is not a directory.** `archive` followed a symlink
  there and kept E outside the state directory, where no barrier of the
  launcher's reaches. The readers followed it too.

**Four links, each certified by the process that relies on it.** E
authorizes a withdrawal only when this process has certified every link from
the state directory down to E's bytes. Each link is certified by a barrier
issued after the thing it certifies was seen:

| Link | Certified by | Never by |
|---|---|---|
| the state directory's name in its parent | every open (§11.6) | — |
| `released/`'s name in the state directory | an fsync of the state directory issued after `released/` was seen as a real directory, by each evidence keeping | mkdir's EEXIST; an earlier attempt, failed or not; an open's certification of the state directory from before `released/` existed |
| E's name in `released/` | an fsync of `released/` after E was renamed into it, or after E was found there | the rename's success |
| E's bytes | their fsync before E's rename (the one publication path); for an E found already there, an fsync of E itself | E's visibility |

**The rule.**

- **Keeping evidence** (`archive`: a timely cleanup's release and the
  operator's) makes `released/`, where EEXIST is fine. `released/` must then
  be a real directory: anything else refuses the release, and nothing is
  written through it. Then it fsyncs the state directory. Then it either
  publishes E (written, synced, renamed, `released/` fsynced), or, when E is
  there already and is this incarnation's evidence, fsyncs E and then
  `released/`.
- **Any failure** leaves a timely cleanup's release authorized and pending:
  charged, listed, reported `unconfirmed`, and retried at the triggers that
  already exist. The operator's release is not done, and the record is still
  an incident.
- **Every evidence keeping certifies again.** The state directory's fsync is
  issued each time, never remembered: EEXIST names a directory, not the one
  an earlier barrier certified, and a remembered certification would be
  state shared by concurrent releases. The cost is one fsync per evidence
  keeping. Once a keeping succeeds, this process's retries of the withdrawal
  do not repeat it.
- **The reopen.** A `stopping` record backed by a visible, timely E is still
  loaded as a pending release, charged either way. But E is not taken as
  durable: its first retry keeps it, which certifies it, before anything is
  withdrawn. A power loss before that leaves the next open without E, which
  quarantines the record (A).
- **The readers.** `Released`, the destroy of an incarnation not held, the
  bare `Destroy`, the loader and the operator's views read E only through a
  real `released/` directory. Through anything else, E proves nothing: the
  answer is `ErrUnproven`, the loader quarantines, and `list` reports it. The
  answers need no barrier of their own. A record is withdrawn only after its
  E was certified, so an authoritative absence (§§11.6, 11.7) always has
  certified evidence behind it. A record still held — its accounting
  pending — is answered `ErrNotDurable`.
- **`rearchive`**, the confirmation's update after the withdrawal, needs no
  new certification. `released/`'s link was certified before the
  withdrawal, and E's replacement goes through the same publication path.
  Whichever version survives a crash is valid evidence.

**The transitions.**

| Case | As submitted | Now |
|---|---|---|
| first creation, the state directory's fsync fails | pending | pending |
| its retry: EEXIST, the fsync still failing | **released** (R3-1) | pending; each retry issues the fsync again |
| its retry once the storage is repaired | released | released, after the state directory's fsync |
| `released/` replaced after an earlier certification, the fsync failing | **released** | pending |
| restart: E visible, `released/`'s fsync failing | **released** by the first reaper pass | pending until E is certified |
| restart: E visible, the state directory's fsync failing after the open | **released** | pending |
| restart: E visible, E's own fsync failing | **released** | pending |
| its keeping failed before E was written, then a crash and a restart | incident (A) | incident (A) |
| its keeping failed before E was written, the stop's retry failing too, then a restart | **released** at the stop (R3-1) | pending at the stop; an incident (A) after the restart |
| `released/` a symlink | E kept through it; **released** | pending; nothing written through it; no reader proves anything through it |
| the operator's release, the fsync failing at two attempts | refused, then **released** | refused at each, still an incident |

**Authority, charge, crash.**

- Nothing is added: no authority, grace, retry budget, delay or trigger.
  The ruling's proof requirement is kept whole: durable, restart-usable
  evidence before capacity returns.
- A failing barrier keeps the release pending. It stays charged, and its
  known execution stays enforced as before — its cleanup had already been
  verified complete. It is retried at each reaper pass. Under a persistent
  failure it stays so, as §11.8 says of stuck storage.
- A crash before the certification leaves one of two images. Either the
  record is `stopping` with a visible E: loaded pending, and certified
  before any withdrawal. Or there is no E: quarantined (A).
- The records namespace's rule (§11.6) is unchanged. The evidence namespace
  now follows the same principle: visibility is an observation, and a barrier
  issued after it is the proof.

**Evidence.**

- **Independent review 03's probe, byte-identical.** On the submitted bytes
  it failed as the reviewer found — its still-failing case on its named
  assertion, its repaired-parent case passing (`evidence/review03-repro/`) —
  and it passes now (the replay's `review03` and `all` runs). The
  reviewer's diagnostic `store.go`, a retried parent fsync, was run only to
  reproduce the reviewer's control. It is not this correction.
- **`namespace_test.go`**, 4 tests and 14 subtests:
  - every evidence keeping certifies `released/`'s link. A barrier sees
    `released/` as a real directory when the state directory's fsync runs,
    before E's rename and before the unlink: at the first creation; at
    every retry while the fsync still fails — the reaper's three and its
    owner's, EEXIST notwithstanding — and then after the repair; with
    `released/` replaced after an earlier certification; and for the
    operator's release, refused at each attempt. E found already there has
    its name certified again at every retry — the reaper's, its owner's and
    the stop's;
  - a reopen. E was visible but never certified in its first life, which
    ended as a crash would. With `released/`'s, the state directory's or
    E's own fsync failing, the release stays pending; with every barrier
    holding, E is certified before the unlink. A keeping that failed before
    E was written, retried at the stop, is an incident after the restart;
  - a real directory. A symlink or a regular file at `released/`'s name
    keeps the release pending, and nothing is kept through it. `released`, a
    replayed destroy, the bare destroy, the operator's views and the loader
    prove nothing through a symlink;
  - the wire. A replayed destroy and `released` are refused at every retry,
    the release listed and charged, and released after the repair.
- On the submitted bytes, 11 of the 14 subtests fail on their named
  assertions. The other three — the first creation, E found already there,
  a regular file — held already; their controls show they can fail.
- Controls E01–E10, with E01R (the reviewer's probe), E01W, E01O, E01S,
  E03S, E03B, E03C, E05R, E07F and E08L: 20.
- One expectation changed, not by policy: the release's trace in
  `TestReleaseIsUnlinkThenSyncDir` now begins with `released/` seen and the
  state directory fsynced, at every keeping (RECORD).

The tests use fresh service objects over the same private files, with each
fault injected before its syscall: not a power loss, and not a real
filesystem's failure.

### 11.10 A reserve request is settled before its capacity returns (settled-admission ruling 01)

This model was written before the code that implements it.

**The ruling.** Settled-admission ruling 01
(`.scratch/source-stage-01/orchestrator/settled-admission-ruling-01/RULING.md`,
sha256 `90778c1f…ec76`) decides QUESTIONS-SOURCE-01 §9. §10 stays undecided.

- After a lost reservation reply, an empty authoritative inventory alone
  cannot return the runner's slot while the original request may still be
  admitted. The capacity stays withheld until the original request is
  conclusively settled.
- Settlement either identifies the exact admitted reservation, for
  ownership-scoped resolution, or establishes a definitive rejection, an
  enforceable cancellation or an enforceable expiry that prevents that
  request from ever creating a reservation. A caller's timeout, a closed
  transport, an elapsed wait or a successful list is no such guarantee.
- Settlement survives restarts and lost settlement replies. Once the
  no-reservation outcome and the accounting are durable, the capacity returns
  automatically: no operator, and no cleanup evidence for a VM that never
  existed.
- An identity found releases nothing. An admitted reservation, and any
  incident, follows its actual disposition: A, and Q8. No quarantine is
  reclassified or cleared.
- Relying on a later reconcile to find an admission that follows an empty
  inventory — this source's earlier reading — is not the selected behaviour.

**What was wrong.** The source accepted by independent review 04 took an
authoritative empty inventory for settlement in two places:

- the Microvm's lost reserve (`recoverReservation`): a list without the
  attempt meant "the reserve made nothing", and the slot was freed at once;
- the runner's operator release of an identity-less retention (`inspect`): a
  list without the attempt allowed the release.

And the daemon kept no durable trace of a reserve in flight: a crash between
sending a reserve and learning its outcome left nothing withheld at the next
start. The launcher handles a reserve on the caller's connection and finishes
it whether the caller is still waiting or not. It may admit the request after
the caller gave up, the answer then lost; or it may reach the request only
after the caller has looked at the list.

**The request token.** Every reserve names a request token R: 32 lowercase
hex, random, chosen by the daemon, bound to (owner, attempt). The daemon
records the admission — its attempt and R — durably before it sends the
reserve. The launcher admits a request at most once, ever, and settles it on
demand. A reserve on the wire must name R (wire protocol 4). An in-process
`Reserve` that names none is a test-only admission that can never be settled.

**The launcher's request ledger**, `<state>/requests/<R>.json`, one file per
request, never deleted:

- written at admission, before the record is published: R, the attempt, the
  owner, and the reservation it admitted (id, token, cid, creation time). The
  reserve is acknowledged only once both the ledger entry and the record are
  durable;
- written when a request the launcher never saw is settled: R, the attempt,
  the owner, *closed*;
- kept as §11.9 keeps evidence: through the one publication path, in a real
  directory whose link is certified at every keeping. Read only through a
  real directory.

**Admission** (`Reserve`, under the service lock). A request is refused —
`ErrSettled`, the reserve answered with the refusal — if this process has
begun settling R, if a held record carries R, or if the ledger holds R,
admitted or closed.

**Settlement** (`Service.Settle(owner, attempt, R)`; the wire op `settle`)
answers exactly one of:

- **admitted**: a held record carries R — the owner's, for that attempt: its
  identity and its state. A reserve still publishing that record is waited
  for, never answered from. The caller resolves the reservation by its
  identity.
- **released**: the ledger says R admitted a reservation that is no longer
  held, and that reservation's durable evidence proves its release (§11.8):
  its disposition.
- **closed**: R holds nothing and never will. Either the ledger says it was
  closed; or it says admitted, but the reservation was never acknowledged —
  no record, no evidence; or R was never seen, and the launcher closes it now,
  the ledger's *closed* entry durable before the answer.

Or it refuses: another owner's request, or another attempt's (a stale or
confused caller); an inventory that is not authoritative (§§11.6, 11.7); a
namespace that is not a real directory; a closure or certification that is not
durable (`ErrNotDurable`). A refusal closes nothing and releases nothing.
Every *closed* or *released* answer rests on durable state that the answering
process has certified (§11.9's rule).

**Why this is conclusive.** Admission and settlement decide under the same
lock: the service's mutex in one process, and across processes the state
lock, which no terminal call gives up while a settlement runs (§11.11). A request not yet admitted when a settle decides is closed before the
answer, durably, so its admission is refused whenever it is processed. A
request admitted before is found: held, or by its evidence. A replay of R is
refused forever by the ledger. No timeout, transport, elapsed wait or list
decides anything.

**The daemon's runtime recovery.**

- **Before a reserve**, an admission entry — the handle, the attempt, R and
  the backend — goes into the state file as a `Quarantine` whose `Request`
  is R, saved durably. If that save is not durable, no reserve is sent. While
  the admission's attempt is being prepared in this process, the running slot
  stands for it: it is not counted twice.
- **The Prepare's outcome settles it at once where it can:**
  - acknowledged: removed. The reservation is the running attempt's, and a
    crash from here leaves an orphan that the first reconcile finds;
  - refused by the launcher: settled too, at once — a refusal is kept
    nowhere, so a replayed or delayed copy of R could still be admitted —
    and removed once settled; the refusal's reason stays the error's.
    *Corrected during the implementation: this said "removed", which
    contradicted "a replay of R is refused forever by the ledger" below — a
    refused R has no ledger entry until it is settled;*
  - its answer lost: the Microvm settles R at once. *Admitted*: rolled back by
    its identity, as before. *Closed* or *released*: removed. The settle
    fails: the admission stays, now withholding its slot.
- **Each reconcile** — the first before any job — settles every admission
  that no running Prepare covers:
  - *closed* or *released*: removed. Its slot returns; while the removal is
    not durable, no new work starts (§5);
  - *admitted*: the reservation is held as an orphan, its slot withheld, and
    the orphan logic resolves it by its actual disposition: the ship's
    status, then destroy or hold; a quarantine stays the launcher's incident,
    and the daemon's retention (A);
  - refused: kept, and settled again at the next pass.
- **A restart** finds every admission in the state file, withholds its slot,
  and settles it before any capacity is advertised.

**The operator's path** (`urgit-runner -recover`). An admission is listed and
selected with its request (a selection names it). Its inspection reads the
launcher's list for a held record carrying R: a presence, never proof of an
absence. Its release (daemon stopped) settles R: *closed* or *released* allow
the release; *admitted* refuses it and names the reservation. An identity-less
entry recorded without a request — this source writes none — is never released
on an absence.

**The durable images, and a crash at each.**

| Step | Durable image | After a crash |
|---|---|---|
| the daemon records the admission | the state file holds admission R | restart: withheld; settle R → closed (it was never sent) |
| the launcher admits R | + the ledger's entry *admitted*, then the record | restart: settle → admitted → held, the orphan logic |
| the ledger entry written, the record's publication failed | the ledger's *admitted*; no record | settle → closed (never acknowledged) |
| the answer lost; the settle closes R | the ledger's *closed* | a replay of R is refused; settle again → closed |
| the launcher refuses R; the settle closes R | the ledger's *closed* | the same: the refusal is now kept, as a closure |
| the daemon removes the admission | the state file without it | the slot is free |

**Authority, charge and budgets.** Nothing is added to what the launcher may
do to a host, and no budget, slot, deadline or grace is granted. The launcher
charges exactly what it holds. The daemon withholds one slot per unsettled
admission not covered by a running Prepare, once — never twice across a
retry, a failed or uncertain save, or a restart. Settlement is idempotent: a
lost answer is asked again.

**Growth.** `requests/` gains one small file per reserve, and nothing deletes
from it (QUESTIONS-SOURCE-01 §10, undecided). Pruning an entry would make its
request admissible again. A later retention policy must keep every entry whose
request could still be replayed.

**What the implementation added to the model.**

- A launcher's *refusal* of a reserve is settled too, before its slot
  returns: the refusal reserved nothing on its delivery, but nothing durable
  stops a replayed or delayed copy of the request from being admitted later
  (the model's daemon bullet is corrected above). The Microvm settles a
  refused request as it settles a lost answer.
- A request is refused admission from its first settle on, in this process,
  even while its closure is not yet durable (`Service.requests`); a restart
  then finds it unseen, and whatever a later settlement finds decides — the
  owner never returned the slot meanwhile.
- Every settle of one request waits for another in progress
  (`Service.settling`), and two settles of one unseen request agree.
- The ledger entry is published through the store's one publication path
  (`publishBytes`, which `publishAt` now calls), and its request token is a
  validated field of the record (`validateRecord`).
- An entry recorded without a request — the pre-ruling identity-less
  retention — is never released on an absence: nothing can settle it
  (`inspect` says so). This source writes none. While the launcher holds its
  attempt, the refusal names that reservation, which follows its own
  disposition there; it no longer promises a release after the launcher's
  (corrected after full-11). How such a pre-ruling entry may ever be
  released is QUESTIONS-SOURCE-01 §11, open.

**Evidence.**

- The launcher (`internal/launcher/settle_test.go`, 9 tests), through the
  real core over private files, a restart a fresh service over the same
  files:
  - a delayed admission crossing an empty list, refused once settled — in
    this life and after a restart — while a request never settled is
    admitted (`TestADelayedAdmissionIsRefusedOnceItsRequestIsSettled`);
  - accepted but its reply lost: found exactly, as often as asked, across a
    restart; charged once; a replay refused, before and after its release —
    the ledger alone refusing it after — and released on its evidence
    (`TestAnAdmittedRequestWhoseReplyWasLostIsFoundExactly`);
  - one admission while its publication is in progress: the same token for
    another attempt refused (`TestARequestIsAdmittedOnceWhileItsAdmissionPublishes`);
  - a settle waits for a reserve still publishing, acknowledged or not
    (`TestASettlementWaitsForItsRequestsPublication`);
  - durability: the closure's rename failing before and after it acted, the
    ledger's and the state directory's fsyncs failing, an entry found and
    not yet certifiable (its own fsync, `requests/`'s), a restart before the
    retry, the admission's own entry failing before and after it acted, and
    the record failing after its entry
    (`TestASettlementIsDurableBeforeItsAnswer`);
  - binding: another owner, another attempt, competing requests of one
    attempt, two settles of one request at once
    (`TestSettlementIsBoundToItsRequestAttemptAndOwner`);
  - a partial inventory settles no absence; a `requests/` that is a symlink
    is neither read nor written through
    (`TestSettlementNeverRestsOnAnUnprovenAbsence`);
  - an incident stays the operator's, and late bookkeeping stays reported
    (`TestSettlementLeavesEveryDispositionAsItIs`: A and Q8);
  - the wire: a reserve without a request refused, the client naming one,
    `settle` answering as in process, protocol 3 refused
    (`TestTheWireSettlesRequests`).
- The daemon (`internal/daemon/settle_test.go`, 8 tests), through the real
  Microvm, the launcher's real core, wire and client, a proxy on the wire
  that holds, loses or drops the answer to one request, the in-process ship
  and the real state file:
  - a delayed admission crossing an empty list: settled first, the slot
    returns and the delayed request is refused; its settlement unanswered —
    at the Prepare, and at a reconcile too — the slot stays withheld, the
    delayed request is admitted and charged once on both sides, and a
    reconcile settles it; settled admitted with its list then lost, the
    reservation is held in the admission's place
    (`TestADelayedAdmissionNeverOutlivesItsSettlement`);
  - accepted but its answer lost: rolled back by its identity, one reserve,
    or retained under that identity when the rollback's answer is lost too
    (`TestAnAdmittedRequestWhoseAnswerWasLostIsResolvedByItsIdentity`);
  - never admitted: closed, the slot returning by itself, no cleanup
    evidence (`TestANeverAdmittedRequestReturnsItsSlotByItself`);
  - refused: settled before its slot returns, a replay refused however much
    the launcher freed; its settlement unanswered, withheld, a replay
    admitted meanwhile settled at the reconcile
    (`TestARefusedRequestIsSettledBeforeItsSlotReturns`);
  - every slot withheld by an admission never stops the daemon: Run settles
    it (`TestAnUnsettledAdmissionNeverStopsTheDaemon`);
  - a lost settlement, then both restarts: withheld from the start, settled
    once, repeated reconciles changing nothing
    (`TestSettlementSurvivesALostAnswerAndARestart`);
  - the admission durable before its reserve — a save failing before or
    after its effect sends none; its removal unsaved comes back after a
    restart, counted once; a running Prepare's admission counted once
    (`TestAnAdmissionIsDurableBeforeItsReserve`);
  - A and Q8 at the daemon (`TestSettlementLeavesADispositionAsItIs`).
- The Microvm against a scripted launcher: a refused reserve settled —
  closed, a copy admitted meanwhile, unanswered, refused
  (`internal/sandbox/settle_test.go`).
- The runner's state: an admission is its request only, and a selection
  names it (`internal/state/admission_test.go`).
- The operator's path: released only on its settlement — never admitted,
  admitted then released, admitted and held, a partial inventory, a
  launcher that cannot be asked — each release's request closed for good (a
  replay refused), its inspection settling nothing, its retry refused
  (`cmd/urgit-runner/settle_test.go`).
- **RED on the source the ruling rules on** (submission 05, preserved; the
  probes written against its API, run on a fresh copy, never in the tree):
  the daemon returned the slot on the empty list and the delayed request
  was then admitted, the daemon offering both slots; the runner released an
  identity-less retention on the empty list and a late reserve was then
  admitted (RECORD, "RED before GREEN").
- **Expectations the ruling changed**, kept apart in RECORD with their
  diffs: the Microvm's lost reserve settled by its request (S1, S2), and an
  entry without its request no longer released on an absence (S3); three
  daemon fixtures and one launcher fixture adapted to the admission saved
  and the ledger renamed before the record.
- **Controls**: the H series, 46, one per guard — the ledger, the
  in-process and held-record refusals, the entry before the record, the
  settle's wait, fence, owner and attempt, the closure's and every found
  entry's certification, the wire, protocol 3, the Microvm's settlement of
  a lost or refused answer (the ruling's rejected reading restored: H20,
  H20D, H20S), the daemon's admission, covering, withholding, reconcile,
  restart and exit, the state's identity and selection, and the operator's
  release, inspection and older entries. Fifteen controls re-anchored on the
  moved code; Y90 and Y90D retired (no release path reads the list for
  proof). trial-20: 60 of 61 held; H03 was caught one check early, its test
  now checks the named observation first; trial-21: H03 holds. full-11
  held 415 of 418: three controls whose anchors had not moved no longer saw
  their guard — a Prepare's retention moved into `resolveAdmission` (G01
  re-anchored; G01D keeps the old line, for a backend without settlement),
  a daemon fixture's premise changed (G04: the admission's save now comes
  first), and S3's refusal outran X37's (its test now checks the
  reservation the refusal names). trial-22 ran every daemon and runner
  control; full-12 runs every control on the final bytes (RECORD).

The tests use fresh objects over the same private files, faults injected
before or after the syscall, and a proxy on a private socket: not a power
loss, not a real process, not a real host.

### 11.11 A settlement ends before the state is handed on (independent review 05)

This model was written before the code that implements it.

**What was wrong.** Independent review 05
(`.scratch/source-stage-01/independent-reviews/integration-05/REVIEW.md`,
sha256 `54d0837f…03ec`) found R5-1. A settlement of a request no record
carries (`settleUnheld`: a closure, or the certification of an entry found)
wrote and fsynced the ledger outside every record's token, and no terminal
drain counted it. `Close` and `Shutdown` released the state lock while it
ran. A second service could then open the same state and admit the same
request, and the first settlement's rename replaced that admission's ledger
entry. It answered *closed* while the new owner held the reservation. The
ledger's "never overwrite" is an `lstat` and then a rename, two steps that
only the state lock makes one.

**The rule.** Every write or fsync a service makes to its state happens
while it owns the state lock, and is finished before a terminal call gives
the lock up. A record's token already gives this to every operation on a
record. Work that holds no record's token is counted (`Service.unheld`)
instead, from its admission to its end, under the mutex that decides
admission. There are two such kinds: an unheld settlement's closure or
certification, and the records directory's recertification (§11.6).

- It begins only while the service is not closing: `Settle` refuses with
  `ErrClosed`, and `recertify` does nothing, once `BeginStop`, `Close` or
  `Shutdown` has begun. The count is taken in the critical section that
  checked this.
- A waiting settlement — behind a reserve still publishing its record, or
  another settlement of its request — wakes at the terminal call's
  broadcast and is refused. It never begins a publication after the
  terminal call began.
- The count is given back on every path: an answer, a refusal, an error.
- `Close` gives the lock up only once every token is back and the count is
  zero. It waits for both as it always waited for tokens, with no deadline
  of its own.
- `Shutdown` drains the operations in progress but teardowns, runs its
  teardowns, and gives the lock up only once every token is back and the
  count is zero. The count delays no teardown: a settlement and a
  recertification touch no record. The caller's deadline bounds this last
  wait, as it bounds the first drain. If the deadline passes with counted
  work still in progress, `Shutdown` returns that count as its error and
  keeps the lock. The state is not handed on while it may still be
  written; the process's exit, or a later `Close`, gives the lock up.

**Nothing is granted, and nothing is abandoned.** No deadline is added to a
settlement (a durable publication was never bounded by a deadline in source:
§7.2), none is lengthened, and no teardown waits for a settlement. The
terminal calls keep their own bounds: none for `Close`, the caller's for
`Shutdown`, and for serve's stop the unit's `TimeoutStopSec` as the outer
backstop. The mutex is held only to take and give back the count; the
filesystem work runs outside it, as before. A settlement in progress is
waited for, never cut short.

**The server's stop.** `Server.Serve` returns once its listener closes,
without joining the connections' handlers. serve then cancels the reaper
without joining it, calls `BeginStop`, and calls `Shutdown`:
- a handler's settlement in progress is counted, so `Shutdown` waits for it
  before the lock goes;
- a handler's later call is refused (`ErrClosed`);
- a reaper pass's recertification in progress is counted too, and its
  teardowns hold tokens.

A reply a handler writes afterwards describes a state that was durable, and
still in this service's hands, when it was decided.

**What a caller sees.** A settlement that answers *closed* made its closure
durable before the answer, and while this service held the state. The next
owner loads the ledger and refuses the request. A settlement cut by the stop
answers `ErrClosed` (it never began) or its own error, and settles nothing:
its caller asks again.

**Evidence.**

- **Reproduced first, on the submitted bytes** (`evidence/review05-repro/`).
  The reviewer's two probes, byte-identical by `-overlay`, fail on their
  named assertions: `Close` and `Shutdown` returned, and a second service
  opened, while a closure was held; and *closed* was answered over a new
  owner's admission. With the reviewer's diagnostic, both pass.
- **The tests, RED against the unchanged product code**
  (`evidence/red-review05/`): 6 of the 8 tests, with 11 subtests, fail on
  their named assertions. The two that pass guard behaviour the old code
  had already (a waiting settlement refused at the stop), or that only a
  counted settlement can show (its count given back on an error). Their
  controls show they can fail.
- **`settle_terminal_test.go`, 8 tests**, through the real core over private
  files, with a fault holding one syscall:
  - every terminal path (`Close`, `Shutdown`, `BeginStop` then `Shutdown`)
    at every stage of a settlement's publication — the closure's rename,
    `requests/`'s link, a found entry's certification. The call does not
    return, and no second service opens, until the settlement ends; it then
    answers closed, and the next owner refuses the request
    (`TestASettlementEndsBeforeTheStateIsHandedOn`, 9 subtests);
  - several settlements in progress: the last one is waited for
    (`TestEverySettlementInProgressIsWaitedFor`);
  - Shutdown's teardowns proceed while a settlement is held
    (`TestShutdownsTeardownsNeverWaitForASettlement`);
  - Shutdown's deadline: an error, the state kept, then a later Close
    (`TestShutdownKeepsTheStatePastItsDeadlineWhileASettlementRuns`);
  - no settlement begins once the service stops: a new one, one waiting for
    another of its request, one waiting for a reserve still publishing
    (`TestNoSettlementBeginsOnceTheServiceStops`);
  - a failed settlement gives the state back
    (`TestAFailedSettlementGivesTheStateBack`);
  - the records directory's recertification is waited for, and none begins
    after the stop (`TestARecertificationEndsBeforeTheStateIsHandedOn`);
  - serve's stop order on the real wire, with the production client: the
    listener closed, `Serve` returned, `BeginStop`, `Shutdown`. The client's
    settlement is waited for and answered closed, and a call on a
    connection the stop found open is refused
    (`TestServeStopWaitsForASettlementOnTheWire`).
- **The reviewer's two probes, byte-identical, pass**: the replay's
  `review05` run, and every probe together in `all`.
- **Controls: the K series, 15.** They cover Close's and Shutdown's waits
  (each seen also by the reviewer's probes and on the wire), the
  settlement's and the recertification's counts, a count kept on an error,
  settlements counted as one, teardowns delayed, Shutdown's deadline kept
  and the state kept past it, and nothing begun after the stop. K04 restores
  the review's diagnostic, which drained settlements before the teardowns.
  Y87 and Z18 are re-anchored on `recertify`'s new end. trial-23: all 17
  RED/GREEN. full-13 runs every control on the final bytes (RECORD).

The tests hold one syscall of private files and run a second service over
them: not a power loss, not a real process, not a real host.

### 11.12 A legacy retention is released from Urgit, on its evidence (legacy-recovery UI ruling 01)

This model was written before the code that implements it.

**The ruling.** QUESTIONS-SOURCE-01 §11 asked whether a retention recorded
without its reserve request, by a runner before settled admission (§11.10),
may ever be released. Its recommendation was a release on the launcher's
authoritative absence of every reservation of its attempt, the launcher
speaking protocol 4. It was approved with one condition: "as long as the
command can come from the UI in urgit" (legacy-recovery UI ruling 01,
`.scratch/source-stage-01/orchestrator/legacy-recovery-ui-01/RULING.md`,
sha256 `d0130da0…5ff3`). The origin in Urgit is part of the decision. The
ruling also requires:
- positive provenance;
- an exclusion no later admission can defeat;
- the exact target and evidence bound to the confirmation, and checked again
  when it runs;
- a durable result;
- one connected, authenticated path through the owner of the runner's state.

**Which entries.** A *legacy retention* is an entry of the runner's state
file that meets both of these:
- **Its shape.** It withholds a microvm slot (`backend` microvm), and names
  neither a launcher identity (`vm`) nor a reserve request (`request`). A
  runner before settled admission wrote this when a reserve's answer was lost
  and the launcher could not be asked.
- **Its mark.** It carries the **legacy mark**: it was in the file when a
  runner of this version first loaded it, and an earlier runner had last
  written the file.

The mark is the positive provenance:
- Every save by this version stamps the file with its format (`format` 1).
  A file without the stamp was last written by an earlier runner.
- Loading such a file marks each entry of that shape (`legacy`: the file's
  format, and when it was found). Its revision moves, since the entry
  changed.
- The next save makes the marks and the stamp durable together, in one
  atomic write. A file of this format never gains a mark.

So a mark says the entry predates this version. The shape says more: no
runner that has settled admission writes it. Every reserve is sent with its
request recorded first (§11.10), and every other microvm retention names its
launcher identity (§§3, 11.1). A test checks this over every path that
retains a microvm slot. Between them, a marked entry was written by a runner
before settled admission.

A missing request alone proves nothing, and marks nothing: an entry of the
same shape in a file of this format is not legacy, and stays withheld.

Not covered, and withheld as before:
- an entry recorded before stage 01, which names only its handle, reason and
  time, and no backend. It may be a Docker sandbox of an earlier
  configuration, and the launcher's inventory cannot prove a Docker sandbox
  released;
- an admission (§11.10): it is settled by its request;
- an entry naming a launcher identity: it is released on the launcher's
  evidence of that incarnation's release (§11.8);
- a Docker retention (§11.4).

**Why no old admission can complete later.** The old request named no
token. The launcher asked is the one this runner is configured for. It
must, at the time of the release:
1. **Speak wire protocol 4.** The client refuses any other at `hello`. The
   server refuses every reserve on the wire that names no request token
   (`proto.go`, `reserve`), and nothing else admits a reservation in
   production. A protocol-3 or earlier runner's client refuses this launcher
   at `hello` and sends nothing.
2. **Hold its state directory's exclusive lock.** Any launcher process that
   could still have been handling the old request has exited, and every
   record it left is loaded.
3. **Answer its list only when its inventory is authoritative.** Its records
   directory is certified and every entry is accounted for (§§11.6–11.7). A
   partial inventory, an unreadable entry or an uncertified directory is a
   refusal, never a short list.

If that list shows no record of the entry's attempt, in any state, no
reservation of it exists now. None can be made from its request later: the
request cannot be admitted, and no process is left to send it. The runner
holds its own state file's lock, so no earlier life of it runs either.

This is a definitive rejection by protocol, which settled-admission ruling
01 counts as settlement. It is not a timeout, a closed transport, an elapsed
wait or an empty snapshot alone.

Like every settlement since §11.10, it rests on the configured launcher
being this runner's launcher: a runner moved to another launcher would have
its admissions "closed" there too. The list is owner-scoped (uid and daemon
id), as every reconcile and settlement is.

**What a legacy release never does.**
- It stops, destroys, retries or reclassifies nothing at the launcher. It
  asks the launcher only `hello` and `list`.
- A record of the attempt, in any state, refuses it. That record follows its
  own disposition: the daemon's reconcile, or the launcher operator's
  `recover` for an incident.
- It writes and claims no cleanup evidence. The released entry records the
  evidence the release stood on, and the command that asked for it.
- It revives no request: it sends no reserve.
- It is never automatic.

**The path.**

1. **The daemon reports** its retentions to the ship. It posts to
   `POST ci/daemon/<id>/retentions` with its bearer, at start, after every
   reconcile, and after every recovery command. The report lists every
   retention with:
   - its selection, revision, job label, attempt, backend, reason and time;
   - how its slot returns: from Urgit, by settlement, with
     `urgit-runner -recover`, or not at all;
   - for a legacy retention, the conditions below, each met or not, with
     their facts and the evidence digest;
   - the runner's capacity: configured, withheld, held, running and
     advertised.

   The report is for display and binding. It authorizes nothing.
2. **The operator inspects and confirms** in Urgit (Settings → Runners → the
   runner → Retentions).
   - The panel lists the retentions by job, attempt and time. For each it
     shows what is withheld and why, the evidence, and every unmet
     condition.
   - A release is offered only for a legacy retention the report shows as
     releasable.
   - The confirmation restates the exact entry, revision and evidence, and
     asks for an explicit acknowledgment.
   - The panel sends the entry's selection, revision and evidence digest,
     taken from the report it showed. Nothing is typed.
3. **The ship records a command** (`POST ci/action`,
   `request-legacy-release`). Only the ship's session may ask, never a
   delegate or a daemon bearer.
   - It refuses unless the runner's latest report shows that entry, at that
     revision, as releasable, with that digest.
   - It keeps at most one open command per entry: a repeated request while
     one is open is that one.
   - The command is durable in `%urgit-ci`'s state with its actor and time,
     and is audited.
4. **The ship signs it and hands it over.**
   - The CI key signs
     `['recovery' 1 recipient command 'release-legacy' expiry nonce selection revision evidence]`.
     Its leading tag keeps it apart from every assignment and grant, so
     neither kind of signature verifies as the other.
   - It expires fifteen minutes after the request (the existing
     unused-authorization lifetime).
   - It is handed over on the daemon's own poll, and only on a poll that
     says it carries such commands out (`x-ci-recovery`). An assignment
     waiting goes first.
   - A command handed over with no answer two minutes later (the existing
     redelivery interval) is handed over again, until the daemon answers.
   - A command not handed over before it expires expires. Nothing was done.
5. **The daemon carries it out, in process**, as the owner of its state
   file. Nothing else can: the operator's `-recover` needs the daemon
   stopped (QUESTIONS §1). The daemon:
   - verifies the signature against the pinned CI key, and the version, the
     recipient (itself) and the operation. `release-legacy` is the only
     operation;
   - answers *completed* if a released entry already names this command.
     This keeps it idempotent across lost replies and restarts;
   - refuses the command once it has expired, by the daemon's own clock;
   - finds the exact entry by its selection: its identity, time and
     revision. A changed or replaced entry is stale. It then checks that the
     entry is a legacy retention;
   - checks that the entry's attempt is not running or waiting here;
   - asks the launcher, on a fresh connection, for `hello` and the
     authoritative list;
   - recomputes the conditions and the digest, and refuses unless every
     condition holds and the digest is the one confirmed.

   Recovery commands run one at a time. A second delivery of one in progress
   is ignored.

   **Applying the release.** Under its lock, the daemon checks the entry
   again. It then saves a state file in which the entry is released, stamped
   with the command and its evidence.
   - Only once that save is durable does the entry leave memory. The slot
     comes back, and the capacity is recounted.
   - A save that fails before its rename changes nothing, and the command is
     refused, the refusal recorded first (below).
   - A save whose outcome is uncertain is answered *uncertain*, and the slot
     stays withheld. The state is marked not durable, so no new work starts
     (§5), until a save of memory's view succeeds. The command is then
     answered *refused*, not applied.
   - If the daemon restarts first, the file's version decides: a redelivered
     command is answered from it.
6. **The daemon answers** at `POST ci/daemon/<id>/recovery`, with its bearer
   only; the ship's session is refused. The answer is one of:
   - *completed*, with the release time and the capacity now;
   - *refused*, with every reason, the charge kept;
   - *uncertain*.

   Answers are retried until the ship takes them. Each command gets one
   decision, recorded in the state file before it is answered as final
   (below).
7. **The ship records the answer** on the command. *Completed* stands
   against any earlier answer; a refusal or doubt changes only an open
   command. The panel shows each command as:
   - *queued*: not yet fetched;
   - *pending*: fetched, with no answer yet;
   - *completed*, *refused*, *uncertain* or *expired*.

   The panel reports a slot returned only on the daemon's *completed*
   answer, never on the ship's receipt of the request.

**Every final answer is recorded first (independent review 07, R7-1).**
This model was written before the code that implements it. Review 07
(`.scratch/source-stage-01/independent-reviews/integration-07/REVIEW.md`)
found that an authenticated refusal lived only in the daemon's memory. Once
its saved state was reopened, the same signed command — a delayed duplicate,
or a replay — was judged again, and released the entry without a new
confirmation. The trace of every decision, answer and replay path in
`recovery.go` before this correction:

| Path | Answer | Where the decision lived |
|---|---|---|
| a message that fails verification | *refused* | nowhere, by design |
| a released entry names the command | *completed* | the state file |
| expired; the entry stale, replaced, gone or not legacy; the proof unmet; other evidence | *refused* | memory (`decided`) |
| while the evidence was taken, the entry changed, stopped being legacy or began running; no state file; the release's save failed before its rename | *refused* | memory |
| the release's save uncertain | *uncertain* | memory (`doubtful`), not final |
| a save of memory's view succeeding after that | *refused*, not applied | memory |
| the same command again, in this life | memory's answer | memory |
| the same command after a restart | judged again | nowhere |

The correction enforces ruling requirement 4, a durable outcome across
restarts and lost replies. It adds no policy:

1. **A final answer is the state file's.** A command's outcome is
   *completed* when a released entry names it, as before. It is *refused*
   when the state file's new `refused` list names it, with:
   - its command, selection and revision;
   - the evidence confirmed, and the evidence found when the launcher
     answered;
   - the reason and the time.

   The daemon gives either answer only once that record is durable. Every
   save carries the list forward, the daemon's and `urgit-runner -recover`'s
   alike, and nothing prunes it (QUESTIONS-SOURCE-01 §10 stays open).
2. **The record decides a repeat, before anything else.** Once a command
   verifies, the daemon looks for its record first:
   - a release answers *completed*;
   - a refusal answers *refused*, with its recorded reason.

   This holds after the command's expiry and after a restart too. The
   command is never judged again. A new confirmation is a new command, and
   is judged on its own.
3. **A refusal is recorded before it is answered.** Every refusal of an
   authenticated command is saved first, with memory's view: expired; the
   entry stale, replaced, gone or not legacy; the proof unmet; other
   evidence; a change while the evidence was taken; a release whose save
   failed before its rename.
   - Once that save is durable, the answer is *refused*.
   - If the save fails, or its outcome is uncertain, the answer is
     *uncertain*: refused, but not recorded yet. Nothing is released. The
     refusal stays in memory, and the next save that succeeds records it;
     while the state is not durable the daemon saves again at every turn
     (§5). Only then is it answered *refused*.
   - If the daemon restarts before that, nothing final was said. The
     command, handed over again, is judged again.
4. **An uncertain release is settled by the save that proves it absent.**
   A save of memory's view that succeeds after a release's uncertain save
   records, in that same save, the command's refusal ("not applied"). Only
   then is it answered *refused*.
5. **Unauthenticated input decides nothing.** A message that fails
   verification is no command of this daemon's.
   - Nothing of it is recorded, so no unauthenticated input fills the state
     file.
   - Nothing final is answered. The answer is *uncertain*, with why the
     message could not be authenticated. The ship keeps the command open and
     hands it over again.
   - The genuine command, once it verifies, is decided on its own.
6. **Memory holds no final decision.** The per-life map `decided` is gone.
   Memory keeps only what is not final:
   - the commands in progress;
   - the releases whose save was uncertain;
   - the refusals not recorded yet;
   - the answers the ship has not taken yet.

   A non-final answer never replaces a final one still waiting for the ship.
7. **The ship and the panel keep their rules.** The ship already keeps a
   final status against a later refusal or doubt, and lets only *completed*
   stand over an earlier answer. The runner no longer sends *completed* for
   a command it has answered *refused*. The panel's *uncertain* now reads
   "no final answer yet", since the runner gives it for every outcome not
   yet recorded.

No slot returns on any of these paths but a durable release. There is no new
timer, budget or grace: the retries are the daemon's own turn and the ship's
existing redelivery.

**One command, one identity (independent review 08, R8-1).** This model was
written before the code that implements it. Review 08
(`.scratch/source-stage-01/independent-reviews/integration-08/REVIEW.md`)
found that the signature and the record disagreed about what a command is:
- the signature binds the command's atom (`sig.ParseUV`, which accepts dots
  anywhere and leading zeros);
- the record was looked up by the command's text.

A separator or leading-zero spelling of a refused command's atom has the
same signed bytes and carries the original signature. It was judged again,
and released the retention, in the same life and after a reopen. The trace
of every place a command's identity is used, before this correction:

| Where | By | What an alias did |
|---|---|---|
| the signature | the atom | verified, with the one signature |
| the in-progress set (`startRecovery`) | the text | ran beside the command in progress, one after the other |
| a released entry's command | the text | was judged again: refused as "no retention", and that refusal recorded |
| a recorded refusal (`RefusalOf`) | the text | was judged again, and could release: R8-1 |
| a refusal not recorded yet, an uncertain release | the text | was judged again, and could release |
| the outbox | the text | made two answers for one command |
| the answer's command | the text as delivered | named a spelling the ship's `@uv` parser may not take |
| the recipient | the text | refused as another daemon's, before any effect (fail-closed) |

The correction: **a command is its atom.**
1. **One key.** Once a command verifies, the daemon computes its atom's
   canonical spelling, as Hoon writes a `@uv` (`sig.FormatUV`). That key is
   used for everything after verification:
   - the in-progress set;
   - the refusals not recorded yet and the uncertain releases;
   - the outbox;
   - every new record;
   - every answer, so the ship correlates the answer with its own command.
2. **Every record matched by its atom.** A released entry and a recorded
   refusal are found by their command's atom, whatever its spelling.
   Records written under another spelling, by an earlier version, keep their
   protection. They are neither rewritten nor dropped. A record whose command
   is not a `@uv` matches its exact text only; no verified command has such
   text.
3. **The other domains are unchanged.** The recipient must still be this
   daemon's own id, as written, and anything else is refused before any
   effect. The nonce is no key. The selection and the evidence are cords,
   signed as exactly their text, and the revision a number.
4. **A new confirmation is a new atom.** The ship mints each command's id
   fresh. A replay under any spelling is the same command, answered from its
   record; it is never a fresh confirmation.

**Authority.**
- Only the ship's owner starts a release: through the session-authorized
  action route, or a poke from the ship itself.
- The action is not delegable. A CI job's bearer or payload never reaches
  it.
- The ship's signature authorizes the attempt only; the daemon's own state
  and the launcher's evidence decide. A compromised ship, or a forged
  request, can at most ask for a release that the daemon's checks allow.
- No channel is added to the host. The daemon's own poll and posts carry
  everything, and no launcher or root credential leaves the host.
- The daemon carries out one operation, with three bound parameters, and
  nothing else.

**The daemon stays up for it.** A daemon whose every slot was withheld used
to stop (`ExitNoCapacity`). One whose withheld slots include a legacy
retention keeps polling at capacity 0, as one with unsettled admissions does
(§11.10), because its release comes through that poll. Only retentions whose
release needs the daemon stopped count toward the stop.

**The ship's state** (`state-0`, changed in place per AGENTS.md; no
migration):
- two new maps: `recovery-reports`, each runner's latest report (its body
  and its parsed entries), and `recovery-commands`;
- the types `recovery-report`, `recovery-entry`, `recovery-command` and
  `recovery-status`, and the action `request-legacy-release`.

`on-load` reads only `state-0` as now declared. An agent state saved before
this change does not load; it is not reinterpreted. `%urgit-ci` is nuked and
revived, as for every state-0 change in this greenfield phase. Its runners
must then re-enroll, and what that does to their retentions is
QUESTIONS-SOURCE-01 §12.

**Since state-migration ruling 01** (Q12 A; contract §8c), that paragraph
describes the Q11 stage as it was, and no longer the rule. The state is
versioned: `state-1`, tagged `%1`, has the Q11 fields exactly. `on-load`
reads the saved state as a noun and converts every supported earlier shape
explicitly (`desk/lib/ci-migrate.hoon`). Those shapes are the committed
base, review 06's schema and the Q11 schema, all tagged `%0` and each frozen
byte for byte. It refuses any other shape. Nothing is nuked or reset. The
runners keep their enrollment and bearers, so no runner re-enrolls. Every
runner's own state file, with its retentions and recorded refusals, is
untouched and stays authoritative.

**Not run here.**
- No ship, no Hoon compilation, no Eyre route, no browser and no deployment.
  The Hoon is inspected source only.
- The React components are not rendered, and `vite build` is not run: this
  worktree has no `node_modules`. Their logic is tested through the modules
  they delegate to.
- The cross-language vector for the recovery message
  (`desk/gen/ci-recovery-vector.hoon`) needs a ship.
- The live path end to end (Urgit, the ship, the daemon, the launcher and
  back) is a separately authorized qualification.

**Evidence.**

- **RED first, on the unchanged product code** (`evidence/q11-red/`): 9 tests
  and 21 subtests fail on their named assertions. They ran in a verified
  private copy of the accepted runner module, with only the new tests added.
  Most stop at the first observation, "NO RETENTION REPORT REACHED THE SHIP":
  before this stage the daemon had no channel. Their guards are shown apart
  by the U controls.
- **The runner, black box** (`internal/daemon/legacy_test.go`), through the
  real Microvm backend and launcher core over private files. The ship is an
  in-process handler modelling the contract, and its commands are signed with
  the key the state file pins:
  - the whole path, to a durable release answered completed with its
    capacity, the launcher's host untouched;
  - every refusal of the proof: a record of the attempt; the launcher gone,
    of protocol 3, or with a partial inventory; the entry this version's own
    shape, with no backend, or an admission; the attempt running here;
  - every refusal of the command: unsigned, another key, another daemon,
    another operation, another version, expired, tampered, a stale revision,
    a replaced entry, other evidence;
  - once across lost replies, redelivery and a restart; a second command
    refused;
  - an uncertain save returning no slot and starting no new work, then
    answered not applied; a restart while the file holds the release,
    answered from it;
  - a daemon whose only slot a legacy retention withholds staying up;
  - no retention this source writes being of the legacy shape, and the
    format stamped.
- **Units**: the judgement's six conditions and the digest's bindings; a
  change while the launcher is asked, refused; a delivery in progress,
  ignored; a decision per life; the report's kinds; the marking and the stamp;
  the client's one kind of work per poll, its header and its bearer; the
  recovery message's vector, its single-field mutations and its separation
  from the assignment's, both ways.
- **The CLI**: a legacy retention's provenance is shown, and its release is
  refused with a pointer to Urgit; an older file is saved with its marks.
- **The panel** (node, no dependency): the model, the view in every state,
  the real component with its hooks driven against a model of the ship's
  contract, the API's routes, and a refusal's status. The React render is
  NOT RUN.
- **Controls**: the U series (31), each an unsafe variant of one boundary
  seen by its named assertion, with G05, G09 and H31 re-anchored; and the
  frontend's F series (21). Their runs are in RECORD.

**Review 07's correction: evidence** (R7-1; the model above, "Every final
answer is recorded first").

- **RED first, on the submitted bytes** (`evidence/r7-red/`). The tests ran
  in private copies of `submissions/08`'s runner module, with only the new
  tests added:
  - 10 tests and 18 subtests fail on their named assertions;
  - one subtest passes there, as it should: a completed release repeated
    after its expiry, whose behaviour is kept;
  - review 07's probe, replayed byte-identically through a local overlay,
    reaches its own assertion there ("REVIEW REFUSED COMMAND REVIVED AFTER
    RESTART"), and passes on this source.
- **The runner** (`internal/daemon/refusal_test.go`,
  `refusal_repeat_test.go`), through the real Microvm backend and launcher
  core over private files:
  - every refusal of an authenticated command is recorded before it is
    answered, kept by a later save, and answered from its record after a
    restart. The refusals tested: the proof unmet, expired, a stale
    revision, other evidence, a change while the evidence was taken, and a
    release's save that failed before its rename. A new confirmation is
    carried out;
  - through the ship: a refusal delivered, its first reply lost, then handed
    over again before and after a restart; a refusal lost with the daemon
    before it reached the ship;
  - a refusal whose save failed, or whose save's outcome is uncertain:
    answered uncertain and not judged again, then recorded and answered once
    the state file is durable. A restart before that leaves nothing final. A
    repeat once saves recover is recorded, not judged again;
  - an uncertain release settled as not applied, recorded in the save that
    proves it, and refused after a restart;
  - a message the daemon cannot authenticate, answered uncertain and recorded
    nowhere; the genuine command is decided on its own;
  - a final answer never replaced by a non-final one waiting for the ship;
  - a repeat after its expiry answered from its record, for a release and a
    refusal alike.
- **The state file and the CLI** (`internal/state/refusal_test.go`,
  `cmd/urgit-runner/refusal_test.go`): Load and Save keep the recorded
  refusals whole, and so do `urgit-runner -recover`'s retry and release.
- **Adapted**: `TestARecoveryCommandIsAuthenticatedAndBound` now expects a
  message the daemon cannot authenticate to be answered uncertain and
  recorded nowhere, and an authenticated refusal to be recorded.
- **The panel**: *uncertain* reads "no final answer recorded yet", with the
  runner's own reason (`fe/src/runnerRecoveryAnswers.test.js`, RED first).
- **Controls**:
  - the V series (14), each an unsafe variant of the correction's
    boundaries, seen by its named assertion. Review 07's probe is among the
    observers;
  - U06, U07, U09 and U10 retargeted to the assertion for unauthenticated
    input, and U14 and U17 re-anchored, each with the same guard;
  - the frontend's F16.

  Their runs are in RECORD.

**Review 08's correction: evidence** (R8-1; the model above, "One command,
one identity").

- **RED first, on the submitted bytes** (`evidence/r8-red/`). The tests ran
  in private copies of `submissions/09`'s runner module, with only the new
  tests added:
  - 6 tests and 17 subtests fail on their named assertions;
  - review 08's probe, replayed byte-identically through a local overlay,
    fails there four times on its own assertion ("REVIEW SIGNED ATOM ALIAS
    REVIVED REFUSED COMMAND"): both aliases, in the same life and after a
    reopen. Review 07's probe passes there, as the review found. Both pass
    on this source.
- **The runner** (`internal/daemon/identity_test.go`), through the real
  Microvm backend and launcher core over private files. Every replay reuses
  the signed wire map with only its id respelled (a separator added, a
  leading zero, a separator moved), so no signing key makes an alias:
  - a refused command is refused under every spelling, with its recorded
    reason, in the same life and after a reopen. It is recorded once, and
    the answer names the command as the ship writes it;
  - a carried-out command is answered completed under every spelling, from
    its record, and never judged again;
  - a command not decided yet is not judged again under another spelling:
    its refusal not recorded yet, its release's save uncertain, its delivery
    in progress;
  - one command has one answer waiting for the ship;
  - a refusal or a release recorded under another spelling, as an earlier
    version could write it, keeps its protection and is kept as written.
- **Found by tracing again, after the first passing run**: the retention
  report named each release's command as its record spelled it. It now
  names it as the ship writes it, and the record is kept as written
  (`identity_report_test.go`). That test failed first on the submitted
  bytes, and on this correction's tree before the change
  (`evidence/r8-dev/03-report-red`).
- **Units**: every spelling of an atom has Hoon's one spelling, and a text
  that is no `@uv` has none; 500 random atoms round-trip, each respelled
  (`internal/sig/uv_test.go`). A recorded refusal is found by its
  command's atom (`internal/state/identity_test.go`).
- **Adapted**: some fixtures found an answer, a record or a queued answer
  by its command's text: `legacyShip.answersFor`, `recordedRefusal`, the
  outbox lookups of three tests, and the released entry's command in
  `TestALegacyRetentionIsReleasedFromUrgitOnItsEvidence`. They now find it
  by its atom, as the ship does. Their commands are still spelled `0v5.cmd`,
  which is not how the ship writes that atom. The daemon's answers and new
  records name it `0v5cmd`.
- **Unchanged**:
  - the recipient is compared as written, and a respelled one is refused
    before any effect;
  - `PollWork` takes any id, and one that is no `@uv` fails verification;
  - the ship reads an answer's command with `slaw %uv` and finds it by its
    atom (`handle-recovery-answer`). Whether its parser takes a spelling
    other than its own is not checked here: no Hoon ran.
- **Not claimed**: two uses of the key change nothing on any state the
  daemon can reach, so no control claims them:
  - `settleLocked` keys records the daemon wrote itself, whose commands are
    already canonical;
  - the answer to a message the daemon cannot authenticate is named again
    when it is queued.
- **Controls**:
  - the J series (13), each an unsafe variant of one seam of the
    correction, seen by its named assertion. Review 08's probe is among the
    observers: J07R restores the submission's textual identity at its three
    decision seams;
  - U13 and U21 re-anchored, each with the same guard. U13 is now seen by a
    direct observer, because its earlier one met one of two assertions by
    scheduling alone.

  Their runs are in RECORD.

### 11.13 One assignment, one identity (assignment-identity ruling 01, Q13 A)

This model was written before the code that implements it. Independent
review 09 (`.scratch/source-stage-01/independent-reviews/integration-09/
REVIEW.md`, R9-1) confirmed QUESTIONS-SOURCE-01 §13. An assignment's
signature binds its attempt as an atom, while the daemon's claim keyed the
attempt by its text. Both attempt fields respelled together, without the
signing key, kept the signed bytes and the original signature, and took a
second claim beside the first. Michael approved Q13 A: one authenticated
assignment attempt is one identity, from its verification to the last
message about it (`orchestrator/assignment-identity-01/RULING.md`).

The trace of every place an assignment's identity is used, before this
correction:

| Where | By | What a respelling did |
|---|---|---|
| the signature (`sig.ManifestMessage`) | the atoms: recipient, attempt, nonce, and the manifest's incarnation and candidate | verified, with the one signature |
| `verifyAssignment`: the attempt against the signed attempt; the candidate against the signed candidate | the text | both respelled together: passed. One respelled alone: refused, and that refusal is an abandon |
| the recipient | the text | refused before any effect (fail-closed) |
| `claim`, `release`, `claimed`: the attempts running or waiting for a slot | the text | a second claim beside the first: R9-1 |
| the abandon of a delivery while the state is not durable, and the refusal of one that fails verification, both before the claim is checked | — | a delivery of an attempt already running here, exact or respelled, was given up. The ship then offers that attempt to another runner: a fresh execution elsewhere |
| the work directory, the sandbox's spec and network, the launcher's record id, the VM and container names | the text | a second work directory, reservation and sandbox for one attempt |
| the messages that follow: event, result, upload, plan, lock, abandon | the text | named the attempt as delivered, a spelling the ship's `slaw %uv` may not take |
| the grants | the atoms: each is verified against the attempt and the manifest | verified |
| the image projections and the bundle's manifest | the text | named after the spelling |
| reconcile: whether a sandbox is this process's own | the sandbox's exact name | exact: that is its physical identity |
| reconcile: the ship's status of an orphan's attempt | the name's text | asked under the orphan's spelling |
| recovery: "its attempt is idle here" and "the launcher holds no record of its attempt" | the text | a retention in one spelling looked idle, or unheld, while its attempt ran here or the launcher held it in another. A legacy release's proof could pass wrongly |
| the retention report's attempt | the text as recorded | a spelling the ship's `slaw %uv` may not take |
| the nonce | no consumer | — |
| the manifest's incarnation | the signature and the grants only | — |

The correction:

1. **An authenticated assignment is its atoms.** `verifyAssignment` binds
   the attempt to the signed attempt, and the candidate to the signed
   candidate, by atom. Once the signature verifies, it rewrites the
   assignment in the one spelling the ship writes (`scot %uv`, `sig.FormatUV`):
   - its attempt, in both fields;
   - its candidate, in both fields;
   - the manifest's incarnation.

   Everything after verification sees only that spelling: the claim, the
   work directory, the sandbox and the launcher's record, every message to
   the ship, the grants, the projections and the bundle. A respelled
   delivery of an assignment is the same assignment.
2. **One claim per atom.** `claim`, `release` and `claimed` key the attempts
   running or waiting for a slot by that spelling. A delivery of an attempt
   already claimed, under any spelling, is ignored.
3. **Nothing is said to the ship about a claimed attempt's delivery.** A
   delivery that names, by atom, an attempt this process has claimed is
   ignored before anything else. That comes before the not-durable abandon
   and before verification's refusal, because an abandon would hand the
   attempt to another runner while it runs here. The delivery gains nothing
   by this: nothing runs, and nothing is said.
4. **Unauthenticated input is not rewritten.** A delivery that fails
   verification is refused as before, under its own text. No spelling of it
   is taken for an authorized one, and nothing is inferred from a parse.
5. **The recipient stays exact.** It must be this daemon's own id as
   written, and anything else is refused before any effect, as for a
   recovery command (§11.12).
6. **Physical names stay exact.** A sandbox, a launcher record, a work
   directory and a retention are what they are named, and nothing recorded
   is renamed. Where the question is the physical sandbox — reconcile's
   "this process's own", a retention's identity, a holding — the name is
   compared exactly. A sandbox named in another spelling of a claimed
   attempt is not this process's: it is an orphan like any other, held
   while the ship says its attempt runs and destroyed once it does not.
7. **Where the question is the attempt, the atom answers.** Reconcile asks
   the ship about an orphan's attempt in the ship's own spelling. The legacy
   release's conditions — its attempt idle here, the launcher holding no
   record of it — compare atoms. A retention or a launcher record in another
   spelling of a running or held attempt counts for it, which keeps a slot
   withheld and never frees one. The retention report names each attempt in
   the ship's spelling; its evidence digest still binds the entry as
   recorded.
8. **A new attempt is a new atom.** The ship mints each attempt's id, and a
   genuinely fresh assignment — a new atom, signed anew — is claimed and
   run as before. Nothing here refuses a canonical assignment, extends a
   deadline, restarts a cleanup clock, or authorizes anything again.

Unchanged: the version, expiry, operation, recipient and manifest checks
(the repository, OID, workflow and job are still compared exactly), the
signature itself, and the grants' own verification.

Not in scope, and disclosed (RECORD; QUESTIONS §14): the daemon keeps no
record of the attempts it has finished. A replay of a finished attempt's
assignment within its signature's expiry (the attempt's deadline and a
minute) is claimed again, exactly as before this correction. A respelled
replay is now that same replay, no more.

**Assignment-identity ruling 01: evidence** (R9-1; the model above).

- **RED first, on the submitted bytes** (`evidence/r9-red/`). The tests ran
  in private copies of `submissions/10`'s runner module, with only the new
  tests added:
  - 8 tests and 27 subtests fail on their named assertions. 10 subtests
    pass there, as they should: the same spelling repeated and a new
    attempt, running and waiting; the ship's own spelling; and the five
    refusals;
  - review 09's probe, replayed byte-identically through a local overlay,
    fails there twice on its own assertion ("REVIEW ASSIGNMENT ATOM ALIAS
    CLAIMED TWICE"), for the separator and the leading-zero spelling.
    Reviews 07's and 08's probes pass there. All three pass on this source.
- **The runner** (`internal/daemon/assignment_identity_test.go`), through the
  real `Run` loop, `verifyAssignment` and `claim`, the real Microvm backend
  and the launcher's real core and wire over private files. Every
  respelling copies the signed assignment and respells only its attempt:
  - a respelled delivery of an attempt running here, or waiting for a slot,
    takes no second claim and reserves no second sandbox (three
    spellings). The same spelling repeated is ignored as before, and a new
    attempt is claimed;
  - a respelled first delivery reserves its sandbox, and is answered, in
    the ship's spelling;
  - a delivery of an attempt claimed here is never given up to the ship,
    under any spelling: not while the state file is not durable, and not
    when it fails verification. Before, neither held even for the same
    spelling;
  - verification: the attempt or the candidate respelled in one field or
    both, and the incarnation respelled, verify and are named as the ship
    writes them. Another atom under the original signature, a respelled
    recipient, a text that is no `@uv`, another candidate and an expired
    authorization are refused;
  - one claim per atom through `claim`, `release` and `claimed`;
  - saved spellings: a legacy retention recorded as `0v5.cmd` is not idle,
    and a command cannot release it, while `0v5cmd` is claimed here. The
    launcher's record of `0v5cmd` refuses its release. The report names its
    attempt `0v5cmd`, and the record stays as written;
  - reconcile: an orphan named `ci-0v5.cmd`, while `0v5cmd` is claimed, is
    charged, and the ship is asked about `0v5cmd`;
  - after a restart: a respelled delivery of an attempt whose earlier
    reservation the ship still runs reserves no second sandbox.
- **Adapted** (RECORD adaptation 46). The fixtures' ship finds an attempt's
  status by its atom, and `f.at` names a delivered attempt's launcher
  record in the ship's spelling. Two checkout hooks and one check after a
  run find an attempt by its atom, and three launcher ids computed for
  delivered attempts use `f.at`. The fixtures still deliver attempts in
  their own spellings (`0v1.att`), so every existing `Run` test now runs a
  respelled delivery too.
- **Unchanged**: the recipient is compared as written, and the manifest's
  other fields exactly. A delivery that fails verification is refused under
  its own text.
- **Not observable alone, and not claimed alone**: a legacy release checks
  twice that its attempt is idle here, in the judgement and again in the
  release (`releaseLocked`), both by atom. Either refuses the release
  without the other (trial-28, AI11's first run), so the release's own
  check is seen only together with the judgement's (AI14).
- **Controls**:
  - the AI series (17), each an unsafe variant of one seam of the
    correction, seen by its named assertion. Review 09's probe is among the
    observers: AI13R restores the submission's identity at its two seams on
    the probe's path;
  - U04 re-anchored, with the same guard.

  Their runs are in RECORD.

### 11.14 An assignment runs at most once here (assignment-replay ruling 01, Q14)

This model was written before the code that implements it. Independent
review 10 (`.scratch/source-stage-01/independent-reviews/integration-10/
REVIEW.md`, R10-1) confirmed QUESTIONS-SOURCE-01 §14. The daemon held a
claim on an attempt only while the attempt ran. So a finished attempt's
assignment, delivered again while its signature was valid, ran again:
exactly or respelled, in the same life or after the state file was reopened.
Michael ratified CI-ASSIGNMENT-REPLAY-1: durable exclusion of repeated
execution, every record kept with no expiry, and genuinely new attempts
still runnable (`orchestrator/assignment-replay-01/RULING.md`).

The path an attempt takes, and what excluded a second run of it, before
this correction:

| Step | What happens | What excluded a repeat |
|---|---|---|
| delivery | `PollWork` hands over an assignment. The ship signs each hand-over anew (a fresh nonce); the attempt and its expiry stay | a claim, while the attempt runs or waits (§11.13) |
| verification, honourable | the signature, the manifest | — |
| claim, a slot | in memory | the claim |
| the durability gate | `retrySave` | — |
| execution | `handle`: the work directory, the sandbox's reservation and preparation, the checkout, the grants, `act`, the result | the claim; after a restart, sometimes the orphan's own reservation (a second reserve of an attempt is refused while its record exists) |
| teardown, retention | in `handle` | — |
| terminal publication | the result or the abandon, posted | the ship refuses a late post (`attempt is closed`) — too late: the work ran |
| release | the claim is dropped | nothing: R10-1 |
| a stop | memory is gone | nothing |

The correction:

1. **An execution ledger**, beside the state file (`<state dir>/executions/`),
   keeps one record in `taken/` for each attempt this runner takes for
   execution, named by the attempt's canonical spelling (§11.13). The record
   is created exclusively and made durable, the file and its directory
   synced, before the attempt crosses into execution (`handle`). That is,
   before its work directory, sandbox, checkout, grants and run. An attempt
   whose record is not durable is not started.
2. **No record is ever deleted, rewritten or expired** (Q10 KEEP). Nothing
   prunes the ledger, and it has no limit of its own. When the filesystem
   refuses a record — no space, no inodes, no permission — the attempt is
   not started, and nothing is evicted to make room: it fails closed.
3. **When `handle` returns, the attempt is finished**: a second record, in
   `finished/`. A record in `taken/` with none in `finished/` is an attempt
   a stop interrupted.
4. **A delivery of an attempt taken here is never run again**, whatever its
   spelling, nonce, signature or expiry:
   - running, or waiting for a slot: ignored (§11.13);
   - finished: ignored, and nothing is said to the ship. Its result or
     abandon was posted when it finished;
   - interrupted by a stop in an earlier life: abandoned, once verified,
     with the reason. The ship offers it elsewhere at once, as it did before
     this correction, when the sandbox's leftover refused a second
     preparation.
5. **An uncertain outcome counts as taken.**
   - A record whose creation failed before the file existed leaves the
     attempt untaken. It is not started, it is given back to the ship
     (abandoned), and it is not run again in this life.
   - A record that was created, but whose durability was not proven, may
     exist after a restart, and then it counts. The attempt is not started
     now, nor ever later.
   - A finished record that cannot be written leaves the attempt counted as
     interrupted after a restart: abandoned, never run.
6. **What the ledger cannot answer, it does not guess.** A delivery whose
   ledger this runner cannot read (its directory, or a record) is refused:
   abandoned, not run. A ledger that cannot be opened or created stops the
   daemon at its start.
7. **A state file from before the ledger is read as it is.** An attempt its
   retentions, released entries or held orphans name was taken by an
   earlier life, and counts as interrupted. What an earlier version ran and
   finished left no trace, and cannot be known. The ledger records when its
   history began (`executions/HISTORY`); that residual is QUESTIONS §15.
8. **A new attempt is a new atom.** The ship mints each attempt's id, and a
   genuinely fresh assignment is taken, recorded and run as before.

Not changed: the claim and the identity rule (§11.13), verification, the
durability gate, the sandbox, retention and reconcile semantics, and the
ship's own handling of a closed attempt.

**Assignment-replay ruling 01: evidence** (R10-1; the model above).

- **RED first, on the submitted bytes** (`evidence/r10-red/`). The tests ran
  in private copies of `submissions/11`'s runner module, with only the new
  test file added (`internal/daemon/replay_test.go`):
  - 8 tests and 11 subtests fail, all 16 leaves on their named assertions.
    Three copies ran, and each run is kept:
    - the first (`new-tests.*`): two of its leaves (the delivery signed
      anew, in its life and after a restart) failed on a fixture fault, a
      nonce that is no `@uv`;
    - the second (`new-tests-final.*`), with the nonce corrected before any
      source change: every leaf on its named assertion;
    - the third (`new-tests-third.*`), the RED of record. After the source
      change, the test of an interrupted attempt was corrected: at its
      cleanup it resumed its stopped life, which could then write into the
      directories being removed. A stopped process does nothing more, so
      the stopped life is now never resumed. With the tests in their final
      form, every leaf fails on its named assertion again;
  - review 10's probe, replayed byte-identically through a local overlay,
    fails there four times on its own assertion ("REVIEW FINISHED
    ASSIGNMENT EXECUTED AGAIN"): exact and respelled, in the same life and
    after a reopen. Its fresh attempt passes there. So do reviews 07's,
    08's and 09's probes. All four probes pass on this source;
  - review 10's diagnostic (a record of completion, review only: no
    production design) ran against the new tests in the third copy
    (`evidence/r10-dev/10-review-diagnostic`). The finished attempts'
    tests and review 10's probe pass there. 7 tests and 5 subtests fail on
    their named assertions: the interrupted attempt, the record before
    execution, the ledger that cannot be written, read or opened, the old
    state, and the history. It proves discrimination only.
- **The runner.** These tests (`replay_test.go`, the RED tests;
  `replay_uncertain_test.go`, the uncertain outcomes) run through the real
  `Run` loop, the real Microvm backend, and the launcher's real core and
  wire over private files:
  - a finished attempt is never run again, and nothing is said to the
    ship: exact, respelled or signed anew with a fresh nonce, in its life
    and after a restart. A new attempt after it runs;
  - an attempt a stop interrupted during its run is given back to the ship
    once verified, and never prepared again. The ship then times it out,
    and the next life reconciles its leftover away;
  - the record exists when the launcher prepares the attempt's sandbox;
  - the uncertain outcomes, injected around the ledger's real calls:
    - a take that failed after its effect: not started and given back;
      after a restart, given back again and never run;
    - a take that failed before its file existed: not started, and given
      back once, with the reason. Its deliveries in its life are ignored;
    - a finish that could not be recorded: answered in its life, and
      interrupted after a restart;
  - a full ledger (ENOSPC at every take) starts nothing and gives each
    attempt back once. Its earlier records (128 taken, 64 finished) stay
    byte for byte. Once it has room, a new attempt runs and is recorded;
    one given back while the ledger was full stays unrun in its life;
  - deliveries while an attempt's record or its finish is being written
    (exact, respelled, signed anew) are that attempt: it runs once, and is
    answered once;
  - a ledger that cannot be read (its taken directory, or its finished one)
    gives the delivery back with the reason, and runs nothing. A ledger
    that cannot be opened stops the daemon at its start, and so does one
    that has lost itself or a record directory; nothing is made in its
    place;
  - a malformed ledger. A taken record of garbage, of nothing, a directory
    or a dangling symlink counts: the attempt is given back. A finished
    record of garbage is an answered attempt: ignored. A HISTORY that
    cannot be read leaves the daemon running, saying so, and is kept;
  - old state: an attempt named by a retention, a released entry, a held
    orphan, or an earlier version's retention of the legacy shape is given
    back, and never run or taken again. The HISTORY of a ledger begun with
    an earlier version's state file says so.
- **The ledger** (`internal/state/ledger_test.go`), over real files; a fault
  is injected before or after each real step:
  - a record is made durable in order: created, written, synced, closed,
    its directory synced;
  - a record is never replaced: a second take is refused;
  - records are named by the attempt's canonical spelling only;
  - what cannot be read is no answer;
  - a record counts whatever it holds;
  - a refused record, at every step, before or after its effect, is
    reported with whether its file exists, and evicts nothing;
  - a ledger made before, or kept, is never made again. One whose making
    was cut short is completed;
  - a HISTORY that cannot be read is unknown, and kept.
- **The implementation adds to the model**, each with its tests and
  controls:
  - item 6 extended. The state file names the ledger its runner keeps
    (`ledger: true`). It is saved at the first start, and there is no new
    work until that save is durable (§5). A ledger made before (its
    HISTORY there) is never made again. A start that finds the ledger gone,
    or missing a record directory, is refused, since the records went with
    it: the operator restores them (QUESTIONS §15). A ledger whose first
    making a stop cut short (no HISTORY yet, so nothing was recorded in it)
    is completed;
  - HISTORY is evidence, and no record depends on it. One that cannot be
    read answers an unknown history (logged), is kept as it is, and does
    not stop the daemon;
  - the exclusion has two layers: the history a delivery is checked
    against, and the take's exclusive create. A record found at the take,
    after the delivery was checked, gives the attempt back as interrupted;
  - HISTORY's `state_format` is the file's as it was loaded (0 for an
    earlier version's), not the format that Load stamps in memory.
- **Residuals, disclosed** (RECORD):
  - an attempt given back because its record's file could not be created
    (the create itself refused) leaves nothing durable. In its life it is
    ignored. After a restart, a copy of its assignment delivered while its
    signature is valid runs: its first run here, of an attempt the ship has
    closed or re-offered. The ship delivers no closed attempt; only another
    source of its signed bytes could;
  - a finished attempt whose result the ship did not receive is not run
    again. The ship re-offers it at its deadline (CI-DELIVERY-1.1 c), where
    before the next re-delivery would have run it again;
  - what an upgraded runner's earlier version ran is not known
    (QUESTIONS §15).
- **Adapted**: no earlier test or fixture. U25's anchor moved: the same
  guard, re-anchored.
- **Not claimed**: a stop here is a daemon object abandoned without its
  cleanup, not a process crash and not a power loss. The failures are
  injected at the daemon's take and finish, and at the ledger's steps: no
  filesystem was filled.
- **Controls**:
  - the RP series (34), each an unsafe variant of one seam of the
    correction, seen by its named assertion. Review 10's probe is among the
    observers: RP04R restores the rejected submission's behaviour at the
    two seams on the probe's path;
  - U25 re-anchored, with the same guard.

  Their runs are in RECORD.

### 11.15 A runner with an incomplete history runs nothing until its transition (legacy-replay-upgrade ruling 01, Q15)

This model was written before the code that implements it. Independent
review 11 (`.scratch/source-stage-01/independent-reviews/integration-11/
REVIEW.md`) closed R10-1 at bounded source level. It found Q15 missing:
the daemon's start logs a ledger history that is incomplete or unknown, and
runs anyway. What a runner did before its ledger existed left no record
(QUESTIONS §15). A copy of an assignment its ship authorized then, still
valid, could run once more.

Michael ratified CI-LEGACY-REPLAY-UPGRADE-1
(`orchestrator/legacy-replay-upgrade-01/RULING.md`). Such a runner refuses
execution until software verifies that its old authorizations cannot be
replayed; an idle inventory or a human assertion alone is not that
evidence. The transition is inspected and confirmed in Urgit, carried
through the controller and the runner as authenticated, identity-bound
requests, and its results are durable across interruption and retry. No
old history is fabricated, nothing is reset, nothing waits on a guessed
time, and nothing re-enrolls.

**The proof: an authorization epoch in the signed nonce.** Every
authorization the ship signs names a nonce, and the signature binds it
(contract §5). Every version of `desk/app/urgit-ci.hoon` that signs an
assignment takes that nonce from `fresh-nonce`, which is sixteen bytes
(`(end [3 16] …)`, from its first commit, `bcafb5f`, to this source). So
every assignment the ship ever signed carries a nonce below 2^128. From a
runner's first confirmed transition, to epoch E, every assignment the ship
signs for it carries a nonce of E·2^128 or more: E above bit 128, a fresh
sixteen bytes below. The runner records E durably, and from then on refuses
every assignment whose nonce is below E·2^128. So:

- no assignment the ship signed before the transition can run, whatever
  its spelling, expiry or signature — and whether or not the ship still
  holds its record (a deleted repository's assignments are gone from its
  state; their signatures are not);
- the proof uses no clock, no deadline and no assumption about delivery. A
  clock that jumps back, a restart, and an assignment delivered late or
  delivered again all meet the same comparison of two signed numbers;
- the ship's epoch for a runner is a fact the ship holds forever: the
  number of transitions the owner has confirmed for it (its
  `confirm-history` commands, which, like every recovery command, are
  never pruned). An assignment's epoch is that number at its creation, so
  a delivery of an assignment created before a transition, however often
  the ship signs it again, carries the old epoch, and is refused.

Nothing here expires or deletes a replay record: the ledger (§11.14) keeps
every record, and it alone excludes a second run of an attempt taken after
the transition. The epoch excludes only what the ledger cannot know.

**Which runner waits.** A ledger's history is complete when it began with
the runner's enrollment (`HISTORY`: `enrolled: true`). A runner whose
history began with a state file kept before it (`enrolled: false`), or
whose history cannot be read (`HISTORY` missing from a ledger it kept, or
unreadable), is not a fresh runner: it waits. It waits until its state file
records its transition (`transition`: the epoch, the command, the evidence
and the time). A genuinely new runner — enrolled with this version — never
waits.

**A waiting runner stays on the operator's surface.** It polls, as its
heartbeat, and advertises no capacity, so its ship offers it no work. It
reports its retentions and its history, reconciles, and carries out the
owner's recovery commands: a legacy release too. Every assignment it
receives regardless — one its ship offered before, one delivered again —
is given back once verified, with the reason, and never started. Its held
capacity stays held: a transition enables execution and releases nothing.

**The transition.**

1. **The runner reports its history** with its retention report
   (`history`): its selection (`history/<since>`) and revision (the unix
   time its history began), whether its history is complete, known and
   waiting, the epoch it is at, its transition if any, and the digest of
   its evidence: the runner's id and its `HISTORY`, as the ledger holds it.
2. **The owner inspects it in Urgit** (Settings → Runners → the runner):
   the runner's evidence, the epoch the transition takes it to, and what
   the ship knows — the assignments it holds for the runner, and those
   still running there, which the transition gives back (below).
3. **The owner confirms.** The Runners panel posts the history exactly as
   inspected: its revision and evidence, never typed. The ship accepts it
   only from its owner's session, never delegated, and only while the
   runner's latest report shows that history, waiting, with that evidence,
   and no transition command of the runner is open. It records a
   `confirm-history` recovery command, bound to the history's selection
   and revision, whose evidence names the next epoch and the history
   evidence (`epoch <E> history <digest>`). From that moment every
   assignment the ship creates for the runner is of epoch E.
4. **The runner receives it on its own poll**, and checks it again:
   - the signature, by the CI key it pinned; the recipient, itself; the
     operation; its time;
   - the history it names, and the evidence digest, against its history
     now; the runner is waiting; the epoch is 1 or more.

   Then it records the transition in its state file, durably, and only
   then answers `completed`, and runs again. A save whose outcome is
   uncertain is answered uncertain, and the runner keeps waiting; the next
   save that succeeds records the command refused, not applied. A refusal
   is recorded before it is answered.
5. **Every later delivery of the command** — its answer lost, or the
   command replayed — is answered from the record, never judged again: a
   completed transition as completed, a refusal as refused. A command
   whose evidence, history or time no longer holds is refused, and one the
   runner cannot authenticate decides nothing.
6. **After it**, the runner advertises its capacity again. Every
   assignment it receives of an epoch below its own is given back, never
   run: an assignment still running on it at the ship before the
   transition is given back when it is delivered again, and the ship
   offers the job again as a new attempt, or closes it (a privileged one
   as unknown). New work, of its epoch, runs, and the ledger keeps it to
   one run.

**What the ship guarantees, and the runner cannot check.** The runner
verifies the command and enforces the fence. That old assignments carry
nonces below 2^128 is a fact of the ship's source, recorded above; that new
ones carry the epoch is the ship's signing (`epoch-nonce`, `epoch-at`,
`assignment-json`). Both are in `desk/`, and no Hoon ran here.

Not changed: the ledger and its rules (§11.14), verification, the claim,
retention and recovery semantics, and the recovery message's format (the
transition is a second operation of it).

**Evidence.** Every run, with its numbers, is in RECORD.

- **RED first, on the submitted bytes** (`evidence/q15-red/`). The tests ran
  in private copies of `submissions/12`'s runner module, with only the new
  test file added (`internal/daemon/history_test.go`):
  - the first copy ran the whole daemon package: its 91 earlier tests pass,
    and the 8 new tests fail, with 15 subtests;
  - the second (`new-tests-2.*`) is the RED of record: the new tests alone,
    in their final form. One fixture changed after the first run: the
    waiting runner's retention, in the recoverable-while-waiting test, is an
    earlier version's. The 8 tests and 15 subtests fail;
  - three leaves reach the counterexample. An upgraded runner, one whose
    HISTORY cannot be read, and one whose HISTORY is missing from the ledger
    it kept each ran an assignment: the submission's start only logs. The
    other sixteen fail at their first step: the submission reports no
    history, and has no transition. On this source, each of their guards
    is reached by its control (below);
  - reviews 07–10's probes pass on the submitted bytes, and on this source.
- **The runner.** `history_test.go` (the RED tests) and `history_post_test.go`
  (what the correction adds) run through the real `Run` loop, verification,
  the recovery channel, the real Microvm backend, and the launcher over
  private files:
  - a runner with an incomplete history runs nothing. It is upgraded, or its
    HISTORY cannot be read, or HISTORY is missing from the ledger it kept.
    An assignment its ship signed before and one of a later epoch are each
    given back once, and never prepared. Every poll says capacity 0, and its
    report shows its history waiting;
  - a runner enrolled with this version runs without a transition,
    advertises its capacity, and reports its history complete;
  - the transition, confirmed, is recorded in the state file and answered
    completed. In its life and after a restart:
    - an assignment signed before it is given back once, never prepared:
      the largest old nonce (2^128−1), and one respelled;
    - work of its epoch runs, at the epoch's floor (2^128) too;
    - the runner advertises its capacity again;
  - a transition whose save is uncertain is answered uncertain, and the
    runner keeps waiting and runs nothing. Once saves succeed, it is
    refused, not applied, and answered so from its record. A new
    transition, to the next epoch, completes, and fences the epoch before
    it;
  - a transition that does not hold is refused and recorded, and nothing
    runs: bound to other evidence or to another history, expired, of epoch
    0, or naming no epoch. A completed transition delivered again is
    answered from its record. A later one, the runner no longer waiting, is
    refused;
  - a transition the runner cannot authenticate decides nothing and records
    nothing: signed by another key, for another runner, its epoch changed
    after it was signed, or unsigned;
  - a transition releases no held capacity. A waiting runner with a
    retention advertises nothing. Transitioned, it advertises its slots less
    the one the retention withholds, which it still keeps and reports. A
    waiting runner carries out a legacy release, and that release enables no
    execution;
  - after the transition, an attempt an earlier state file names is still
    given back, even delivered with a nonce of the new epoch;
  - the report shows a transitioned runner's epoch and transition;
  - the transition's evidence is read exactly, and the history evidence
    binds the runner and its history.
- **The client, the state file and the vector**: a waiting daemon's polls
  send `x-ci-capacity: 0`, and any other's as before
  (`internal/ship/paused_test.go`). The state file keeps the transition
  exactly (`internal/state/transition_test.go`). `internal/sig/epoch_test.go`
  pins an epoch-1 assignment's message bytes with the Hoon vector's, and its
  signature does not verify for the nonce's low bits alone.
- **The panel** (node: the real modules and component, a hooks runtime, and
  a model of the ship's transition contract):
  - each history is told apart: waiting, unreadable, complete,
    transitioned;
  - a transition is offered only to a waiting runner, with evidence to bind
    and no command open;
  - the confirmation restates the history, revision, epoch and digest, and
    needs its acknowledgment. The post is exactly that binding;
  - a stale binding is said, and its button disabled. The ship's refusal (an
    actor who is not the owner, changed evidence) is shown in its own words,
    and a lost reply is said to be unknown;
  - the command's states are told apart. Transitioned comes only from the
    runner's answer, and a transition is never called a release;
  - no second transition is offered before the runner's report after its
    answer. One confirmation is open at a time;
  - the list marks a paused runner.
- **The implementation adds to the model**, each with its tests and
  controls:
  - the ship took an explicit capacity 0 for nothing (`?:  =(0 u.reported)
    daemons`). It takes it now, and the resolver's fallback skips a runner
    whose capacity is 0. No earlier runner sends 0: the client sends a
    capacity only when it has one, and a waiting runner's 0 explicitly
    (`Client.Paused`);
  - every verified assignment passes the fence before the ledger is asked,
    or anything taken;
  - the transition's command is a second operation of the recovery message,
    so its record, refusal, doubt and answers are §11.12's. A refusal settled
    from an uncertain transition says `notTransitioned`;
  - the history evidence is a sha256 over the runner's id and its HISTORY as
    the ledger holds it (`history unknown` when it cannot be read).
- **Residuals, disclosed** (RECORD):
  - a runner re-enrolled with its old ledger still beside its state file
    keeps that ledger's history, and waits. That is conservative;
  - a waiting runner of this version needs a ship of this version to
    confirm its transition. An earlier ship neither takes its capacity 0 nor
    has the action. The runner gives back what such a ship offers it, and
    runs nothing;
  - that every nonce the ship signed before is below 2^128 is a fact of
    `desk/`'s history (every commit since `bcafb5f`), which the runner cannot
    check. The Hoon did not run (§10);
  - after the transition, the state file's named attempts and the ledger
    stand behind the fence: defense in depth.
- **Adapted** (RECORD lists each):
  - the fixture of a runner enrolled with this version (`newVMFixture`) now
    makes its complete ledger first. An upgraded runner's is `newVMHost`,
    and `newStateFixture` stands over it;
  - five tests of §11.14 changed. Four start from `newVMHost`, a runner
    without a ledger before its first start, as they were written. Two now
    expect the runner to wait: the one begun with an earlier version's state
    file (one of the four), and the one whose HISTORY cannot be read;
  - RP14's observation moved to
    `TestAnAttemptOldStateNamesIsGivenBackAfterTheTransition`. U09's and
    V04's anchors moved; each is the same guard.
- **Not claimed**: a restart here is a new daemon object over the same
  private files, not a process crash and not a power loss. The save
  failures are injected at the daemon's save. The ship is modelled in the
  panel's tests, never run.
- **Controls**:
  - the RQ series (39), each an unsafe variant of one seam of the
    correction, seen by its named assertion;
  - the panel's G series (27);
  - the ship's source: 15 static controls, each a mutant of its Hoon that
    the static check must refuse, and the schema check's 4;
  - RP14, U09 and V04 re-observed or re-anchored, with the same guards.

  Their runs are in RECORD.

### 11.16 A transition stands only on a record this version writes; each Prepare boots the image it verified (the correction after independent review 12)

This model was written before the code that implements it. Independent
review 12 (`.scratch/source-stage-01/independent-reviews/integration-12/
REVIEW.md`) found two defects in the Q15 submission.

**R12-1: a malformed transition record lifted the pause.** §11.15 lets a
runner with an incomplete history run once its state file records its
transition. The state file's record counted as a transition whenever it was
present. `transition: {}`, or a record of epoch 0, lifted the pause, and the
fence refuses nothing at epoch 0: old, authentic, still-valid work ran,
though no confirmation was ever sent. The record is local state. Only
corruption, a hand edit or another program writes one this version did not,
and the review names no remote path. The fault is that the runner trusted
it.

The correction:

1. **A transition record is read, never trusted.** Loading the state file
   keeps the record exactly as the file holds it, whatever it is, and reads
   its fields where it can. The state file loads either way: a record is no
   reason to refuse the start, as an unreadable HISTORY is none (§11.15).
2. **A record is a transition only when it is one this version writes**:
   - an object of exactly its four fields, each named once: an
     authorization epoch of 1 or more, the command in the ship's spelling
     (a canonical `@uv`), the history evidence (a sha256, in hex), and its
     time;
   - bound to this runner's history as it is now: its evidence is the
     digest of this runner's id and its HISTORY (`historyEvidenceOf`).

   Anything else is no transition: an empty object, epoch 0, a field
   missing, of another type or not this version's, an extra field, a field
   named twice, or another history's evidence.
3. **Every consumer asks the same question.** Each takes only a record that
   is a transition:
   - the pause, and the fence's epoch;
   - the report;
   - the transition's own checks;
   - a delivered command's answer from its record (§11.12, "Every final
     answer is recorded first").

   A malformed record never answers the command it names.
4. **A runner with a malformed record waits**, as one with none does:
   capacity 0, every assignment given back, and still inspectable and
   recoverable. Its report says that its history waits, and why the record
   is no transition; its start logs it. Nothing erases the record: every
   save writes it back byte for byte.
5. **A transition the owner confirms supersedes it, and keeps it.** The
   command is carried out as for a runner with no record (§11.15, step 4).
   The malformed record moves, as the file held it, into
   `superseded_transitions`, with why it was no transition, the command
   that replaced it, and when. Old work stays fenced, and fresh work runs
   again.

6. **A state file that names a field twice is refused, and left as it is.**
   Go's decoder takes two names for one field (the same name, or one in
   another case), keeps the last, and drops the other, which the next save
   erases. The correction's own audit found this after its first final
   runs (below). Within the transition record, a field named twice makes
   it no transition (step 2), and it is kept, both names, as any other.
   Anywhere else in the file (its `transition` named twice, or a field of a
   retention, a refusal or a superseded entry) no runner writes such a
   file, and which of the two is meant is not known. No save could keep
   both, so the load is refused, and with it the start: the explicit
   startup refusal the review allows. The file is left as it is. The
   values kept as the file holds them, the transition record and each
   superseded record, are read for no field: a runner that superseded
   such a record loads the file it wrote.

Not changed: what a transition is, the epoch proof, the ship's side, and the
recovery message.

**R12-2: two Prepares shared one verified image.** A Microvm backend
verifies its image directory at start and before every Prepare (M10). The
verification wrote the manifest and its digest into the backend's shared
fields, and Prepare reserved with the shared digest. With two attempts
preparing at once, as on a runner of capacity 2, one Prepare's verification
wrote while another's reserve read: a data race, which `-race` found in the
review's combined regression run. With the image changed between two
verifications, a Prepare could reserve an image its own verification never
saw. And only the start checked that the guest helper speaks this runner's
protocol and preloads its act image, so an image changed in place was booted
unchecked for both.

The correction:

1. **A verification is a value.** `verifyImage` returns what it verified:
   the manifest and its digest. It writes nothing shared except, under the
   backend's lock, the last image verified, which only the runner's own
   reports read (`Name`, `ImageDigest`).
2. **A Prepare boots exactly the image it verified**: its reserve names its
   own verification's digest.
3. **Every Prepare checks all that the start checks**: the kernel's and the
   rootfs's digests against the manifest (the tamper check), and that the
   guest helper speaks this runner's protocol and preloads its act image. An
   image changed in place since the start is booted only if it passes all of
   them. One that does not is refused, and nothing is reserved.

Not changed: the verification's rules, the launcher's protocol, and every
other backend field, each under its lock. No test is serialized, and `-race`
stays on.

**Evidence.** Every run, with its numbers, is in RECORD.

- **RED first, on the submitted bytes** (`evidence/r12-red/`). The tests ran
  in private copies of `submissions/13`'s runner module, with only the three
  new test files added:
  - 6 tests and 18 subtests fail, every leaf on its named assertion: 22
    leaves. Fifteen reach the counterexample:
    - with an empty record, one of epoch 0, or one naming no epoch, both an
      assignment its ship signed before and one of a later epoch ran;
    - with a record of epoch 1 wrong in one other way, fresh work ran,
      though no transition was ever confirmed. The records named their
      command in another spelling or no atom, or named none; had no
      evidence, malformed evidence or another history's; had no time or a
      time of 0; or carried an extra field;
    - a malformed record answered the command it names, and the owner's
      transition was refused over one ("not waiting");
    - a Prepare reserved the image another Prepare verified;
  - five fail where the submission cannot decode the record (a field of
    another type, a string, a number, a list), and refused the start or the
    load. That fails closed, but leaves the runner neither inspectable nor
    recoverable;
  - two fail where an image changed in place, to another helper protocol or
    act image, was reserved unchecked;
  - three pass there, as they should: a record this version writes
    round-trips, a tampered rootfs is refused, and an image changed in place
    that passes is booted;
  - the first copy's run and the RED of record fail the same way. Between
    them, gofmt reformatted the daemon test's table (line breaks only);
  - review 12's probe, replayed byte-identically, fails there twice on its
    own assertion (the empty object, epoch 0), as the review found. Its other
    cases pass;
  - no race is reported in the RED runs. The image test's forced schedule
    passes through the launcher's socket, which the race detector takes for
    synchronization. Its assertion is what fails, and its controls show it
    (RR13, RR13U).
- **The runner.** `history_record_test.go` (the RED tests) and
  `history_record_post_test.go` (what the correction adds) run through the
  real `Run` loop, verification, the recovery channel, the real Microvm
  backend and the launcher over private files. Each record is written into
  the state file between two lives:
  - reopened over each of sixteen malformed records, the runner starts,
    waits and runs nothing. An old and a fresh assignment are each given back
    once and never prepared, every poll says 0, and its report shows its
    history waiting. A refusal recorded in the state file meanwhile (a save)
    leaves the record as it was;
  - the owner's transition over a malformed record completes. The new
    record is the transition; the malformed one is kept in
    `superseded_transitions`, with why, by whom and when. Old work is fenced
    and fresh work runs, and again after a reopen;
  - a malformed record never answers the command it names: the command is
    carried out, and supersedes it;
  - the report says why a record is none and shows it, and the start logs
    it. A record right in every field but bound to another history is none.
    Superseded records accumulate, each kept.
- **The state file** (`transition_record_test.go`,
  `transition_problem_test.go`): every malformed record loads, and is
  written back as the file held it, through two saves. A record this version
  writes round-trips, and a null is none. `Problem` names each rule broken
  alone.
- **The backend** (`image_snapshot_test.go`):
  - two Prepares run at once, the image changed in place between their
    verifications. A launcher that holds the first Prepare's hello forces
    the schedule. Each reserve names the image its own Prepare verified;
  - an image changed in place to another helper protocol or act image, or
    tampered, is refused, and nothing is reserved. One that passes is booted,
    and named.
- **The panel** (`runnerHistoryRecord.test.js`): a record that is no
  transition is read from the report, said, and shown as the file holds it,
  and the transition is offered.
- **The correction's own audit, after its first final runs** (step 6).
  `Problem` counted a record's names through a map, which holds one of two
  names alike, and the state file was decoded as Go decodes it, keeping the
  last of two. full-20, then running on the correction's bytes, was stopped
  and is kept as it stopped. Two new test files (`transition_twice_test.go`,
  `history_record_twice_test.go`) ran RED first, in a private copy of the
  correction as pinned before full-20 (`evidence/r12-red/twice-tests.*`),
  every leaf on its named assertion and reaching the counterexample:
  - a record naming its epoch twice, 0 then 1, or its command or evidence
    twice, the last right, was taken for a transition, and fresh work ran;
  - the owner's transition was refused over one ("not waiting");
  - a state file naming a field twice loaded, the first name dropped, and
    the start went on over one naming its transition twice.

  On this source such a record is none. It is reported, kept with both
  names, and superseded by the owner's transition, after which the runner
  loads the file it wrote. A file naming a field twice elsewhere is refused
  at the load and at the start, and left as it is.
- **Review 12's probe** passes unchanged. So does every earlier probe,
  together, under `-race` (`all`): the run in which the review found the
  race.
- **The implementation adds to the model**:
  - "byte for byte" is, precisely, the same JSON, token for token: a save
    re-indents the file, and the record with it;
  - a record whose fields give it back exactly (one this version could have
    written) is written back from its fields; any other, from its bytes;
  - a record that is none is kept in memory too, so every save writes it
    back until the owner's transition supersedes it;
  - `Name` and `ImageDigest` read the image last verified, under the
    backend's lock. No reserve reads it.
- **Adapted**: the live opt-in test (`microvm_live_test.go`) reads its image
  from `verifyImage`'s value; it did not run. No other earlier test or
  fixture changed. RQ15's anchor moved; it is the same guard, re-anchored.
- **Not claimed**: the records are written into a private state file by the
  tests. That stands for corruption or a hand edit, not a remote path, a real
  disk fault or a power loss. The concurrent Prepares run against a scripted
  launcher, and no VM booted.
- **Controls**:
  - the RR series (35), each an unsafe variant of one seam of the
    correction, seen by its named assertion. RR01R restores the submission's
    trust of any record at both seams, seen by review 12's probe,
    byte-identical. RR19 to RR24 are the audit's: a field named twice taken,
    the refusal dropped, another case or a nested object not read, and the
    values kept as the file holds them read for fields;
  - the panel's G18 and G18V;
  - RQ15 re-anchored, the same guard.

  Their runs are in RECORD.

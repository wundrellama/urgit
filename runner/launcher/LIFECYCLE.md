# urgit-vm-launcher — durable reservation lifecycle

This is the model the launcher core (`runner/internal/launcher`) and its root
command (`runner/cmd/urgit-vm-launcher`) implement: who owns a reservation,
what is durable when, which host effects are known, failed without effect,
or uncertain, and how restart, release, quarantine and concurrency agree
with that. It changes nothing in `specs/ci-execution-contract.md` §9
(operations, budgets, authority) and conforms to it — in particular to
"Quarantine is cleared only by the root CLI `urgit-vm-launcher clear <id>`
after an operator inspected it" (line 193), which §4.2 below carries through
every path. Since recovery ruling A (source stage 01; `INTEGRATION.md` §8)
that root CLI is `urgit-vm-launcher recover`, and clearing is two separate
operator actions: a cleanup retry, which never releases, then an explicit
release of the exact incarnation and revision the operator inspected, once
nothing is held. `clear` only names `recover` and exits 2 (the contract's
line still names the old command: QUESTIONS-SOURCE-01 §7). It pins down the persistence and ownership rules §9 left
implicit. Four further invariants close review 02's classes. The namespace
a record lives in is certified by every open (§2.1). No host effect begins
at or after the selected deadline (§8). Every record the launcher
acknowledges or publishes reloads as exactly itself, with every identifier
inside the domain the loader and the host accept (§2.2, §3.1). One terminal
call owns the service's end until its lock is released (§4.3).

**What the tests behind this model do and do not establish.** They run as an
ordinary user against private temporary directories with nonexecuting host
fakes. They prove the core's ordering, refusal and recovery rules; real
filesystem faults the kernel produces for an ordinary user (a directory or a
symlink in a record's path, `EACCES`, `EFBIG`, `EISDIR` from a rename);
injected faults at the filesystem adapter (fail-before and effect-then-error
around the real syscall); and process death by `SIGKILL` of a re-executed
test child. They do **not** establish real VM cleanup, jailer/cgroup/netns
semantics, protected-directory trust, power-loss tolerance (fsync is issued
and its failure honoured; no power cut was simulated), installation,
network isolation or guest execution. Durability is stated as syscall
ordering and recovery evidence: which fsyncs completed, in which order,
before which acknowledgement, and what the next open does after a failed one.
No power cut was performed and none is claimed. Since source stage 01 the
timing guarantees below no longer rest on the Host returning promptly: every
teardown ends by its cleanup obligation's deadline — one allowance after the
earliest trigger that asked for it (the owner's destroy, the job's deadline,
the launcher's stop, a create's failure), carried through every wait before
it — and the core hands that deadline, and each create step the create's
context, to the host through `Bounder`; the real adapter bounds every
command and filesystem operation by them, killing an owned process group at
its bound (`INTEGRATION.md` §7). That is source behaviour, proven on
command recorders, host models with barriers, a private cgroup-filesystem
model and inert re-executed children; the durable publications of outcomes
are not bounded by it, and the complete production launcher is still **not
shown on a real host** to meet the 120-second cleanup obligation
(RIDER-CI-P4-04 line 14): NOT RUN.

`INTEGRATION.md` is the companion model of what the core hands to its callers
(the retained identity a failed `Prepare` carries to the daemon, the
incarnation a destroy names) and to its host (the bounded adapter).

Two unexported `Config` fields are test seams that production leaves unset:
`now` is the clock every deadline decision reads (`time.Now`), and `at` is a
callback at named points of the terminal sequence and of a reaper pass, used
to hold a `Shutdown` or the reaper where a test needs it.

## 1. Components and callers

| Caller | Path | Core entry |
|---|---|---|
| `urgit-vm-launcher serve` | `main.go` `serve` | `NewService` (creates at most the state directory; exclusive state lock; certifies the namespace, §2.1; loads and classifies), the problems and leftovers logged, every quarantined record logged (`QUARANTINED <id> (<job>; owner …): …; the operator's urgit-vm-launcher recover, with serve stopped, retries its cleanup and releases it`), `listen` (a socket whose group/mode cannot be set is not served), reaper goroutine (`Reap`: its first pass at once, then one every 5 s), `SetReport` (every teardown the launcher starts on its own and does not release is logged: `TEARDOWN HALTED`, `QUARANTINED` — a late one with its `LATE` reason —, `RELEASE NOT CONFIRMED`), `Server.Serve`; on SIGINT/SIGTERM: socket closed, reaper stopped (a teardown it has in progress keeps its deadline), `BeginStop` (no new operation; the stop request becomes the trigger of every teardown the stop runs; every create asked to roll back for it: its cancellable step cut short at once, a start within a quarter of the allowance), `Service.Shutdown` (it owns the service's end until its lock is released, §4.3) |
| `urgit-vm-launcher recover`: list, select, inspect (`-select`/`-action inspect` for scripts) | `recover.go` `incidents`, `lookup`, `recoverInteractive`, `recoverScripted` | `ReadState` + `InspectRecord` (read only, no lock: works while `serve` runs) |
| `urgit-vm-launcher recover`: retry, release | `recover.go` `act` | `NewService` (refused with `ErrStateBusy` while `serve` holds the state), `RetryCleanup` or `Release`, `Close` |
| `urgit-vm-launcher clear` | `main.go` | none: it names `recover` and exits 2 |
| `urgit-vm-launcher list` | `main.go` `list` | `ReadState` (read-only snapshot: records — an interrupted teardown shown quarantined —, leftovers, problems; no lock, no host call); problems fail the command |
| `urgit-vm-launcher check` | `main.go` | host preflight only; no state access |
| runner daemon (`sandbox.Microvm`) | `internal/sandbox/microvm.go` (stage 01: a connection of its own per operation, bounded by its context) | wire ops via `Client`: `hello`, `reserve` (naming its request token, wire protocol 4), `settle`, `create`, `connect`, `inspect`, `stop`, `destroy` (with `cid`/`created`: `DestroyOf`), `released`, `list` (settled-admission ruling 01 added `settle` and the token, and this row now names `released`, added by late-accounting ruling 01) |
| wire server | `proto.go` `Server.handle` | one `Service` method per op; `Reply.Quarantined` = `errors.Is(err, ErrQuarantined)`; a reserve left charged answers `retained` with its id, cid and created; a reserve that names no request token is refused (INTEGRATION.md §11.10) |

**How the daemon consumes a quarantine** (resolved at source in stage 01;
`INTEGRATION.md` §§3–6 is the model, and QUESTIONS-SOURCE-01 records where
each item stands). What follows is the round-three diagnosis, kept as the
failure scenarios stage 01's tests preserve:

* **Ordinary post-`Prepare` destroy** (`internal/daemon/run.go` :96–108): any
  destroy error — a teardown that quarantined, a halted teardown, or the
  refusal of a record already quarantined (§4.2) — makes the daemon
  quarantine its slot (`d.quarantine`, persisted in its state file).
* **Rollback inside a failed `Prepare`** (`internal/sandbox/microvm.go`
  :253–263): when `create` fails, `fail` destroys the reservation; if that
  destroy fails, `Prepare` returns an error with an **empty `Handle`**, and the
  daemon's `Prepare`-error path (`run.go` :89–93) calls `d.fail` and returns
  without `d.quarantine`. The daemon keeps advertising a slot the launcher
  still charges. The launcher's own durable budget remains the final admission
  barrier (a later `reserve` beyond it is refused, visibly), but the daemon's
  advertised capacity is wrong. **Stage 01:** the rollback destroys exactly
  that incarnation and, when it cannot, `Prepare` returns a
  `sandbox.RetainedError` naming it (record, incarnation token, cid, created); the daemon
  retains it durably and withholds its slot.
* **`Reconcile` at daemon start** (`internal/daemon/daemon.go` :151–193):
  `Orphans` lists every launcher record of this daemon, quarantined ones
  included (`microvm.go` :416–437); for an attempt the ship calls terminal or
  unknown, the daemon destroys it and quarantines the slot on error. A
  quarantined record's destroy is always refused now (§4.2). Each daemon start
  first carries over every slot quarantine its state file holds (`daemon.go`
  :108–114), and `quarantine` (:406–423) neither deduplicates by handle nor
  consults that carry-over. So once the daemon has recorded a slot quarantine
  for a launcher quarantine, every later start counts it again: the carried
  entries, plus one more from `Reconcile`, which also persists. Each restart
  withholds one more slot for the same record, until `ExitNoCapacity`. Never
  an over-admission, but a capacity erosion. **Stage 01:** retentions are
  keyed by identity (an older handle-only entry is completed, not counted
  beside it), a reconcile pass neither destroys nor charges what is retained,
  and any number of restarts charge it once.

## 2. Durable state

```
<state_dir>/launcher.lock            flock(LOCK_EX|LOCK_NB) held by exactly one Service
<state_dir>/attempts/<id>.json       the authoritative record: {"format":1,"record":{…}}
<state_dir>/attempts/<id>.json.tmp   a publication in progress (never authoritative)
```

`<id>` is `IDFor(id_prefix, attempt)` = prefix + `-` + 24 lowercase hex.

### 2.1 The namespace, certified by every open

The constructor creates at most two directories: the state directory itself
(`mkdir`, never its ancestors) and `attempts/`. It also creates the lock file.
The state directory's parent is the installation's, and must exist (the
`check` preflight already requires the state directory). A link is durable
only once its parent directory has been fsynced after the link was made.
Visibility is no proof: an earlier open may have created a link and then
failed at the fsync that would certify it. So **every** open, after taking the
lock, fsyncs:

1. the state directory's parent, which certifies the state directory's own
   link;
2. once `attempts/` exists, the state directory, which certifies the links of
   `attempts/` and the lock file;
3. `attempts/` itself, which certifies its contents — every record's presence
   and absence — before anything is read from it (stage 01, triage 08's
   restart proof; INTEGRATION.md §11.6).

A failed certification of 1 or 2 fails the open (`ErrNotDurable`; the lock is
released), and the next open performs them again. A failed certification of
3 fences the open instead: the records it can read are loaded and enforced,
but nothing is admitted or booted and no absence is answered as a release
(the owners' list is refused) until a reaper pass's retry succeeds. Only an
open whose certifications succeeded ever acknowledges anything. Each publication then
fsyncs `attempts/` after its rename, so every link from the installation's
directory down to an acknowledged record has had its parent fsynced after its
creation, whichever process created it.

### 2.2 Encoding: every record reloads as itself

`encodeRecord` is the one encoder, for publication and admission alike:

1. It normalizes the only field that may be lossy, the diagnostic reason:
   invalid UTF-8 is replaced and the reason is cut at a rune boundary to
   `maxReasonBytes` (4 KiB).
2. It refuses an envelope larger than the loader reads (`maxRecordBytes`,
   1 MiB, whose read bound is unchanged).
3. It decodes the bytes back strictly, validates them exactly as open does
   (`validateRecord`, and the id's namespace), and requires the result to equal
   the record, field for field.

A record that would not reload as written is never written (`notWritten`, op
`encode`), so it is never acknowledged, and a later effect never depends on it.

**Publication** (`store.publish`): encode (above), then open the temp with
`O_WRONLY|O_CREAT|O_NOFOLLOW|O_NONBLOCK` (no truncation yet, never following
a link, never waiting for a FIFO's reader), check that what was opened is this
launcher's own — a regular file with exactly one name (`ownTemp`) — then
truncate, write every byte, `fsync` the file, `close`, `rename` onto
`<id>.json`, `fsync` the directory. Outcomes:

| Failure point | Outcome | Why |
|---|---|---|
| open, ownership check | `notWritten`, and the entry in the temp path is left exactly as found | nothing was truncated, written, renamed or removed |
| truncate, write (incl. short), fsync(file), close | `notWritten` — the authoritative file is certainly unchanged | the rename was never issued; the temp is unlinked (a failed unlink leaves an explained leftover) |
| rename | `uncertain` | treated as possibly applied (effect-then-error); the caller resolves it |
| fsync(directory) | `uncertain` | the rename is visible but its durability is unproven |

**Withdrawal** (`store.remove`, returns `(unlinked, err)`): `unlink(<id>.json)`
(ENOENT = already absent; `unlink(2)` never removes a directory). A failed
unlink is followed by an `Lstat`: the file still there → `unlinked = false`
with the error (the record, and its evidence, stay); the file gone → the
unlink acted despite its error, continue. Then `fsync` the directory; its
failure is `unlinked = true` with an error: the file is gone but its absence
is not yet durable.

A temp path occupied by something else is refused and preserved: a directory
(`EISDIR`), a symlink (`ELOOP`) or a FIFO without a reader (`ENXIO`) by the
kernel itself; a FIFO with a reader, a device, or a regular file with another
name (a hard link) by the ownership check, before anything is truncated or
written through it.

## 3. The record and its holdings

`Record` (persisted inside the envelope and returned on the wire) carries the
owner, the charge (`cpus`, `memory_mib` + `OverheadMiB`, one guest), the
deadline, the network scope, `cid`, `net_index`, `state`, `reason`, the job's
`label`, the obligation a teardown began under (`cleanup_trigger`,
`cleanup_due`), a quarantined record's `incident` and the record's revision
`rev` (`INTEGRATION.md` §8.1), and the **holdings** — what the launcher may
own on the host for this id:

| Holding | Set (durably, before the effect) | Cleared (only after) |
|---|---|---|
| `has_disk` | before `PrepareDisk` | `RemoveDisk` + `RemoveJail` succeeded |
| `has_cgroup` | before `CreateCgroup` | `RemoveCgroup` succeeded |
| `has_network` (+`net_index`) | before `CreateNetwork` | `RemoveNetwork(id, net_index)` succeeded |
| `has_vmm` (+`pid` once known) | before `StartVM` | the pid is verified gone after TERM/KILL (`Liveness`: an answer the host cannot verify is never gone; INTEGRATION.md §11.2), or the host said the start had no effect (`ErrNoEffect`), or the record is released |

A holding is "may exist", never "exists": an effect that fails, or reports an
error after acting, leaves its holding set, and the idempotent `Remove*` proves
absence later. `has_vmm` with `pid == 0` is a VMM whose start outcome is
**unknown**: the core cannot prove it absent. Automatic cleanup removes
nothing from under it. Only the operator's cleanup retry looks for it, on a
host that can scan for the exact `--id <id>` (`VMMFinder`): none found by
a complete scan resolves the holding (the finding and its time recorded), a
VMM found is stopped, verified, and an incomplete scan (one that failed, was
cut short or could not classify a process) or a host that cannot look
resolves nothing. A release is refused while the holding stays: release is never
proof that anything is gone.

If the publication of an intent fails in any way, the effect is not
attempted and the holding is undone **in memory** (memory says what may exist
on the host). A durable record that may still name the holding is dealt with
by the release, which withdraws the record durably or keeps the charge; a
restart then reads the conservative record.

States: `preparing` (reserved; holdings only while a create runs or after it
was interrupted), `running` (VMM started, pid recorded), `stopping` (a
teardown began: written durably **before** any removal or quarantine decision,
and loaded as `quarantined` after a restart, §5), `quarantined` (a teardown or
rollback could not finish, or a teardown's outcome was never recorded;
`reason` says why, `incident` its history; only the operator's separate
release, once it holds nothing, releases it). `reaped`/
`destroyed` exist only in `History` — a released record has no file. **Every
record in memory counts against the budget**, whatever its state.

### 3.1 Identifiers, room, and what a host reports

| Identifier | Domain (allocator = loader, unless noted) | Allocation |
|---|---|---|
| vsock `cid` | [3, 0xFFFFFFFE]: 0–2 are the hypervisor's, the local and the host's; 0xFFFFFFFF is `VMADDR_CID_ANY` | at admission: from a rotating cursor, the first value no held record has (`firstFree`); past the top it starts again at 3; the whole domain held is a refusal (`ErrOverBudget`), never a wrap to 0 or an alias |
| `net_index` | allocated in [1, 16383] = `[MinNetIndex, MaxNetIndex]`. The adapter derives both /30 subnets from 14 bits of it (`netAddrs`: one to one there, index 16384 aliases index 0). The loader also accepts 0, which launchers before this bound allocated first | at `Create`: the first index no held record names; the record carries it (in memory) from that instant until its release, also before its network intent is published, so no other create is given it |

At open, the cursors start past the highest value recovered (inside the
domain). They are hints only: the allocators skip every value a held record
has. A test in the command package ties `MaxNetIndex` to the adapter's
addressing.

**Room.** Admission encodes the would-be record and refuses a request whose
envelope would leave less than `recordGrowthBytes` (128 KiB) of the loader's
1 MiB. That room covers everything a lifecycle adds: a reason of
`maxReasonBytes`, a disk path of `maxPathBytes`, the obligation a teardown
began under and an incident's history — at most 8 attempts and a release,
each of bounded text (`maxDetailBytes`, `maxLeftEntries`) — (at most 6
encoded bytes per byte of any of them), a pid, a network index, the
holdings, timestamps and a revision. So no later publication of an
acknowledged record, nor its archived evidence, can outgrow what the loader
reads, and the encoder's own size check stays a final guard.

**What a host reports** is recorded only if the record still reloads.
- A disk path that is not valid UTF-8, or is longer than `maxPathBytes`, is a
  failed `PrepareDisk`: rolled back, the holding removed.
- A `StartVM` that returns no error but a pid ≤ 0 may still have left a
  process. The holding stays, no pid is recorded, and it is quarantined as a
  VMM of unknown pid; nothing is removed from under it.

## 4. Operations (per record, `Service`)

Every operation that can cause a host effect or a durable change holds the
record's exclusive **operation token** (`entry.busy`); tokens are taken and
given back under the service mutex, which also guards every record field.
Waiters capture the entry pointer and never re-resolve the id, so an operation
queued behind one incarnation can never act on a later one with the same id;
and a waiter decides on the record **as its wait left it**, never on what it
saw before waiting.

| Op | Precondition (else refusal, no change) | Durable steps and host effects | Result |
|---|---|---|---|
| `Reserve` | open, valid request, its request token — when named: always on the wire — valid and never admitted, settled or being settled before (`ErrSettled`; an unreadable ledger `ErrUnsafeState`: INTEGRATION.md §11.10), each value within `maxUnit`, a deadline after now (`ErrExpired`; no maximum), not fenced, id free (a quarantined record keeps its id), fits what is left of the budget (compared per request: no sum can wrap), a free cid (§3.1), a record that reloads as itself and keeps its room (§2.2, §3.1; else `ErrInvalid`) | insert the entry (charged, operation token held; the record's incarnation token fresh from the kernel's random generator, or the request refused, nothing charged — INTEGRATION.md §11.1) → the request's ledger entry (*admitted*, naming the reservation) kept durably in `requests/`, else the entry dropped and nothing acknowledged (§11.10) → `publish(preparing)` | `written`: acknowledged. `notWritten`: entry dropped, error. `uncertain`: `remove`; success → dropped, error; failure → kept charged as an **unacknowledged `preparing`** reservation (no holdings, not a quarantine): its owner may destroy it, its deadline reaps it; error `ErrRetained` naming it (the reply stays empty; wire `retained` + id, incarnation token, cid, created) |
| `Create` / `CreateOf` | owner (`CreateOf`: the incarnation its `Ref` names, else `ErrStale`, untouched), `preparing`, no holdings (an interrupted create is torn down, never resumed), not busy, **before the deadline** (`ErrExpired`: refused before it takes the record, nothing published, the reaper releases it), not fenced, image installed, destinations inside the current ceiling, a free network index when networked (§3.1) | per step: may it act (not cancelled, before the deadline) → holding → `publish` → may it act, again, just before the effect (if not, the holding is undone in memory and the effect never attempted) → host effect under the create's context (the deadline; a destroy, the reaper or `BeginStop` cuts it short), the VMM start under `startBound` and cut a quarter of the allowance after the earliest trigger of a rollback (the job's deadline, a destroy's request, the stop's): a VMM of unknown pid; a step answering `ErrNoEffect` records no holding; then a usable pid (in memory at once), may it act once more (a start that ended after a rollback was asked for, or at or past the deadline, is rolled back, never published) and `running` → `publish`; a rollback runs from its trigger | success: pid. A failed intent, a host error or unusable report (§3.1), a failed `running` publication, a cancel request, or the deadline reached before a step or an effect → **rollback** = teardown (releasing, final `destroyed`): released → "…(rolled back)"; quarantined → `ErrQuarantined`; halted or unconfirmed (§4.1) → "…; rollback not finished", charged, not quarantined |
| `Connect` / `ConnectOf` | owner (`ConnectOf`: the incarnation its `Ref` names, else `ErrStale`), not busy, `running`, before the deadline (`ErrExpired`: a connection is how work reaches the guest) | host `Connect` | as the host answers |
| `Stop` / `StopOf` | owner (`StopOf`: the incarnation its `Ref` names, else `ErrStale`), not busy | `Kill TERM` of a known pid only; no state change, no removal (on a quarantined record too: it ends execution, it clears nothing; the real adapter signals only a process it verifies as this id's VMM, and answers an error for one it cannot verify) | as the host answers |
| `Inspect` / `List` / `All` / `Budget` | `List`: an authoritative inventory, else refused (INTEGRATION.md §11.7); `Inspect`: owner — an id that is not loaded is `ErrUnknown` only on an authoritative inventory, else refused | none (a snapshot under the mutex) | copies (`All` and `Budget`: the loaded records, a presence) |
| `Settle` | a valid request token and its attempt; the owner's (else `ErrNotOwner`), that attempt's (else `ErrStale`); for a request no held record carries, an authoritative inventory and a readable ledger | a request never seen: its closure kept durably in `requests/` before the answer; an entry found: certified first (INTEGRATION.md §11.10); counted as work no token holds until it ends, so no terminal call gives the state up before it, and refused (`ErrClosed`) once the service stops (§11.11) | *admitted* (the held record; a reserve still publishing it is waited for), *released* (its reservation's evidence), or *closed*; a refusal settles nothing |
| `Released` | the incarnation named not held, an authoritative inventory, its evidence kept, this owner's (else: `ErrState` held, `ErrNotDurable` pending, `ErrUnproven`, the fence's) | reads its evidence in `released/` | its disposition — who released it, the obligation, the cleanup's end, the confirmation, late or not (INTEGRATION.md §11.8) |
| `Destroy` / `DestroyOf` | owner (`DestroyOf` names an incarnation — its token; a record written before tokens by cid and created; on the wire every destroy names one: INTEGRATION.md §11.1. An incarnation not held — an unknown id, another incarnation of the id — is answered nil, untouched, only on the durable evidence of its release, this owner's, on an authoritative inventory; else refused: `ErrUnproven`, or the fence's error — INTEGRATION.md §§11.6–11.8) | its request is the trigger: the cleanup, the wait below included, ends by one allowance after it (or after the job's deadline, when that came first); waits for the token (a running create gets a cancel request, its step in progress cut short); then, on the record as the wait left it: **quarantined → refused untouched** (no host call, no durable change); an unconfirmed withdrawal → finish it; otherwise teardown (releasing; begun — or finished — at or after its obligation's deadline, a late recovery: quarantined, never released; INTEGRATION.md §11.3) | nil once released; `ErrQuarantined` ("… only the operator's clear releases it: a cleanup retry, then a separate release of the inspected incident (urgit-vm-launcher recover)"; wire `quarantined: true`) — also for a late recovery, whatever it removed; `ErrNotDurable` for a halted teardown or an unconfirmed withdrawal |
| `RetryCleanup` (operator) | the selection's exact incarnation (else `ErrStale`) is `quarantined` **and no operation holds it** — it never waits: an operation in progress may end in an outcome nobody inspected, and a create is never asked to roll back | teardown (`retrying`, one allowance from the request): a VMM of unknown pid looked for where the host can (`VMMFinder`) and stopped, verified, if found; removals only when no VMM may run; a record already quarantined writes no new `stopping`; the attempt recorded in its incident and published (`rev` moves) | the inspection after it; `ErrQuarantined` while anything is left unresolved; **never a release** |
| `Release` (operator) | the selection's exact incarnation **and revision** (else `ErrStale`), `quarantined`, no operation holds it (`ErrState`; also while a cleanup is still publishing its outcome), nothing held (`ErrUnresolved`: release is never proof that anything is gone) | **no cleanup**: the evidence (the record, `released`, with a final `operator release` entry) published first to `released/<id>.<token>.json` (a record without an incarnation token: `<id>.<cid>.<created>.json`) (`archive` → `publishAt`; this incarnation's own earlier evidence is kept, anything else there refuses), then the record withdrawn (`remove`) | nil once released and durable; any failure keeps the charge; an unconfirmed withdrawal is finished by a retried release |
| `Incidents` / `InspectIncident` / `InspectRecord` | — | none (a snapshot; `InspectRecord` from a durable record alone, for the read-only CLI) | the incident as the operator sees it: label, exact incarnation, revision, obligation (missed or met), attempts, holdings, charge, whether release is allowed and why |
| `ReapOnce` | not closing | per candidate, re-validated under the mutex; busy → skipped (a running create past its deadline gets a cancel request): a pending accounting → finish it (INTEGRATION.md §11.8); an interrupted teardown found at open → its owed cleanup, once (retaining); deadline passed (`running`/`preparing`) → teardown (releasing, final `reaped`); `preparing` with holdings → teardown (releasing, final `destroyed`); any other quarantined record → untouched; each teardown on its own goroutine, ending by its deadline plus one allowance (a busy create past its deadline is asked to roll back, the deadline its trigger); no pass waits for another's teardowns (`Reap`; `ReapOnce` waits for its own) | the record's own outcome; one that does not release is reported (`SetReport`) — a late one always, never released |
| `BeginStop` | — | refuse new ops — a settlement or a recertification too: one waiting is refused as it wakes (INTEGRATION.md §11.11); the stop request becomes the trigger of every teardown the stop runs; every create in progress asked to roll back for it, its cancellable step cut short, its start within a quarter of the allowance; nothing torn down | — |
| `Shutdown` | no other terminal call owns the service's end (else it waits, §4.3) | take the end; refuse new ops, drain every token but teardowns' (a teardown has its own deadline) — the drain spends the stop's allowance; then unconfirmed withdrawals finished, `running`/`preparing` torn down side by side (releasing), each by one allowance after the stop request (or the job's deadline, when that came first) — also one another operation leaves so, once — quarantined records left untouched; wait for every token, then for every settlement and recertification in progress — no teardown waits for these, its deadline bounds the wait, and past it the lock is kept (INTEGRATION.md §11.11); release the lock; give the end back. Alone (no `BeginStop`) it cuts no create short, as before: a teardown that begins after the stop's allowance is a late recovery — quarantined, never released (ruling A) | aggregated error; a drain that times out gives the end back, keeps the lock and tears down nothing; a wait for a settlement that times out keeps the lock too |
| `Close` | no other terminal call owns the service's end (else it waits, §4.3) | take the end; refuse new ops, drain every token and every settlement or recertification in progress (INTEGRATION.md §11.11), release the lock; give the end back | — |

### 4.1 Teardown — the one cleanup path

`teardown(e, final, why, how, by)`, `how` ∈ `releasing` (owner destroy,
deadline, create rollback, interrupted create, shutdown) · `retaining` (the
owed cleanup of an interrupted teardown; and a releasing teardown begun at or
after its obligation's deadline — a late recovery) · `retrying` (the
operator's cleanup retry):

1. **`stopping` first.** A record not already quarantined publishes
   `stopping`. If that publication is not `written`, the teardown **halts**
   (`halt`): a known live VMM is still stopped (that only ends execution),
   nothing is removed, nothing is quarantined or released; the record returns
   to its previous state with the halt as its reason, stays charged, and the
   caller gets `ErrNotDurable`. The owner's retry or the reaper's next pass
   starts over. (After an uncertain failure the file may already say
   `stopping`; a restart then loads it quarantined — nothing was removed, so
   that is conservative, never unsafe.)
2. **VMM.** The teardown ends by its cleanup obligation's deadline:
   `CleanupBound` after the earliest trigger that asked for it, carried
   through any wait before it and through a halted teardown's retry
   (`obligationLocked`), never restarted at the teardown's entry nor per
   step; it is handed to a `Bounder` host as one deadline for every call. A
   known pid verified running → TERM, wait until a quarter of what is left
   has passed, KILL, wait until half has; a pid verified gone clears the
   holding, and one the host cannot verify is never signalled nor taken for
   gone (INTEGRATION.md §11.2). A teardown
   that begins after its deadline has passed (the launcher was not running,
   a halted teardown retried late) is LATE: a late recovery (ruling A) — one
   bounded allowance from its start, `retaining`; its reason and report say
   LATE and its incident records the missed deadline. An unknown pid → a
   problem, nothing removed from under it, except in the operator's retry,
   which looks for it where the host can: none found resolves it, a VMM
   found is stopped as above, an incomplete scan or a host that cannot look
   resolves nothing (the VMMs an incomplete scan verified are still
   stopped).
3. **Removals**, only when no VMM may run, before the deadline:
   `RemoveNetwork`, `RemoveCgroup`, `RemoveDisk`, and `RemoveJail` only for
   a record that may hold the jail (a disk holding or path, or a VMM), each
   clearing its holding on success. A removal the deadline cuts short is a
   failed removal.
4. **Outcome**, decided after the last host effect has returned and before
   anything is withdrawn: a `releasing` teardown that finishes at or after
   its obligation's deadline missed it, however it began, and ends as a late
   recovery does — `retaining`, its incident missed, its caller answered
   `ErrQuarantined` ("… when its cleanup finished"), reported LATE
   (INTEGRATION.md §11.3).
   * any problem, or `how` other than `releasing` → **quarantine**
     (`quarantine`): `quarantined` + reason, and the attempt recorded in the
     record's incident — opened by its first quarantine, with the
     obligation's trigger and deadline and whether it was missed — published.
     If that publication fails, the reason says so and the durable `stopping`
     written in step 1 still holds the quarantine (a restart loads it
     quarantined). The caller gets `ErrQuarantined` wrapping the host causes —
     or nil for a `retaining` or `retrying` cleanup that met no problem
     (retained by rule), except a late recovery, whose caller asked for a
     release: `ErrQuarantined` ("released nothing: its cleanup obligation's
     deadline had passed …").
   * otherwise the release is **authorized** (late-accounting ruling 01;
     INTEGRATION.md §11.8) and `finishAccounting` completes it:
     - its **evidence** first. This is the record as `released`, with its
       disposition: this exact incarnation, the obligation's trigger and
       deadline, the cleanup's verified end, *timely cleanup*, its
       accounting pending. It is kept through the one publication path in
       `released/` (`archive`). `archive` makes `released/` if missing,
       refuses anything but a real directory there, and fsyncs the state
       directory — `released/`'s link — at every keeping; evidence found
       there already has its bytes and its name fsynced again
       (INTEGRATION.md §11.9);
     - then `remove`: the unlink, then the directory fsync;
     - then its capacity returns. The entry leaves the map, `gone` is set,
       and `History` gets the final record. The evidence then records the
       confirmation's instant, and *late* when that instant was at or after
       the deadline (`rearchive`). That is late bookkeeping: reported apart
       (`released`, `LateAccounting`, an owner's destroy's too), never
       claimed on time. Evidence that could not record the confirmation
       reads pending and counts as late;
     - a failure of any of these steps leaves it **pending**, not a
       quarantine. It stays charged (`acct`, `stopping` in memory), the
       caller gets `ErrNotDurable`, and the next destroy, reaper pass or
       shutdown retries it.

       Before the ruling, a record file that could not be withdrawn was
       quarantined ("cleanup succeeded but the record could not be
       withdrawn").

       A restart certifies the records directory first (§2.1). A `stopping`
       record that its evidence backs is then a pending release (§5), which
       the first reaper pass finishes once it has certified that evidence
       (INTEGRATION.md §11.9); one without evidence is an
       interrupted teardown, quarantined. A restart that cannot certify the
       directory is fenced and answers no absence as a release
       (INTEGRATION.md §11.6).

**Release rule.** Capacity is freed only when every holding was cleared by a
successful host operation (or an explicit no-effect answer, or the operator's
retry finding no VMM of the id) **and** the record's withdrawal is durable,
by a releasing teardown begun and finished before its obligation's deadline, after its
release's evidence is durable (INTEGRATION.md §11.8) — and a
quarantined record is freed by nothing but the operator's separate release,
once nothing is held, its evidence published first. An incarnation not held
is answered released only from its evidence, never from its absence.

### 4.2 Quarantine authority (§9)

| Path | On a quarantined record |
|---|---|
| owner `destroy` (wire and core) | refused untouched: `ErrQuarantined`, wire `quarantined: true`, no host call, no durable change — also when it became quarantined while the destroy waited (a create's rollback, the deadline reaper, another destroy, a late recovery, the operator's retry) |
| owner `stop` | TERM to a known pid only; the record stays quarantined |
| owner `connect` / `create` / `reserve` of the same attempt | refused (not running / not preparing / id held) |
| deadline reaper | never (it reaps `running`/`preparing` only) |
| restart | `quarantined` loads quarantined; `stopping` loads **quarantined** — its owed cleanup runs once (retaining) and it stays quarantined |
| shutdown | untouched |
| late recovery (a teardown begun after its deadline) | quarantines it, never releases it |
| operator retry (`recover`, root CLI; `serve` stopped) | cleanup only, on the selected incarnation, never waited for; it stays quarantined |
| operator release (`recover`, root CLI; `serve` stopped) | the only release: the exact incarnation and revision inspected, idle, holding nothing; its evidence first |

The rules, each tied to the code and its tests:

* **R1** — The owner never releases a quarantine; `Destroy` decides on the
  record as its wait left it (`Destroy`, `refusalLocked`).
* **R2** — No quarantine is decided without durable evidence that loads as
  quarantined: a teardown writes `stopping` first, or halts (`teardown` step
  1, `halt`).
* **R3** — A quarantine whose own publication fails is still held by that
  durable `stopping` (`quarantine`).
* **R4** — A `stopping` record at open is **never** read as proof that no
  quarantine occurred: it loads quarantined (`interruptedTeardown`,
  `Snapshot.Interrupted`), its cleanup is owed and performed once by the
  reaper (`entry.recover`, `retaining`), and that cleanup releases nothing.
* **R5** — The reaper, a restart and shutdown never release a quarantine
  (`ReapOnce`, `Shutdown`), and no cleanup of a quarantined record does: the
  owed recovery, a late recovery, the operator's retry (`teardown`,
  `RetryCleanup`).
* **R6** — The operator's release is the only release of a quarantine:
  separate from any cleanup, on the exact incarnation and revision the
  operator inspected, only once nothing is held, never while an operation
  holds the record, never waiting into a new outcome; its evidence is
  published first (`Release`, `selectLocked`, `archive`).
* **R7** — A pending accounting is not a quarantine: an authorized release
  whose evidence or withdrawal is not durable yet stays charged until a
  retry confirms it (`entry.acct`, `finishAccounting`; INTEGRATION.md §11.8).
  Its evidence counts as durable only once this process has certified every
  link down to it (`accounting.evidenced`, `archive`; INTEGRATION.md §11.9).
  Before late-accounting ruling 01, only an unconfirmed withdrawal was; a
  record file that could not be withdrawn was quarantined.
* **R8** — An uncertain reservation whose withdrawal fails is not a
  quarantine: it is an unacknowledged `preparing` reservation with no
  holdings, charged until its owner's destroy or its deadline (`Reserve`).
* **R9** — A missed obligation is an incident (ruling A): a teardown begun at
  or after its obligation's deadline releases nothing, whatever it removes;
  its record is quarantined, its incident records the missed deadline, and
  its caller is answered quarantined (`teardown`'s late recovery).

**The trade-off R4 accepts.** A process death anywhere inside a teardown —
even one about to release — leaves `stopping`, which cannot tell a release in
progress from a quarantine whose publication failed; so it needs the
operator's retry and release afterwards. Conservative by design; distinguishing the two
would need another durable state, which is not added.

### 4.3 The service's end

A `Shutdown` or a `Close` **owns the service's end** (`terminating`) from
before its drain until after its state lock is released. That covers the
interval between the drain and the teardowns, which then run side by side,
where no operation token of the `Shutdown`'s is held. `Shutdown`'s drain waits
for every operation but teardowns in progress (they end by their own
deadlines, and it waits for them before the lock goes), and spends the
stop's allowance: the teardowns after it end by one allowance after the stop
request; `Close`'s waits for every token.
- A `Close` or another `Shutdown` that arrives meanwhile waits
  (`awaitTerminalLocked`). It then finds the lock released and returns: it
  never closes the lock under a terminal sequence that can still act.
- So no new opener acquires the state directory while an older `Shutdown` may
  still remove a record or its host resources.
- A `Shutdown` whose drain times out tears down nothing, keeps the lock (an
  operation is still running), and gives the end back. A later `Close` or
  `Shutdown` then ends the service.
- Work no record's token holds — an unheld settlement's closure or
  certification, the records directory's recertification — is counted
  (`unheld`) from its beginning, which only an open service admits, to its
  end on every path. `Close` waits for it with the tokens. `Shutdown` waits
  for it after its teardowns, which never wait for it, within its caller's
  deadline; past that it keeps the lock (INTEGRATION.md §11.11). Before
  independent review 05's correction a settlement could outlive the lock:
  a second service admitted its request, and its closure then replaced that
  admission's ledger entry and answered *closed* (R5-1).
- Once the lock is released, the service writes nothing more. `closing` stays
  set, every operation answers `ErrClosed`, and the reaper stops at its next
  candidate.

The `Config.at` point `shutdown: drained` sits exactly where review 02's
scheduling overlay held the old `Shutdown`: after its drain, before its first
teardown. So that interleaving is exercised on every test run. A second
point, `reaper: candidates taken`, holds a reaper pass after its snapshot and
before it looks at any candidate: the stale-candidate tests use it, since the
pass no longer pauses between candidates.

## 5. Open, restart and fencing

`NewService` creates the state directory if absent (never its parent) and
takes the lock (`ErrStateBusy` if another Service holds it: a second process
and a second Service in one process are both refused; the lock dies with its
process). It then certifies the state directory's link, creates `attempts/` if
absent, certifies that link too, and then `attempts/`'s own contents — a
failure there fences the service rather than failing the open (§2.1, at
every open). It refuses an
`attempts` that is not a directory or cannot be listed, classifies every entry
(`readState`), and starts the allocation cursors past the highest cid and
network index recovered (§3.1):

| Entry | Classification |
|---|---|
| `<id>.json`, regular, format 1, strict decode (unknown fields and trailing data refused), id = file name = `IDFor(prefix, attempt)`, owner/charge/state/holdings valid | loaded, charged — a `stopping` record loaded **quarantined** (reason "teardown interrupted: its outcome was never recorded and may have been a quarantine; only the operator's release, after its cleanup is resolved, returns its charge (was: …)"), its cleanup owed; unless a timely cleanup's evidence of exactly its incarnation backs it (`released/`, read only through a real directory), when it is loaded as that **release, its accounting pending**, and the first reaper pass certifies that evidence and then withdraws it (late-accounting ruling 01; INTEGRATION.md §§11.8, 11.9) |
| `<id>.json.tmp`, regular with one name, `<id>` in this namespace | leftover publication: never authoritative (every effect waits for a *completed* publication), reported, overwritten by the next publication |
| unreadable, malformed/partial, larger than 1 MiB, another format (incl. the pre-repair flat records), identity mismatch, another prefix, an unknown or terminal state, an invalid charge or holdings, a cid outside [3, 0xFFFFFFFE], a network index outside [0, 16383], a non-regular entry anywhere, a temp with more than one name, any other name | **problem**: the entry is left byte-for-byte, the service opens **fenced** |
| two loaded records sharing a `cid`, or a `net_index` with `has_network` | both loaded (charged, cleanable) and **fenced** |

A fenced service refuses `Reserve` and `Create` with `ErrUnsafeState` and the
diagnosis, and keeps doing everything that only reduces what exists for the
records it loaded (reaper, destroy, the operator's retry and release,
inspect, connect). It answers no absence as a release: the owners' list, an
id that is not loaded (its destroy, inspection or selection) and a stale
destroy are refused, never answered short, done or unknown: any
unaccounted entry may be the record asked about, and a duplicate means the
directory is not what this launcher wrote (independent review 02;
INTEGRATION.md §11.7). Neither a reaper pass nor a healed barrier lifts
this fence; only an open that accounts for every entry does. It is never silently "empty": the problems are logged
at `serve` start and printed by `list`, and the operator resolves them by
hand, then restarts. Nothing is migrated or deleted.

Loaded records need no host call at open. `serve` logs every quarantined
record, then its first reaper pass (before the first tick) performs the owed
cleanup of each interrupted teardown — which stays quarantined — and tears
down each interrupted create (`preparing` with holdings: no quarantine can
have been decided for it, since every decision follows a durable `stopping`).
A record with `has_vmm` and no pid stays quarantined until the operator's retry
resolves it and the operator releases it. An idle `preparing` record (acknowledged — or published but never
acknowledged, when a process died between the rename and the reply, or an
uncertain reservation whose withdrawal failed — and never created) and a
`running` record keep their charge until the owner destroys them or the
deadline reaper does.

**Retry and release need `serve` stopped** (a disclosed implementation
limitation, not a ratified trade-off). `recover`'s retry and release take the
state lock, so they are refused with `ErrStateBusy` while `serve` runs; its
listing and inspection only read the state directory and work beside it. And
stopping `serve` runs the ownership-scoped `Shutdown`, which tears down
**every** running or preparing VM this launcher owns — not only the incident
being recovered. No root-only retry or release over the live socket exists;
none was added.

## 6. Fault inventory

| Where | Fault | Outcome |
|---|---|---|
| reserve publication | open/write/fsync/close fails | not acknowledged, nothing durable, charge dropped |
| | rename or directory fsync fails | not acknowledged; withdrawn durably → dropped; withdrawal fails → charged unacknowledged `preparing` (owner destroy or deadline) |
| | a directory/symlink/FIFO/hard link occupies the temp path | `EISDIR`/`ELOOP`/`ENXIO`/ownership refusal, not acknowledged, evidence untouched, nothing blocks; a restart fences on it |
| | a directory appears at the record path | the rename fails (`EISDIR`), the withdrawal cannot unlink a directory: charged unacknowledged `preparing`, directory untouched; a restart fences on it |
| create intent publication | any failure | the effect is not attempted, the holding is undone in memory; rollback |
| host create step | error without effect (`ErrNoEffect`: nothing done, or the object exists already and is not this reservation's) | holding cleared, for every step; nothing it did not make is removed; rollback |
| | does not end by itself | cut short at its bound (the deadline, or a destroy, the reaper, `BeginStop`); reported as possibly acted: the holding stays and the rollback removes it; a start is cut a quarter of the allowance after the earliest trigger: a VMM of unknown pid, quarantined, never reported stopped |
| | ends after its rollback was asked for, or past the deadline | not published: its VMM rolled back, by the trigger's deadline |
| | error after acting (plain error) | holding stays; rollback removes it; `StartVM` → VMM unknown → quarantined |
| create result publication | any failure | the started VMM is killed in rollback; a later restart that lost the pid quarantines |
| teardown `stopping` publication | any failure | **halt**: a known VMM stopped, nothing removed, no quarantine, previous state and charge kept, `ErrNotDurable`; retried by the owner or the reaper |
| host removal | error (with or without effect) | holding kept, quarantined; the operator's retry retries it (its idempotent removal proves absence); only a separate release frees it |
| VMM survives TERM+KILL | — | quarantined naming the pid, within half of what the teardown has left |
| host removal | does not end by itself | cut short at the obligation's deadline: a failed removal, quarantined |
| teardown | begins after its deadline (the launcher was not running; a halted teardown retried late) | a late recovery: one bounded allowance from its start, never released — quarantined, its incident recording the missed deadline, reported LATE; the obligation was not met |
| | begins in time, finishes at or after its deadline (a host effect that returned late, even after its context was cut) | the same: a missed obligation — quarantined, charged, its incident missed, its caller answered quarantined, reported LATE (INTEGRATION.md §11.3) |
| VMM liveness | a command line that cannot be read, a proc root that is not there, a command line that mentions the id otherwise than as `--id <id>` | unknown: never signalled, never taken for gone; nothing removed from under it (INTEGRATION.md §11.2) |
| quarantine publication | any failure | quarantined in memory (the reason says so); the durable `stopping` holds it; a restart loads it quarantined |
| release evidence | not written, not certified, or renamed with an error after it acted | pending accounting: nothing is withdrawn before it, charged, retried (INTEGRATION.md §11.8) |
| the evidence namespace | the state directory's fsync (`released/`'s link) failing, at `released/`'s creation or at any later keeping; evidence found already there, its own fsync or `released/`'s failing; `released/` not a real directory (a symlink, a file) | pending accounting (the operator's release: not done, still an incident); each keeping issues the state directory's fsync again, EEXIST notwithstanding; nothing is kept or read through a non-directory (INTEGRATION.md §11.9) |
| | its update after the confirmation fails | released (the accounting is confirmed); the evidence reads pending and counts as late |
| withdrawal | unlink fails, file still there | pending accounting: charged, not quarantined, retried (INTEGRATION.md §11.8; before late-accounting ruling 01: quarantined, "cleanup succeeded but the record could not be withdrawn") |
| | unlink acts, then reports an error | looked up: gone → the directory fsync decides |
| | directory fsync fails | unconfirmed withdrawal: charged, not quarantined; the next destroy, reaper pass or shutdown confirms it and releases — late bookkeeping when that is at or after the deadline, reported apart |
| request ledger | the admission's entry not durable (its write, rename — before or after acting — or fsyncs) | not acknowledged, nothing charged, the record never published; its settlement closes the request (INTEGRATION.md §11.10) |
| | a settlement's closure not durable | refused (`ErrNotDurable`); the request is admitted no more in this process's life; a retry, or after a restart the next settlement, closes it |
| | an entry found whose certification fails (its own fsync, `requests/`'s, the state directory's) | refused, never answered from |
| | `requests/` not a real directory | nothing is admitted with a request, nothing settled, nothing kept or read through it |
| admission | a request large enough to wrap `used + requested` | refused (`maxUnit`, then per-request comparison with what is left) |
| | a deadline not after now | refused (`ErrExpired`), nothing charged or written |
| | a request whose record would not reload as itself (e.g. a string that is not UTF-8) or would leave less than its room | refused (`ErrInvalid`), nothing charged or written |
| | every cid held | refused (`ErrOverBudget`), never a wrap |
| open | a certification fsync fails (the state directory's parent, or the state directory) | the open fails (`ErrNotDurable`), the lock released; the next open certifies again before anything is acknowledged |
| | the records directory's own fsync fails | the open is fenced, not failed: the loaded records enforced, nothing admitted or booted, no absence answered as a release (the owners' list refused); each reaper pass retries it, and a success lifts the fence (INTEGRATION.md §11.6) |
| | an entry the loader cannot account for (the classification in §5) | the open is fenced, not failed: the loaded records enforced, nothing admitted or booted, no absence answered as a release (the owners' list, an unknown or stale destroy, an inspection and a selection refused); the entry left as found; only an open after the operator's resolution lifts the fence (INTEGRATION.md §11.7) |
| | the state directory's parent is missing | the open fails; nothing is created above the state directory |
| create | the deadline reached before a step or just before an effect | the effect is never attempted (a holding recorded for it is undone in memory); rollback; a VMM never started is not taken for one of unknown pid |
| | every network index held | refused before the record is taken (`ErrOverBudget`) |
| | the host reports an unusable disk path | a failed `PrepareDisk`: rollback |
| | the host reports no error but a pid ≤ 0 | a VMM of unknown pid: quarantined, nothing removed from under it |
| terminal calls | a `Close` or another `Shutdown` while a `Shutdown` owns the end | it waits; the lock is released only after the owner's last action |
| | a `Shutdown` whose drain times out | nothing torn down, the lock kept, the end given back to a later call |
| | a settlement or a recertification in progress | `Close` waits for it; `Shutdown` runs its teardowns, then waits for it within its deadline, past which it returns the count and keeps the lock; no second service opens the state meanwhile (INTEGRATION.md §11.11) |
| wire client | the launcher answered (a refusal) and closed before reading the request | the client reports the answer, not its failed write |
| process death | anywhere | the lock dies with the process; the durable record names every holding that may exist; restart: `stopping` → quarantined with owed cleanup, interrupted create → torn down, unknown VMM → quarantined |
| second opener | another Service holds the lock | `ErrStateBusy`; `recover`'s retry and release must wait for `serve` to stop |
| concurrent ops | create vs destroy/reap/shutdown | one token; destroy/shutdown wait, create sees the cancel request at the next step |
| | owner destroy vs a quarantine reached meanwhile | the destroy decides after its wait: refused |
| | operator retry or release vs any operation in progress | refused at once (neither waits), a create is not cancelled; a release asked while a retry publishes its outcome is refused too |
| operator retry | the host cannot scan, or the scan fails or is incomplete (a process it cannot classify, a look cut short) | the unknown VMM stays held; nothing removed from under it (the VMMs it verified are stopped); unresolved, recorded |
| | a VMM found by the scan survives TERM and KILL | held, unresolved; nothing removed |
| operator release | a stale selection (another incarnation, an older revision, a replay) | refused (`ErrStale`, or `ErrUnknown` once released — on a partial inventory `ErrUnsafeState`, never unknown: INTEGRATION.md §11.7); nothing changed |
| | anything still held | refused (`ErrUnresolved`); nothing changed |
| | the evidence cannot be published, or something else occupies its path | refused, charged; the record and the foreign file untouched |
| | the withdrawal's unlink or directory fsync fails | charged; the evidence kept; a retried release, or the reaper's next pass, finishes it |
| stale work | a waiter or reaper candidate whose entry was released | sees `gone`, does nothing; the new incarnation is a different entry |
| | a destroy naming an incarnation that is gone | answered nil; the id's current incarnation untouched (`DestroyOf`) |
| | a create, connect or stop naming an incarnation that is gone | `ErrStale`; the id's current incarnation untouched (`CreateOf`/`ConnectOf`/`StopOf`) |
| | the same `(id, cid, created)` again after an empty restart within one second | another incarnation token: the stale reference names nothing (INTEGRATION.md §11.1) |
| wire | a create, connect, stop or destroy that names no incarnation | refused (`ErrInvalid`): there is no bare-id route |
| stop | a create in progress | `BeginStop` cuts its step short, it rolls back; `Shutdown` never waits for a teardown or a rollback in progress before starting the others |
| malformed hello | a hello without a daemon id | refused; the server no longer dereferences the missing owner (which used to crash the launcher) |

## 7. The host adapter (`main.go` `realHost`) and what is conditional on it

Stage 01 rewrote this adapter's command, cgroup and network handling;
`INTEGRATION.md` §7 is its model. In short: every command runs through
`runCommand` under the operation's bound, as the leader of its own process
group (the jailer excepted: its `--daemonize` calls `setsid`), killed with
its group at the bound and reported as possibly acted; the adapter's
cgroups, named namespaces and `/proc` are rooted (tests use private
models); `cgroupPaths` names the launcher's leaf and the jailer's cgroup
under it, and `RemoveCgroup` removes that subtree deepest first; every create
step proves its per-attempt object absent and refuses without effect
otherwise (`CreateNetwork` also lists the table and the veth, creates the
per-VM chain with `nft create chain`, tolerates no `File exists`, and reads
the egress interface without a shell); `Kill` pins the process (pidfd)
before it verifies it; `listen` unlinks only a socket. What follows is the
round-three text, with its items now marked where stage 01 addressed them.

* `StartVM` wraps `ErrNoEffect` only when no jailer process ran (the
  `vm.json` write or chown failed, or the jailer could not be started); a
  jailer that ran and failed, or no pid verified as its VMM within 10 s,
  stays uncertain.
* `RemoveNetwork(id, index)` removes the rules naming the veth `"vh<index>"`
  or the namespace address, matched as exact tokens, then chain
  `vm-<index>`, then the namespace. The index comes from the record: a retry
  after a partial removal — the namespace already gone — still removes the
  per-VM chain, instead of reporting success and leaving a policy a later VM
  with the same index would inherit.
* `Kill` signals a pid only while its command line verifies it as this id's
  VMM (`--id <id>`); a process it cannot verify is not signalled (an error),
  and a foreign or vanished one is left alone (INTEGRATION.md §11.2).
* Host commands go through `realHost.command` (`runCommand` in
  production); tests record commands without executing them.

**Bounded Host methods (stage 01).** Round three stated this as conditional:
the core ended every teardown in a release, a quarantine, a halt or an
unconfirmed withdrawal only if each Host call returned, and the adapter's
commands ran without a timeout. Now every call of a teardown carries the
teardown's obligation deadline (one allowance after its trigger), create
steps carry the create's context, a start is cut a quarter of the allowance
after its trigger, and the adapter kills a command's process group at its
bound. The launcher's stop ends every teardown by one allowance after the
stop request, the drain included. The durable publications of outcomes are
not bounded by that deadline (an explicit, unqualified bound); the unit's
`TimeoutStopSec=180` (`KillMode=mixed`) is systemd's outer backstop, not the
obligation and no extra cleanup time. This is established at source level
only: the production launcher is still **not shown on a real host** to
satisfy RIDER-CI-P4-04 line 14 — NOT RUN.

**Round three's open items, and stage 01.** The jailer's cgroup layout
(`--parent-cgroup <cgroup_parent>/<id>` while `RemoveCgroup` removed only the
leaf) is coupled at source: one function names both, and the removal handles
the jailer's cgroup under the leaf. `CreateNetwork`'s reuse of a per-VM chain
is gone: it refuses a preexisting chain or rule, and creates the chain new.
The unit's stop timeout is set. **Still not established** (qualification, NOT
RUN): the real jailer's cgroup behaviour and systemd delegation for the
launcher's slice (QUESTIONS-SOURCE-01 §2), real `ip`/`nft` listing formats and
semantics, SELinux, installation, and a real start's and stop's timing.

## 8. Deadlines

The selected absolute deadline is enforced where each kind of work begins,
not only by the periodic reaper, which is no permission to begin expired work:
- **admission** takes only a deadline after now;
- **`Create`** is refused at or after it, before it takes the record;
- **every provisioning step and every host effect** is checked before its
  intent and again just before the effect, so no disk, cgroup, network or VMM
  start begins at or after it;
- **`Connect`** is refused past it.

A deadline that passes while a VMM already runs is enforced by the reaper
itself, whatever the daemon is doing. Past it, a `running` or `preparing`
record is torn down, and a create in progress is asked to roll back at its
next step. This happens within one 5-second tick plus the teardown. Effects
already begun are handled as before: rolled back through teardown, or
quarantined when that cannot finish. All of this enforces the **selected**
deadline; it does not prove the deadline was **authorized**. `Reserve`
accepts any future deadline the runner names (the daemon takes the assignment's
`deadline-seconds`, one hour when absent, `internal/daemon/run.go` :54–57;
rider 04 sets it from the job's `timeout-minutes`, one hour only when that is
omitted), and the launcher has no maximum of its own. None is invented here: an administrator
ceiling would be a separate policy. Cleanup and reporting never extend
execution: the VMM is stopped first, and even a halted teardown stops a known
live VMM.

## 9. Process stop, process death, power loss

* **Process stop** (`Close`/`Shutdown` in-process, then a new `NewService`):
  tokens drained, lock released, the next open sees exactly the durable state.
  Under systemd, `serve`'s stop is bounded (`BeginStop`, then `Shutdown`:
  every teardown by one allowance after the stop request, publication time
  aside), and the unit waits `TimeoutStopSec=180` before it kills — an outer
  backstop — signalling the launcher alone (`KillMode=mixed`).
* **Process death** (tests: a re-executed test binary killed with `SIGKILL`
  by its exact pid at a barrier inside a publication or a host effect): no
  drain; the kernel releases the lock; the durable state is whatever the
  completed publications left, plus explained temps. A teardown killed midway
  leaves `stopping`: loaded quarantined, its cleanup owed; only the operator's
  release, after resolution, frees it.
* **Power loss**: the code issues `fsync` on the file and the directory and
  refuses to acknowledge when either fails, which is what the filesystem
  contract requires. No power cut was simulated; no claim is made.

## 10. Memory that grows

`Service.hist` gets every record this process releases
(`finishAccounting`) and is never trimmed: in a long-running `serve` it grows
without bound. `History()` is called only by tests
(`concurrency_test.go`, `service_test.go`); neither `main.go` nor the wire
reads it. No retention policy is set here; a bounded design is a separate
proposal. The same holds on disk for released incidents: their evidence
(`released/`) is kept and nothing prunes it (ruling A invents no retention
policy). Since late-accounting ruling 01, every release keeps one — a timely
cleanup's too — so `released/` grows by a file per job (QUESTIONS-SOURCE-01
§10). Since settled-admission ruling 01, `requests/` gains one small file per
reserve request, admitted or closed, and nothing deletes from it: pruning an
entry would make its request admissible again (INTEGRATION.md §11.10). In
memory, `Service.requests` — the requests this process began to settle —
grows likewise in a long-running `serve`.

## 11. Code map

| Concern | Where |
|---|---|
| publication, encoding, withdrawal, adapter, lock, classification | `store.go`: `encodeRecord`, `boundReason`, `boundText`/`boundLabel`, `normalized`, `store.publish`/`publishAt`, `archive` (the evidence namespace's certification: INTEGRATION.md §11.9), `evidenceDir`, `syncFile`, `ownTemp`, `store.remove`, `osFS`, `lockState`, `readState`/`ReadState`, `interruptedTeardown`, `decodeRecord`, `validateRecord`; domains `minCID`/`maxCID`, `MinNetIndex`/`MaxNetIndex`, sizes `maxRecordBytes`, `recordGrowthBytes`, `maxReasonBytes`, `maxPathBytes` |
| the namespace | `service.go`: `openService` (`certify`; the records directory's own fsync, `uncertified`), `recertify`, `absenceLocked` (the directory certified and every entry accounted for: INTEGRATION.md §11.7), `List` |
| identifiers | `service.go`: `firstFree`, `cycleNext`, `admitLocked` (cid; the incarnation token), `allocNetLocked`; `ref.go`: `newIncarnation`, `validIncarnation`, `Ref`, `Liveness` |
| tokens, waiting, release | `service.go`: `take`, `put`, `await`, `drop`, `entry` (`recover`, `acct`, `released`, `disposition`) |
| late accounting (INTEGRATION.md §11.8) | `accounting.go`: `Release`, `authorizeLocked`, `finishAccounting`, `accountingPending`, `Released`, `releasedAny`, `timely`, the evidence's readers; `store.go`: `archive`, `rearchive`, `readState` (`Snapshot.Authorized`) |
| intents, results, the deadline at every effect | `service.go`: `intend`, `commit`, `save`, `provision` (`mayAct`, `begin`, `undoIfNoEffect`), `Create`, `Connect` |
| bounds | `service.go`: `Bounder`, `hostWith`, `startBound`, `obligationLocked`, `cancelCreateLocked`, `entry.stop`/`cancelAt`/`cutStart`/`owed`/`late`, `Service.stopAt`, `retag`, `tearing` |
| admission | `service.go`: `Reserve`, `admitLocked`, `usage`; `settle.go` (INTEGRATION.md §11.10): `admissibleLocked`, `heldRequestLocked`, `Settle`, `settleUnheld`, `requestNote`, `readNote`, `keepNote`; `store.go`: `publishBytes` |
| cleanup, quarantine, release | `service.go`: `teardown` (`ending`, `incidentSeed`), `quarantine`, `halt`, `stopVMM`, `findVMM`, `liveLocked`, `Destroy`/`DestroyOf` (`destroy`, `refusalLocked`), `CreateOf`/`ConnectOf`/`StopOf`, `ownedOfLocked`; `incident.go`: `RetryCleanup`, `Release`, `Incidents`, `InspectIncident`, `InspectRecord`, `selectLocked`, `withAttempt`, `remaining`, `Selection` |
| recovery, deadline, reports | `service.go`: `Reap` (its first pass at once), `ReapOnce`, `reapPass`, `candidates`, `SetReport`, `reportOutcome`, `Outcome` |
| the service's end | `service.go`: `BeginStop`, `Shutdown`, `Close`, `awaitTerminalLocked`, `endTerminalLocked`, `reached` (`Config.at`), `drainLocked`/`drainAllLocked`/`drainUntil`, `closeLock`, `unheld` (a settlement's and a recertification's count: INTEGRATION.md §11.11) |
| wire | `proto.go`: `Server.handle` (quarantine and retained flags, a mutation that names no incarnation refused, a reserve that names no request refused, `settle`, hello refusal), `WireProtocol` 4, `Client.Settle`, `Server.mutate`, `refRequest`, `Client.call` (a refusal read after a failed write), `ReplyError`, `Retained`, `Client.Bind`, `Client.DestroyOf` |
| command | `main.go`: `serve` (the quarantine and outcome logs, `Reap`, `BeginStop`), `logOutcome`, `listen`, `list`, `recover.go` (`incidents`, `lookup`, `act`, `recoverScripted`, `recoverInteractive`), `realHost.FindVMMs`, `realHost.service`, `realHost.Bound`, `runCommand`, `waitExited`, `cgroupPaths`, `cgroupFS`/`osCgroups`, `realHost.PrepareDisk`, `realHost.CreateCgroup`, `realHost.CreateNetwork` (`netnsExists`, `linkExists`, `tableObjects`, `defaultDevice`), `realHost.StartVM`, `realHost.RemoveNetwork`, `realHost.RemoveCgroup`, `realHost.Kill`, `realHost.Liveness` |

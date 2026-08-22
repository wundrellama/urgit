# Transfer visibility: what landed, what it proves

Brief: `briefs/transfer-visibility.md`. Branch `feat/progress-and-joinall`.
Commits `9f6bece` (Tier 0) and `53a49aa` (Tiers 1 and 2).

**Tier 0, Tier 1 and Tier 2 are implemented and proven on live ships. Tier 3
was not reached.** T3a has no answer, and the reason is not the one the brief
expected: the `%prog` path was never reachable, because neither test ship ever
became a chum, so `%archive` never ran. §2 gives the evidence and §8 says what
would settle it.

Test ships: `~bud` at `/home/michael/piers/vb2`, HTTP 8096, Ames 31339, and
`~wes` at `/home/michael/piers/vw2`, HTTP 8097, Ames 31340. Both booted from
`pills/brass-408k-1.pill` on the Mesa-capable runtime. Repository `demo`, 117
objects, 31,249 canonical bytes, 13 commits, pushed over Smart HTTP.

**Running source, proved by an arm that cannot exist in the earlier build.**
`/x/visibility/build` is a new `on-peek` path. It answers
`'transfer-visibility-t0-t1-t2'` on both ships:

```
vb2 marker: [0 %avow 0 %noun 'transfer-visibility-t0-t1-t2']
vw2 marker: [0 %avow 0 %noun 'transfer-visibility-t0-t1-t2']
```

The earlier build has no such path, so it answers `~` and the scry crashes the
thread. No size comparison and no shared error string is used anywhere below.

---

## 1. Tier 0 — did the strand reproduce, and does it survive a reload now

### It reproduced, twice, and the brief's reading was right

The mechanism is exactly as the brief states. A `%request` is queued in
`peer-prepare-queue` and answered with `%accepted`; a `~s1` Behn timer fires
`/peer/prepare-start`; the handler looks the transfer up and builds the
snapshot. `on-load` set the queue to `~`, so a reload inside that window left
the timer with nothing to find, and it returned `` `this `` silently.

Hitting a one-second window needed a way to hold the wake. The reliable
sequence is:

1. fire the fork, so the `%request` is processed at about t+0.05s
2. `%kiln-commit` a compiled-core change to the server's desk at t+0.3s to
   t+0.5s

The Clay build then occupies the ship from about t+0.5s to t+8.5s. The `~s1`
wake fires at t+1.0s and queues behind it, so it is delivered **after** the
reload. Firing the fork *after* the commit does not reproduce it: Arvo
processes the reload as an effect of the build event, before any Unix event
queued during it, so the request lands on the new agent and behaves normally.
That is why `delta` values of 0.2s, 1.0s, 4.0s, 7.7s, 7.9s and 8.1s all
completed.

Two runs on the pre-fix build:

| run | fork answered | agent reloaded | outcome |
|---|---|---|---|
| `strand301`, commit at +0.3s | 05:29:48 | +8.6s | `active` until 05:39:48, then `failure` |
| `strand302`, commit at +0.5s | 05:30:16 | +8.9s | `active` until 05:40:16, then `failure` |

The failure message is `peer did not finish preparing the repository
snapshot`. Unreloaded forks of the same repository in the same session
completed in three to eight seconds.

**One correction to the brief.** The requester does not wait forever. It waits
for the `~m10` `/peer/prepare-timeout` armed in `peer-accepted`, exactly ten
minutes to the second in both runs, and then fails. The fork is still lost with
no recovery path — nothing retries, and the server built nothing — but the
transfer does terminate.

### After the fix, the same sequence completes

`peer-prepare-queue` is now a field of `state-2`, so `on-save` carries it, and
`on-load` keeps it and re-arms the `~s1` timer for every entry still queued.

| run | fork answered | agent reloaded | outcome |
|---|---|---|---|
| `t0fix2`, commit at +0.4s | t+0.016s | t+8.96s | `success \| fork complete` |
| `t0fix3`, commit at +0.5s | t+0.017s | t+9.21s | `success \| fork complete` |
| control, no reload | t+0.0s | — | `success \| fork complete` in 5.2s |

The reload is not merely claimed. `on-load` also wipes `peer-activities`, and
immediately after the `t0fix3` run the server's activity list holds **one**
entry — the serve of the very transfer that had been queued before the reload:

```
1 entries
  ~2026.08.22..06.26.33..792c success demo | repository snapshot delivered
```

An empty-then-one-entry list is the reload and the re-armed timer in the same
observation.

### The schema

`state-2` changed in place, per `AGENTS.md`: no `state-3`, no migration, no
compatibility shim. The brief's own instruction ("change the schema in place,
nuke and revive") is the one followed, over its preceding sentence about a
`state-3`. `migrate-state-1` produces the new field as `~`.

Both test ships took the new schema through a fresh `|install`, which runs
`on-init`. The `on-load` path — the one that matters — was then exercised four
times by the reload runs above, each of which decoded a `%2` vase containing
the new field. A pier holding a pre-change `%2` state must be nuked and
revived; `!<` will crash otherwise, by design.

---

## 2. T3a — does `%prog` deliver on a chum

**No answer, and not because `%prog` failed. The path was never reachable.**

`peer-directed` decides `%archive` by scrying `//chums` and requiring the
target to be present, `%known`, with a lane and live QoS. On both ships, at
every point in the run:

```
vb2 chums: [0 %avow 0 %noun 126]      ::  126 is '~', the empty map
vw2 chums: [0 %avow 0 %noun 126]
```

The map is empty, so `peer-directed` is false, so `%archive` never ran. Every
transfer in this run took the `%objects` path, which Tier 1 now prints (§3).
`%archive` is the only mode on which `%prog` is legal, so there was nothing to
send `%prog` about.

What is confirmed, from the vane source at
`sys/vane/ames.hoon` (byte-identical in length, 524,590, to the copy read at
`/home/michael/piers/p1opus-sev/base/sys/vane/ames.hoon`):

- `pe-prog` takes `[dud spar task=$@(~ ?([%chum ~] [%keen key]))  freq=@ud]`
- it is a bare `!!` when `find-peer` returns `%ames`
- it is also a bare `!!` unless the peer state is `[~ %known *]`
- `find-peer` returns `%mesa` only if the ship is in `chums`; otherwise it
  falls back to `peers`, and only then to `core.ames-state`, whose default is
  `%ames`

So the brief's reading of `pe-prog` holds. What the brief does not account for
is that urgit issues no Mesa traffic of its own, and nothing else in this
two-ship setup does either, so `chums` stays empty and the `%archive` branch is
dead on a stock fake network. On the production moon it evidently is not — the
trial's four-minute transfer was an `%archive` — which means the chum was
established by traffic outside urgit.

Tier 3 was not attempted beyond this. Per the brief's stop condition, reporting
the negative and stopping is the answer. §8 records what a real T3a needs.

---

## 3. What a real fork now prints and shows, per tier

One fork of `demo` from `~wes` to `~bud`, with an Eyre channel subscriber
attached to `/peer/activity` on each ship. Times are seconds from subscribe;
the fork POST was fired at t=4.0.

### Tier 1 — the server's pier log

```
urgit: peer-prepare 0v1qn4.pr9eg.mm2cj.nkaiq.gnp7v.70ftd.03d8c.vpis2 -> ~wes demo: serving 117 objects, 31.249 bytes as %objects in 1 page
```

Transfer, target, repository, object count, byte count, mode and page count on
one line, at the moment the mode is decided. `31.249` is Hoon's dot separator
for 31,249. The same sentence becomes the serve activity's message, so the API
and the UI carry it without a second format:

```
[   5.013] BUD activity=1 top=active/demo/'serving 117 objects, 31.249 bytes as %objects in 1 page'
```

Before this change both the log and the message said nothing about the mode;
the activity read `repository snapshot requested`.

### Tier 2 — what the requester sees, pushed

```
[   0.000] PUT subscribe -> 204
[   0.015] subscribe
[   0.020] WES activity=1 transfers=1 top=failure/fork1/'peer did not answer the repository transfer request'
[   3.997] WES activity=2 transfers=2 top=active/fork2/'transferring repository'   progress=[(0,0,0,0),(0,0,0,0)]
[   5.036] WES activity=2 transfers=2 top=active/fork2/'transferring repository'   progress=[(0,0,0,0),(0,117,0,1)]
[   8.526] WES activity=2 transfers=2 top=success/fork2/'fork complete'
```

`progress` is `(received, expected, completedPages, pages)` per transfer.

- **t=0.020** — the watch itself answers with current state. That is `on-watch`
  giving a fact, not a poll.
- **t=3.997** — the fork POST returned at t=4.0 and the start fact is already
  out.
- **t=5.036** — a progress fact: the denominators arrive, 117 objects over one
  page.
- **t=8.526** — completion. The transfer took 4.5 seconds. On the old
  four-second poll this would have surfaced anywhere up to four seconds later,
  and the operator's original complaint was that it surfaced only when cancel
  forced a read.

The server side of the same fork:

```
[   5.013] BUD active/demo/'serving 117 objects, 31.249 bytes as %objects in 1 page'
[   8.539] BUD success/demo/'repository snapshot delivered'
```

**A browser attaching mid-transfer.** A fork was fired, and a *fresh*
subscriber attached 1.2 seconds later:

```
[   0.022] MIDWAY activity=3 transfers=3 top=active/fork3/'transferring repository' progress=[..., (0,117,0,1)]
[   3.233] MIDWAY success/fork3/'fork complete'
```

Twenty-two milliseconds after subscribing, with the transfer still in flight,
it has the transfer and its denominators. Not an empty screen.

**A failure surfaces just as promptly.** Forking a repository that does not
exist on `~bud`:

```
[   3.036] FAIL active/fork4/'transferring repository'
[   3.105] FAIL failure/fork4/'repository not found'
```

Sixty-nine milliseconds from start to failure. This matters because
`peer-fail` on the server and `peer-error` on the requester are a different
code path from `peer-finish`, and the brief is right that a UI which learns
about success instantly and failure in four seconds is worse than one that is
uniformly slow.

### Is the progress figure honest

Yes, and where there is no honest figure the code says so rather than
inventing one. `transferFraction` in `fe/src/transferFeed.js` uses, in order,
`fineFragmentsReceived / fineFragmentsTotal`, then `completedPages / pages`,
then `received / expected`, and returns `null` when every denominator is zero.
`transferBasis` names which one was used, so a caller can label it. All three
are counts the ship measured. Nothing is extrapolated and nothing is smoothed.

On this run the honest figure is coarse: `%objects` over one page means the
page counter reports 0/1 then 1/1, and `expected` gives 117 as a denominator
with `received` only moving at the end. That is the granularity Tier 3 exists
to fix, and Tier 3 was not reached.

---

## 4. What changed, per file

**`desk/sur/git.hoon`** — `+$ peer-prepare-request` and `+$ peer-prepare-entry`
added, and `peer-prepare-queue=(map @uv peer-prepare-entry)` added to
`state-2`. The entry type is declared here rather than imported from
`sur/git-peer`, because `git-peer` imports `git` and the dependency cannot run
both ways; it is structurally identical to `[target=ship request:git-peer]`, so
it nests where the packet type is expected.

**`desk/app/urgit.hoon`**

- the `=/ peer-prepare-queue` binding beside the state is gone; the name now
  resolves to the state field
- `migrate-state-1` produces the new field
- `on-load` no longer wipes the queue, and builds a `%wait` card per queued
  entry before the two Eyre `%connect` cards
- `on-peek` answers `/x/visibility/build` with a build marker
- `+$ peer-ui-state` and `+peer-ui-activity-json`, `+peer-ui-transfers-json`,
  `+peer-ui-json`, `+peer-ui-digest`, `+peer-ui-notify`, `+peer-ui-path` added
  to the outer core. They live there, not in the agent door, because the door
  is cast to `agent:gall` and may hold exactly its ten arms — the first
  attempt failed to build with `core-number-of-arms.exp=10.hav=14`. The outer
  core is also the only scope both `on-poke`'s `|^` block and `on-arvo` can
  see.
- `peer-activities-json` and `peer-results-json` are now three-line delegates
  to the two outer arms, so the two polling endpoints and the subscription
  fact share one implementation
- `on-watch` accepts `[%peer %activity ~]` and answers with a fact carrying
  current state
- `on-poke`, `on-agent` and `on-arvo` take a digest before, run their existing
  body into `result`, take a digest after, and append a fact if it moved
- `peer-prepare` computes a `note` in each of its three mode branches, uses it
  as the activity message, and `%-  (slog ...)` prints it
- `+peer-mode-note` and `+peer-mode-tang` build that sentence

**`fe/src/transferFeed.js`** (new) — `mergeFeed` normalises the activity poll
payload, the transfers poll payload and the subscription fact into one shape,
leaving keys a payload omits untouched. `transferFraction` and
`transferBasis` as described above.

**`fe/src/channel.js`** (new) — an Eyre channel client in about ninety lines
with no dependency. The brief says the frontend "already has the plumbing for a
channel connection"; it does not. There is no `@urbit/http-api`, no
`EventSource` and no `/~/channel` code anywhere in `fe/`. Rather than add a
dependency this uses Eyre's channel API directly, which is a PUT of
subscribe/ack actions and a GET of the same URL as an event stream.

**`fe/src/App.jsx`** — one `transferFeed` state replaces the two activity
arrays, the subscription is opened once from `publicApi.profile().ship`, and
the four-second poll runs only while the subscription is down.

**`fe/src/transferFeed.test.js`** (new) — ten cases over the projection and the
channel action shapes, including a payload that omits keys, a junk payload, the
precedence of the three progress denominators, and the `null` case.

**`fe/src/forkHandshake.test.js`** — its first test asserted the Tier 0 defect
(`=/ peer-prepare-queue` beside the state, `peer-prepare-queue ~` in
`on-load`). Rewritten to assert the fix, including the re-armed timer.

**`fe/src/readers.test.js`, `fe/src/streamedObjects.test.js`** — three
assertions here were **already failing on `HEAD`** before this run touched
anything, against earlier landed work: `peer-repository-browse-json` now calls
`public-repository-json-up-to`, and `peer-transfer-yawns` now gates on an
`issued` window rather than on `!(~(has in completed) revision)`. Verified by
stashing this run's `desk/` changes and re-running. Updated to the source as it
stands. One further assertion in `readers.test.js` was repointed from
`peer-results-json` to `peer-ui-transfers-json`, where that logic now lives.

---

## 5. The `feq` choice and its event cost

**There is none. Tier 3 was not implemented, so no `%prog` is sent and no
`feq` was chosen.** Picking a divisor without a live `%rate` gift to calibrate
against would be a number I did not measure.

The related cost this run *did* introduce is the `peer-ui-digest` comparison,
which runs twice per `on-poke`, `on-agent` and `on-arvo` event. It is built to
be cheap by construction: it reads `peer-activities` (capped at 50 entries of
scalars), `notification-activities` (capped at 50), the keys and values of
`peer-results` and `peer-outgoing`, and, for each entry in `peer-receiving`,
seven scalars plus two folds over `fine-progress`. It never touches
`objects.flight`, which is the only field that scales with repository size, so
the cost does not grow with the repository. **I did not measure it** — see §8.

---

## 6. The subscription design

**Path.** One: `/peer/activity`. It is named for the API surface it mirrors,
`/apps/urgit/api/peer/activity`, rather than a parallel vocabulary.

**Facts.** Mark `%json`. The body carries three keys — `activity`,
`notifications`, `transfers` — which are exactly the keys the two existing
polling endpoints serve, with identical contents. No third shape was invented.
`+peer-ui-json` is the single producer; `peer-activities-json` and
`peer-results-json` now call into it, so a poller and a subscriber cannot drift
apart.

**When a fact goes out.** Not per call site. The brief asks for a fact at every
`peer-activity-finish` — there are thirteen — plus every start and every
progress update. Threading a card through all of them is a large edit to a
368 KB file with a real chance of missing one, and it would still miss the
places that edit `peer-activities` inline (the supersede path in
`peer-prepare`, the `%serve-timeout` handler). Instead each of the three
state-changing entry points compares a digest of everything the UI shows,
before and after, and appends one fact if anything moved. Nothing can be
forgotten, because nothing is enumerated. The observed consequence is the run
above: start, progress and completion facts all arrive, on both the success
and the failure path, with no per-site wiring.

`on-peek` and `on-leave` are not wrapped; they change no state.

**Reconnect.** `on-watch` gives the new subscriber a fact carrying current
state immediately, so a browser that attaches mid-transfer renders the
transfer rather than waiting for the next event — measured at 22 ms in §3. The
client treats any transport error, any `quit`, and any failed subscribe as the
same thing: tear the channel down and open a **new** one, with exponential
backoff from 1s to 15s. A new channel means a new `on-watch`, which means
current state again. There is no attempt to resume an old channel, because
resuming one correctly needs the ack bookkeeping to be right and a fresh
subscribe is idempotent here.

**Fallback.** Both polling endpoints keep working, unchanged, and the browser
runs the four-second poll whenever `transfersSubscribed` is false — which
includes before the first subscribe lands and during every reconnect gap. A
dropped subscription degrades to the old behaviour instead of breaking.

**A fact to a subscriber that has gone away.** Gall owns that: it routes
`%give %fact` to the paths' current subscribers and drops the rest. The agent
emits the card unconditionally and never inspects the subscriber set, so there
is no code path here that can fail on a closed browser. Across every run above
the pier log has no `crud` and no `bail`.

---

## 7. Which other transient maps carry the Tier 0 defect

`on-load` wipes twenty bindings. Fixing them was explicitly out of scope; this
is the written finding the brief asked for instead. Ranked by whether a user
loses work.

**Same defect, same shape, same fix.**

1. **`peer-browse-prepare-queue`.** Structurally identical to the one that was
   fixed: a job is queued, a timer is armed, the handler looks it up. A reload
   inside the window leaves the requester holding a `%browse-accepted` until
   the `~m10` `/peer/browse-prepare-timeout`. Move it into `state-2` and re-arm
   `/peer/browse-prepare` on load. This is the one I would do next; it is the
   same twenty lines.

**Loses in-flight work, but a timeout eventually reports it.**

2. **`peer-stream-jobs`.** Losing this mid-stream is not theoretical — I
   watched it happen. A reload during an `%objects` transfer produced
   `peer announced invalid streamed object bounds` on the requester, three
   times in the pre-fix sweep. The server has already sent `%begin-objects`
   and the requester is expecting pages that the server can no longer produce.
   Persisting it means persisting the remaining object list, which is most of
   the repository, so the honest fix is probably to fail the transfer cleanly
   on load rather than to persist: send `%error` for every `peer-serving` entry
   whose job is gone, so the requester learns at once instead of decoding a
   short page.
3. **`peer-receiving`.** The requester's own in-flight transfer, including
   every object received so far. A reload discards it silently: no activity
   entry survives either, so the fork simply vanishes from the UI while the
   server keeps serving into a `~d1` `/peer/archive-timeout` or its
   `serve-lifetime`. Persisting it is expensive for the same reason as
   `peer-stream-jobs`. A cheap partial fix is to persist only the flight
   header and re-request.
4. **`peer-serving`.** The mirror image. On load the server forgets what it is
   serving, so the requester's `%archive-accept` or Fine scries find nothing
   and it waits out its own timeout. Same trade-off: the `objects` list is the
   repository.
5. **`peer-outgoing`.** A push or pull-request offer in flight. `peer-result`
   from the peer hits `?~ outgoing  \`this` and is dropped, so the offer never
   resolves — but `/peer/offer-timeout` does eventually fail it.
6. **`pending-clay` and `pending-publish`.** A desk publish or a Clay-linked
   push in flight is abandoned. `pending-clay` has `/clay-timeout`;
   `pending-publish` I did not trace.

**Leaves an HTTP client hanging.**

7. **`in-flight` (LFS), `github-in-flight`, `webhook-in-flight`,
   `github-results`.** Each holds the `eyre-id` of a request that is waiting
   for an answer. On reload the outstanding response arrives, finds no context,
   and is dropped, so the browser or the `git-lfs` client hangs until Eyre's
   own timeout. Not data loss, but a visible hang with no message.

**Visibility only, which is Tier 2's subject rather than Tier 0's.**

8. **`peer-activities`, `notification-activities`, `peer-results`.** History,
   not work. Worth noting anyway: `peer-results` is what
   `waitForPeerTransfer` polls, and losing it makes an otherwise healthy fork
   report `ship no longer tracks this peer transfer` after eight polls. Under
   the new subscription the UI recovers current state on reconnect, which
   softens this but does not remove it.

**Harmless.** `request-count` resets to 0; it only mixes with `eny.bowl` for
transfer identifiers, so a reset changes nothing.

---

## 8. What I could not measure and why

**Whether `%prog` delivers a `%rate` gift on a chum.** The whole of T3a. Two
fake ships on a stock fake network never enter each other's `chums` map, so
`peer-directed` is false, `%archive` never runs, and `pe-prog`'s Mesa branch is
unreachable. Settling it needs one of: a pair booted with
`[%load %mesa]` sent to Ames **before** first contact, so both land in `chums`
rather than `peers` — `find-peer` checks `chums` first and never promotes an
existing `peers` entry; or a thread that sends `[%prog spar task freq]`
directly and takes the resulting sign, which tests `pe-prog` without needing
urgit to reach it. I did neither. Everything in §2 about `pe-prog` is read from
the vane source, not run.

**The event cost of the `peer-ui-digest` comparison.** §5 argues from its
construction that it cannot scale with repository size, and the largest
repository it ran against here holds 117 objects, which proves nothing about
9,119. No timing was taken. The measurement that would settle it is the
difference in `on-poke` wall time with and without the wrapper on a transfer
with many `peer-receiving` entries.

**Whether the four-second poll is genuinely off during a subscription.** §3
shows facts arriving 22 ms to 69 ms after the event, which is far inside any
poll interval, so the facts are certainly not the poll. The React side of it —
that `setInterval` is actually cleared while `transfersSubscribed` is true — is
covered by reading the effect and by the unit tests on the projection, not by
driving a browser. No browser was run in this session; every subscription
measurement above came from a Python client speaking the same Eyre channel
protocol.

**Whether a fact to a departed subscriber is safe.** Argued from Gall's
routing, and supported by the absence of any `crud` or `bail` in the pier logs
across roughly a dozen forks with subscribers attaching and detaching. Not
tested by killing a subscriber mid-fact.

**Anything about `%pack`.** Every transfer in this run resolved to `%objects`;
117 objects in one page is well inside `peer-stream-max-objects`. The `%pack`
branch's Tier 1 line is written and compiles but was never printed. The
regression the brief cares about most — that a `%pack` or `%objects` transfer
still completes and sends no `%prog` — holds trivially, since no `%prog` is
sent anywhere: the `%objects` transfers above all completed, and `grep -c
'%prog' desk/app/urgit.hoon` is 0.

**The production-scale case.** The trial's 9,119-object `%archive` over four
minutes is the thing Tier 3 exists for, and nothing here approaches it. The
largest repository this run moved was 31,249 bytes in under five seconds.

---

## 9. Conformance vectors

All nineteen generators in `desk/gen` built out of the **running** desk with
`.^(vase %ca …)`, `slot 3`ed to their gate and slammed. Every one built and
ran; the ones that assert with `?>` would have crashed rather than change a
`mug`.

| generator | `mug` | | generator | `mug` |
|---|---|---|---|---|
| `git-access-vector` | `0x58f6.45c3` | | `git-ofs-delta-pack-vector` | `0x0f9d.cb95` |
| `git-archive-vector` | `0x58f6.45c3` | | `git-pack-decode-vector` | `0x16f7.8288` |
| `git-blame-vector` | `0x5b53.27e0` | | `git-pack-vector` | `0x05a2.80f3` |
| `git-clay-vector` | `0x523f.8fda` | | `git-shallow-vector` | `0x664d.5ac4` |
| `git-codec-vector` | `0x5f23.4243` | | `git-stock-pack-vector` | `0x74cf.c008` |
| `git-delta-pack-vector` | `0x4402.c735` | | `git-storage-vector` | `0x1356.dfd3` |
| `git-github-vector` | `0x58f6.45c3` | | `git-tree-vector` | `0x3385.41cb` |
| `git-gzip-vector` | `0x0295.709b` | | `git-webhook-vector` | `0x58f6.45c3` |
| `git-inflate-vector` | `0x16f7.8288` | | `git-zlib-vector` | `0x16f7.8288` |
| `git-migration-vector` | `0x58f6.45c3` | | | |

**19 of 19 match `GZIP-REQUEST.md` §5c**, the mapping the brief names as
current.

`fe/src/*.test.js`: 88 of 88 pass, up from 85 of 88 on `HEAD` — see §4 for the
three that were already red.

---

## 10. Left undone

- **Tier 3 entirely.** T3b, T3c and T3d are not written. T3a is unanswered for
  the reason in §2, which is a different reason from the one the brief's stop
  condition anticipated, so it is recorded rather than treated as a negative.
- **The nineteen other transient maps.** Findings only, §7, as the brief
  directed.
- **`waitForPeerTransfer` still polls** `/peer/transfers` every 750 ms while a
  fork modal is open. It is the fallback path and rewiring it to the
  subscription is a larger frontend change than Tier 2 asked for. The activity
  panel — the thing that showed "transferring repository" for four minutes
  during the trial — is fully push-driven.
- **A browser was never driven.** §8.

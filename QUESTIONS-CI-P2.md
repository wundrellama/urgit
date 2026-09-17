# CI P2 questions — astra

Rider 1 resolved §1–§3; those sections were removed in S0 (now `a0f38e7`
after the operator's rebase). S1 is complete at `25aa23f`, with Q2–Q4
evidence in `.scratch/p2-live-table.md`.

Rider 2 resolves §4 and §5 in the brief re-frozen at `682c8cf`, launch
HEAD `f589b4e`. The two answered sections remain below because Rider 2
requests their deletion in the S2 commit. S2 is not complete: compilation
exposed the additional lexical-scope issue in **§6**, the only
open question. The incomplete implementation is saved locally as
`.scratch/tmp/s2-rider2-draft.patch`; product source is restored to launch
HEAD. No S2 or later row is claimed complete.

## §4 — The new stage action requires changing the existing push caller

**Read:** `BRIEF-CI-P2.md:191–198` permits exactly three `%urgit` touches:
the web merge route, `land-candidate`, and repository JSON. It also requires
`%stage-candidate` to gain `pull=(unit @ud)` in `sur/ci.hoon`.
`desk/app/urgit.hoon:8723–8727`, in `handle-receive-pack`, constructs the
existing action with `[ship via]` as its tail and explicitly casts it to
`action:ci`. That caller is outside all three permitted touches.

**Tried:** Ran an isolated type probe in the fresh `~lup` dojo, using the
required extended mold and the push caller's existing tuple shape:

```hoon
`[%stage-candidate repo=@t ref=@t head=@ux base=@ux actor=@p via=?(%session %token) pull=(unit @ud)]`[%stage-candidate 'r' 'refs/heads/master' 0x1 0x2 [~lup %session]]
```

Observed:

```text
mint-nice
-need.[via=?(%session %token) pull=u(@ud)]
-have.%session
nest-fail
dojo: hoon expression failed
```

Transcript: `.scratch/tmp/s2-pull-shape.log`. No product mold or `%urgit`
source was changed for the probe. An optional value still requires a tuple
slot; the old cast does not supply it.

**Question:** May S2 change the push action constructor as a fourth touch?

- **Permit that call-site change (recommended):** expand the existing
  `actor` tail into `-.actor +.actor ~`, explicitly supplying no pull for a
  push. Keep the action mold uniform and leave `%urgit` state unchanged.
- **Keep exactly three touches:** revise the contract to use a distinct
  pull-staging action from the web merge route, leaving `%stage-candidate`
  unchanged for pushes. This requires changing the brief's action choice;
  it does not require a compatibility shim.

## §5 — Trust classification needs an authoritative writer read

**Read:** D3 requires staging trust to use the actor's repository `can-write`
result, including owner, explicit writers, and `%write` group capability.
`desk/lib/git-access.hoon:74–81` is pure and requires all those inputs.
`desk/app/urgit.hoon:2415–2434` already resolves group policy and calls that
predicate in `repository-writable`.

**Tried:** Traced the available `%urgit` peeks at
`desk/app/urgit.hoon:9177–9316` and the existing controller's `urgit-peek`
adapter. The CI peeks expose file bytes, tree paths, and `[tip linked]`,
not access policy or a writer decision. `/repository/<name>` returns public
repository JSON only and explicitly answers `[~ ~]` for a private repository
(lines 9227–9233). The debug-state peek exposes transfer diagnostics, not
repository access policy. No existing actor-specific writer peek was found.
I did not issue a known absent/private scry, which can kill the reading event.

The permitted repository-JSON touch adds `ciUntrustedPolicy`; it does not
make private repositories readable or provide the actor-specific group
decision. Copying only owner/listed-writer logic would omit the required
group writer case.

**Question:** May S2 expose the existing writer predicate through a narrow
local CI peek, outside the three enumerated touches?

- **Add `/ci-can-write/<repo>/<actor>` (recommended):** have `%urgit` return
  a boolean using `repository-writable`, for private and public repositories,
  with false for an unknown repository. `%urgit-ci` uses its guarded peek
  adapter and refuses when the read is unavailable. This reuses the existing
  group checks and changes no persisted `%urgit` state.
- **Extend `/ci-ref` with access-policy inputs:** explicitly permit that
  peek change, then implement the group-seat resolution in `%urgit-ci` and
  update its callers for the new response shape. This duplicates access
  resolution and expands the interface beyond the single decision needed.

No push, merge, or rebase was performed. `%urgit` source and state are
unchanged by this chair.

## §6 — The writer helper is local to on-poke, outside on-peek's scope

**Read:** Re-freeze 2's fence touch 5 requires the new
`[%x %ci-can-write @ @ ~]` peek to reuse `repository-writable` and permits
nothing else in `%urgit`. At launch HEAD `f589b4e`, `on-poke` starts at
`desk/app/urgit.hoon:2272` and opens a local `|^` core at line 2278. Its
helper arms include `group-peek` (2329–2346), `group-seat` (2359–2386),
`repository-group-capability` (2406–2411), and `repository-writable`
(2425–2433). That local core closes at line 9168. `on-peek` is a sibling
arm beginning at line 9174, outside the helper scope.

**Tried:** Implemented touch 5 with the ratified call:

```hoon
``noun+!>(?~(found %.n ?~(actor %.n (repository-writable u.found u.actor))))
```

`zig build -Ddesk=<lup-pier>/urgit` copied successfully. The actual
`|commit %urgit` compilation on `~lup` refused the new peek:

```text
-find.repository-writable
/app/urgit/hoon::[9,298 42].[9,298 61]>
```

Evidence: `.scratch/tmp/s2-build2-pane.log` and
`.scratch/tmp/s2-build3.log`. The latter repeats the same error after
placing `ciUntrustedPolicy` in the authenticated repository GET handler,
which has the bowl needed for its guarded read. No access helper has
been moved or duplicated. No Q5a–Q8 assertion was run.

**Question:** May touch 5 include lifting the existing writer helpers into
the enclosing agent door so both entry points can call them?

- **Widen touch 5 to include the lift (recommended):** move `group-peek`,
  `group-seat`, `repository-group-capability`, and `repository-writable`
  to the agent door, keeping their bodies unchanged. Their existing
  on-poke callers resolve the same arms in the enclosing scope; the new
  on-peek branch can then call `repository-writable`. `group-members`
  and `repository-readable` can stay local and use the lifted dependencies.
  No state change, duplicate access logic, or additional wire protocol.
- **Keep the helpers local:** revise the interface to a request/reply
  through `on-poke`, where the predicate is callable. Staging and approval
  would wait for the reply and refuse an unavailable result. This changes
  the brief's synchronous guarded-peek contract and requires a separate
  protocol design; I would not invent it under the current fence.

This is a scope requirement for the already ratified predicate, not a
request to change which actors count as writers. Stopping under brief §6.

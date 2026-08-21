# Note for ~sitful-hatred — the fork progress indicator is dead code on Fine

Short version. The sub-page fragment counter in urgit has never worked and
cannot work on legacy Fine. The page-level counter does work. On a Mesa chum
the fragment counter becomes reachable for the first time, so this is worth a
look on your branch.

Kernel cites refer to `sys/vane/ames.hoon` at tag 408k-2. App cites refer to
`desk/app/urgit.hoon` on `feat/streamed-object-batches`.

---

## What does not work

Every transfer we ran reported the same two values:

```
fineFragmentsReceived: 0
fineFragmentsTotal: 0
```

Six runs, three piers, every time. The wire arm that would fill those in sits
at `app/urgit.hoon:8410-8428`. It is dead code, and it is unreachable by
construction rather than by accident.

The chain breaks in three places:

1. `%keen` reaches `on-keen`, which calls `fi-sub` (**8788**, called at 6715
   and 8731). `fi-sub` registers an **atom** `%sage` interest.
2. `fi-give-rate` (**8713-8717**) opens with `?@ int f`. That skips every atom
   interest, so a `%sage` listener never receives a `%rate` gift.
3. The `[%rate boq feq]` cell interest that `fi-give-rate` would deliver to can
   only be created by `fi-rat` (**8850**). **Nothing in the kernel calls
   `fi-rat`.** We grepped the whole vane.

The only task that reaches the rate machinery is `%prog`. Urgit never sends it.
Sending it on the Fine path would not help either. `pe-prog` (**14008-14013**)
is a bare `!!` when `find-peer` returns `%ames`. The poke crashes the Ames
event instead of reporting progress.

So the counter cannot move on Fine. This is not a defect in urgit's handler.
Nothing upstream can feed it.

---

## What does work

`peer-object-fragments` advances `progress-at` when a page completes
(`app/urgit.hoon:3317`). Our harness observations show that moving normally:
1/8, 2/8, and so on.

The cost is granularity. On Fine a full page takes about 17 seconds, so the
user sees a 17-second dead zone between updates.

The stall timeout reads the same signal. `app/urgit.hoon:8554` fires after
`~m2` without page progress. With 4 MiB pages a link must fall below about
35 KiB/s to trip it. We never saw a false stall, but the margin is thinner than
it was with 512 KiB pages.

---

## Why your branch changes this

`pe-prog` crashes only for **ames-core** peers. For a chum it takes the Mesa
branch and reaches `ev-add-rate` (**10435**). So on a Mesa peer, `%prog` should
work, and fragment-level progress becomes available for the first time.

That makes this the one user-visible improvement in the whole investigation
that needs no upstream change. It ships in a desk.

Two conditions:

1. **Guard on the peer's core before sending `%prog`.** Scry
   `chums/all` and check the tag. An unguarded `%prog` crashes the vane for
   every legacy peer, and legacy is still the default. On our moon that is 22
   of 25 peers.
2. **Send `%prog` alongside each `%keen`**, at `app/urgit.hoon:3135`, `3170`,
   `3333`, and `8407`, with a `feq` divisor that gives useful resolution
   without flooding the agent.

The existing `%rate` handler at `8410-8428` can then stay where it is and start
receiving real gifts.

---

## One caution on the numbers it will report

A fragment counter measures fragments, not bytes moved per second. Given
stop-and-wait on the Mesa bulk path, that counter will advance at roughly one
fragment per round trip. It will look smooth and slow. That is honest, and it
is a better instrument than the page counter for telling a stalled transfer
from a working one.

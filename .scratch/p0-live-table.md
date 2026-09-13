# P0 live table — native CI contracts and harness

Ship `~ryx` (Vere 4.6, brass-408k-1), HTTP port 8340, pier
`/var/home/michael/piers/urgit-ci-p0-ryx`, herdr pane `wN:p2`.
Desk built with `zig build -Ddesk=<pier>/urgit`, committed headlessly through
`.scratch/ci-p0/poke.sh hood kiln-commit '[%urgit %.n]'`, installed with
`|install our %urgit`. `act` 0.2.89, image `catthehacker/ubuntu:act-latest`,
`--network bridge --json --pull=false`. Run on 2026-09-12; raw logs in
`.scratch/tmp/` (ignored); the scripts that produced every row are in
`.scratch/ci-p0/` (committed).

Fixture repository `ci-fixture` (public-read), `refs/heads/master` protected
(today's rule) and CI-protected (D3). Commits:

| name | OID | role |
|---|---|---|
| ONE | `132d04eac9c3401762ad9fb1074c55c9e7f37511` | workflows + README, landed before CI protection |
| TWO | `08c7950aa3abd8c9146b8831ead541a5a607882d` | landed before CI protection; the base tip for H3–H5 |
| THREE | `2316f90dc5bb1589ba7b17329ed1f26886918d8a` | H3 direct push; fast-forward candidate `0v2.qppqk.277k7.kuvjj.2nh94.phqih` |
| DIVERGE | `52ef234be299a3aa93125cf94f3bac831e9890be` | H5 head based on ONE, force-pushed; candidate `0vnlmu6.179je.n6jgi.igqij.sbevg` |
| MERGE | `eb8e8a81a78204f3b4ef14ae1cb8bf6a34d7a567` | H5 materialized merge of DIVERGE onto TWO |
| FIVE | `f3d61e8617d25968bff5f33f6740bde444750a56` | H14 passed candidate `0v2.78ptr.nk906.o8rm8.h7esf.h7l0d` |

Daemon A `0v6.6hdt0.n7d2q.jurur.u3ktn.74qv2` (H6), daemon B
`0vthjtl.q3fp2.pmk75.6b74q.lpmel` (H12). Attempts: A1
`0v1.88c98.r3ocp.o7p5h.rnpil.vav3s` (H7/H8), A2 `0v3.m4dja…` (H9/H12/H13),
A3 `0v2.qoo4j…` (H10), A4 `0v6.tji9l…` (H11), A5 `0v4.if4rp…` (H14).

## Rows

"RED shown?" gives the refusal output observed verbatim for each negative
row, and for the rows with a natural before/after inside the harness the
before state as well.

| Row | Expect | Observed | RED shown? |
|---|---|---|---|
| H1 | `%set-ci-protected ci-fixture refs/heads/master` before `%storage` is configured → refused with the D9 message | `:urgit-ci &ci-action [%set-ci-protected 'ci-fixture' 'refs/heads/master' %.y]` → `'ship object storage is not configured; CI cannot be enabled'` / `dojo: app poke failed`; D3 scry stays `%.n` | yes: the quoted crash line and `dojo: app poke failed` |
| H2 | after `%storage` is configured (six `%storage-action` pokes, endpoint `http://127.0.0.1:1`) → accepted | poke acked `>=`; `.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)` → `%.y` | n/a (positive); H1 is its before |
| H3 | push THREE → `ng refs/heads/master staged as ci candidate <id>; checks pending`; ref unchanged; objects kept | `! [remote rejected] master -> master (staged as ci candidate 0v2.qppqk.277k7.kuvjj.2nh94.phqih; checks pending)`; master oid `08c7950…` (TWO) before and after; objectCount 10 → 13; `%urgit-ci` has one candidate, `status=%pending`, `head`=THREE, `base`=TWO, `candidate=~` | yes: the `! [remote rejected]` line |
| H4 | `%materialize` → `%candidate-ready` with `candidate == head` | after `:urgit-ci &ci-action [%materialize 0v2.qppqk…]`: `candidate=[~ 0x2316.f90d…8d8a]` = head, `conflict=%.n`, still `%pending` | n/a |
| H5 | diverging push from a second clone → staged; materialize → two-parent commit, parents correct, ref unchanged | clone-b reset to ONE, force-push DIVERGE: `! [remote rejected] master -> master (staged as ci candidate 0vnlmu6.179je.n6jgi.igqij.sbevg; checks pending)`; materialized `candidate=[~ 0xeb8e.8a81…a567]` ≠ head; exposed on `refs/ci/candidate` via `%set-ref` and fetched: `git rev-list --parents -n 1` → `eb8e8a81… 08c7950a… 52ef234b…` (first parent TWO, second DIVERGE); master still `08c7950…`; objectCount 19 | yes: the first run (clone-b based on TWO) was correctly judged a fast-forward (`candidate == head`), which is the RED that forced the divergent rewrite |
| H6 | mint → enroll → daemon-id; long-poll → 204; `%assign` by poke; long-poll → the assignment | `:urgit-ci\|mint-enroll-token` printed the token; `POST /ci/daemon/enroll {token}` (no session) → `200 {"daemon-id":"0v6.6hdt0…","bearer":"0vv.t03b0…"}`; re-enroll same token → `401 enroll token is not recognized`; poll with no credentials → `401 daemon authentication required`; wrong bearer → `401`; after `[%assign 0v2.qppqk… 0v6.6hdt0… ~]` the poll answered `200 {"assignment":{…"attempt":"0v1.88c98…","oid":"2316f90d…","trust":"trusted","deadline-seconds":3600…}}` once; the next poll → `204` after 25 s | yes: the two 401s; also the first run's `401 bad session auth` from Eyre, which is why the bearer moved to `x-ci-bearer` |
| H7 | real `act` on the candidate checkout, each `--json` line relayed by a shell loop; `outputs.suite == 'true'` | `act push -W .github/workflows/fixture-pass.yml -j pass …` → 15 lines, all `202`; attempt scry: `status=%running events=15 outputs={[p='suite' q='true']} job-result=[~ %success]` | n/a |
| H8 | `POST /result job-result success` → candidate `%passed`; push the candidate OID → lands | `200 {"status":"passed",…,"candidate-status":"passed"}`; candidate `status=%passed`; D2 scry `%.y`; `git push` → `08c7950..2316f90  master -> master`; master oid = THREE | n/a |
| H9 | `/result success` with no `jobResult` event → 409; candidate stays `%pending` | new attempt A2 on the merge candidate, no events: `409 {"error":"no jobResult event was relayed for this attempt"}`; candidate `status=%pending` | yes: the 409 line |
| H10 | deadline passes with no result → attempt `%infrastructure-error`, candidate `%unknown`; push → still `ng` | `[%assign … \`~s20]` → assignment `"deadline-seconds":20`; before: attempt running, candidate `%pending`; after 28 s: `status=%infrastructure-error events=0 job-result=~`, candidate `status=%unknown`; `git push --force origin eb8e8a81…:refs/heads/master` → `! [remote rejected] … (staged as ci candidate 0v1.c1p3b…; checks pending)` | yes: before/after states and the rejected push |
| H11 | `fixture-fail.yml` → `jobResult: failure` → candidate `%failed`; push → `ng` | act on MERGE with `-j fail`: 26 lines, last `202 … "job-result":"failure"`; claiming success → `409 {"error":"job-result does not match the relayed jobResult event"}`; honest `failure` → `200 … "candidate-status":"failed"`; candidate `status=%failed`; push → `! [remote rejected] … (staged as ci candidate 0v1.c1p3b…; checks pending)` | yes: the 409 for the false claim and the rejected push |
| H12 | event to attempt A with B's bearer → 401 | daemon B enrolled; `POST /ci/attempt/<A2>/event` with `x-ci-bearer: <B>` → `401 {"error":"attempt authentication required"}`; same line with A's bearer → `202 … "events":1` | yes: the 401 line |
| H13 | 65 KiB event line → 413; attempt unaffected | 66,634-byte line → `413 {"error":"event line exceeds 64 KiB"}`; the next accepted line (H12) answered `"events":1`, so the refused line was not counted | yes: the 413 line |
| H14 | agent stopped → push a `%passed` OID → `ng` with the D2 outage reason; push an unprotected ref → `ng`; restart → both `ok` | FIVE staged/materialized/assigned/run/passed like H3–H8; `\|rein %urgit [%.n %urgit-ci]` → `gall: stopping %urgit-ci`, `.^(? %gu /=urgit-ci=/$)` → `%.n`; `git push origin master` → `! [remote rejected] master -> master (ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `git push origin master:refs/heads/side` → `! [remote rejected] master -> side (ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `\|rein %urgit [%.y %urgit-ci]` → `%gu` `%.y`; both pushes → `9aff263..f3d61e8  master -> master` / `master -> side` | yes: both rejected lines. Also a genuine RED first run: the brief's `\|suspend %urgit-ci` is a no-op (`\|suspend` takes a desk), liveness stayed `%.y` and both pushes LANDED (`2316f90..9aff263`); the row was rerun with `\|rein` |
| H15 | sign a GET for `%untrusted` from the `%trusted` attempt → `~` | attempt A1 `trust=%trusted`; `.^((unit @t) %gx /=urgit-ci=/sign-get/0v1.88c98…/untrusted/(scot %t 'cache.tar')/noun)` → `~`; `…/trusted/…` → `[~ 'http://127.0.0.1:1/ci-bucket/ci/ci-fixture/0v2.qppqk…/0v1.88c98…/trusted/cache.tar']` | yes: the `~` |

## Foreground suites (verbatim tails)

```
> +urgit!ci-event-vector
%.y
> +urgit!ci-storage-vector
%.y
> +urgit!git-migration-vector
%.y
> +urgit!git-access-vector
%.y
```

```
$ cd fe && npm test
1..126
# tests 126
# suites 0
# pass 126
# fail 0
# cancelled 0
# skipped 0
# todo 0
# duration_ms 1477.09379
```

## Dojo forms that work (D2, D3)

```
.^(@ud %gx /=urgit-ci=/state/version/noun)                                   → 0
.^(? %gu /=urgit-ci=/$)                                                      → %.y (%.n after |rein %urgit [%.n %urgit-ci])
.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)                          → %.y
.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/side')/noun)                            → %.n
.^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/(scot %t '2316f90dc5bb1589ba7b17329ed1f26886918d8a')/noun)  → %.y
.^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/(scot %t 'eb8e8a81a78204f3b4ef14ae1cb8bf6a34d7a567')/noun)  → %.n
.^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/side')/(scot %t '2316f90dc5bb1589ba7b17329ed1f26886918d8a')/noun)    → %.n
```

`%urgit` builds the same paths programmatically
(`/(scot %p our.bowl)/urgit-ci/(scot %da now.bowl)/eligible/(scot %t repo)/(scot %t ref)/(oid-text:git-codec oid)/noun`);
the receiver decodes `(scot %t …)` segments and takes raw ones as-is. A raw
40-hex OID or a dotted name typed into the dojo is a path syntax error, which
is why the typed forms wrap those in `(scot %t …)`.

## Shutdown

Pier processes found by `/proc/<pid>/cmdline` containing
`urgit-ci-p0-ryx`: `804972` (`urbit -F ryx … -c /var/home/michael/piers/urgit-ci-p0-ryx`)
and `805998` (`urbit work --snap-dir /var/home/michael/piers/urgit-ci-p0-ryx …`).
`ctrl+d` sent to the dojo in pane `wN:p2`; both `/proc` entries gone
afterwards; the pane returned to the shell prompt (`took 59m2s`) and was
closed by id. The pier directory is retained on disk. No other pier was
touched.

## Deviations and notes for the record

- Brief re-frozen at `161566d` (Q1: `%ci-action` case on `%urgit`, Q2: `signed-request` return with the SigV4 lifetime, Q3: `group-peek`'s guard and refuse-every-ref outage). Code follows the rulings.
- Eyre reads `Authorization: Bearer` as its own session token (`401 bad session auth` before any agent runs), so the daemon bearer travels in `x-ci-bearer`. Everything else about D6 auth is as written: bearer derived from daemon-id and token, returned once, stored only as a hash, compared as hashes.
- `+urgit-ci!mint-enroll-token` cannot resolve (there is no desk `%urgit-ci`); the generator is `desk/gen/urgit-ci/mint-enroll-token.hoon`, invoked as `:urgit-ci|mint-enroll-token`, which prints the token once and pokes its hash in.
- `|suspend`/`|revive` take a desk; H14 stops the single agent with `|rein %urgit [%.n %urgit-ci]` and restarts it with `[%.y %urgit-ci]`.
- `desk/mar/yml.hoon` (text mark) exists only so `desk/tests/ci/*.yml` can be committed to Clay.
- `desk/lib/ci-candidate.hoon` holds the pure merge-base and fast-forward/merge decision so `materialize-candidate` in `%urgit` stays one short arm that calls `merge-commit:git-tree`.
- `read-settings:ci-storage` re-implements `storage-settings` (an arm inside `%urgit`'s agent core cannot be imported) behind a `%gu` liveness read of `%storage`.
- `%assign` takes an optional deadline (`(unit @dr)`, default `~h1`) so H10 can be run; `%materialize` is the explicit P0 trigger for the `%urgit-ci` → `%urgit` poke direction.
- `upload-pack` serves only objects reachable from an advertised ref, so H5 exposes the merge candidate on `refs/ci/candidate` with the existing `%set-ref` poke to read it from a clone. That ref is harness scaffolding, not part of the contract.
- The first H5 run based clone-b on the current tip; the ship judged it a fast-forward (correctly). The row's divergent case needs a head that does not descend from the tip (based on ONE, `--force`).
- The candidate id printed by the push is `(sham [repo ref head base])` computed on both sides; the poke is fire-and-forget, as D4 says.
- Docker on the runner host: the host's default Docker socket may be rootful (`/var/run/docker.sock` owned by root, `docker info` showing no `name=rootless` security option), while the orchestrator's battery ran `act` under a rootless daemon (`docker info` → `name=rootless`). Which socket the harness uses is R3.1-A hygiene for the harness host only; in production the VM wrapper (spec § Execution: one disposable VM per job, the Docker socket belongs to the VM, never to the host) is what makes either acceptable.

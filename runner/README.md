# Native CI for `%urgit`: the operator's setup guide

Your ship runs the CI. It stages every push to a protected branch as a candidate, plans the candidate's GitHub Actions workflows, hands each job to a runner daemon you install on a Linux host, keeps every log in your own object store, and advances the branch only when every job passes. Nothing here talks to GitHub.

This guide is the whole setup, in order. Every step is either a click in the ship's web interface or a command on the runner host. There is no dojo step.

1. [Object store](#1-object-store)
2. [Mint a token](#2-mint-a-token)
3. [Install the daemon](#3-install-the-daemon)
4. [Protect a branch](#4-protect-a-branch)
5. [Watch](#5-watch)
6. [Runners](#6-runners)
7. [Limitations in this release](#7-limitations-in-this-release)

## 1. Object store

CI keeps logs and artifacts in an S3-compatible object store. It reads the endpoint, bucket, region and credentials from the ship's `%storage` agent — the same settings Landscape uses for uploads and `%urgit` uses for Git LFS. Any store that accepts Signature Version 4 requests works: a bucket at a cloud provider, or a store you run yourself such as RustFS or MinIO. There is no separate CI setting. Until `%storage` is configured, the **CI required** toggle refuses with `ship object storage is not configured; CI cannot be enabled`.

**The endpoint must be reachable from every browser that will view logs, not only from the ship's host.** The ship signs a short-lived link into the store for each log, and the viewer's browser follows that link directly. A `localhost` or LAN-only endpoint works for the runner and breaks the log view for everyone else, and the symptom is `Log unavailable: NetworkError` (or `Failed to fetch`) when a log is opened. The CI tab shows the store's host with a pip: green when the browser you are using can reach it, red with the reason when it cannot.

Two things the store must allow, whichever you use:

- **Reachability from viewers.** Name the store by an address your viewers' browsers resolve and reach: a public hostname, or the LAN address of the host that runs it if every viewer is on that LAN. `127.0.0.1` is only ever right when the browser runs on the store's own host.
- **Cross-origin reads (CORS).** The web interface is served by the ship and the log by the store, so the bucket must answer CORS for `GET` and `HEAD` from the ship's origin. Without a rule the browser blocks the read and the symptom is the same `NetworkError`; the CI tab's pip then reads red with *the store refuses cross-origin reads from this page*. On RustFS and MinIO this is one bucket CORS rule (`AllowedOrigin` the ship's origin or `*`, `AllowedMethod` `GET` and `HEAD`).

On a NativePlanet box, GroundSeg fills the `%storage` settings in for you when you enable its object storage; check that the endpoint it names is one your browser reaches, not the box's own loopback.

## 2. Mint a token

In the ship's web interface open the repository, then **Settings → Runners → Mint token**.

The dialog shows the enrollment token once, beside the three config lines the daemon needs:

```toml
ship_url = "http://your-ship.example:8080"
enroll_token = "0v…"
sandbox = "docker-rootless"
```

Copy them now. The ship keeps only a hash of the token; closing the dialog is the last time it is readable. If you lose it, **Expire** the row and mint another. A minted token that no daemon has used yet shows as `minted` in the Runners table.

The dialog also offers **Repositories this runner may take**. Leave it at *any* for a runner that takes every job (the pool), or pick repositories to make this runner run only their code. You can change this later from the table.

## 3. Install the daemon

The daemon is one static Go binary. Build it on any machine with Go 1.22 or later:

```text
cd runner && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner
```

(`zig build -Drunner` from the repository root does the same when Go is on the PATH.)

On the runner host you need:

- Any Linux host with outbound HTTP to the ship. It may be the ship's own host or another machine.
- A rootless Docker daemon owned by the user the runner runs as. Check with `docker --host unix://<socket> info --format '{{.SecurityOptions}}'`; the output must contain `name=rootless`.
- The runner image `catthehacker/ubuntu:act-latest` loaded into that rootless daemon.
- A statically linked `act` 0.2.89 (the release tarball from `nektos/act`; a Homebrew `act` links against Homebrew's libc and cannot run inside the container).

Then, on the runner host:

```text
install -m 0755 urgit-runner /usr/local/bin/urgit-runner
install -m 0600 urgit-runner.toml /etc/urgit-runner.toml
systemctl enable --now urgit-runner
```

`urgit-runner.toml` starts from `urgit-runner.toml.example` with the three lines the dialog gave you pasted in. The other keys:

| Key | Meaning |
|---|---|
| `ship_url` | The ship's HTTP origin, from the dialog. |
| `enroll_token` | From the dialog. Consumed on the first start; remove it from the file afterwards. |
| `sandbox` | `docker-rootless`, the one backend in this release. |
| `docker_host` | The rootless daemon's socket, as `unix:///run/user/<uid>/docker.sock`. |
| `act_binary` | Path to the static `act` 0.2.89 the daemon copies into each sandbox. |
| `act_image` | The image `act` maps `ubuntu-latest` to. |
| `capacity` | Concurrent jobs this daemon offers the ship. Default 1. Changing it takes effect at the first poll after a restart. |
| `labels` | The labels this runner declares, matched against a job's `runs-on` (see [Runners](#6-runners)). Default none. |
| `work_dir` | Checkouts, projections and saved `act` streams, one directory per attempt. |
| `state_file` | Where the daemon writes its identity, bearer and the ship's CI public key after enrolling. Mode 0600. |

`urgit-runner.service` is a `Type=simple` unit with `Restart=on-failure`, running as the unprivileged user `urgit-runner`; create that user and give it the rootless Docker daemon.

On its first start the daemon enrolls with the token, writes its identity to `state_file`, and forgets the token. Its row in the Runners table flips from `minted` to `healthy` as it does — with no page refresh. A later start with the state file present polls at once and never re-enrolls.

## 4. Protect a branch

Open the repository's **Settings → Protected branches** and turn on **CI required** for the branch.

From then on a push or a web merge to that branch is never applied directly. The ship stages the pushed head as a candidate — the push answers `staged as ci candidate <id>` — plans its workflows, runs every job, and advances the branch to the candidate's exact commit only when every job passes. A candidate whose branch moved meanwhile is refused with `destination moved; rebase and push again`. A revision from a ship that cannot write the repository (a fork pull request) is untrusted: it waits for a writer's approval on the CI tab, or runs as a restricted check with no credentials if you choose that policy in Settings → Untrusted revisions.

The toggle refuses a branch with no commit yet (`ref has no tip; push a commit before CI-protecting it`) and any repository while `%storage` is unset. A repository bound to a Clay desk may be CI-protected: its candidate lands through the desk — the candidate's files are written to the desk first, exactly as a push to the linked branch is (so the repository must be desk-shaped: `sys.kelvin`, a mark for every file it carries, and the marks those marks build on), and the branch advances only once the desk took them and the checks still hold. When you turn it on, the CI tab's storage check runs; a red result is a warning under the toggle, not a refusal — the runner may still reach the store even when your browser does not.

## 5. Watch

The repository's **CI** tab lists candidates newest first and updates as the ship works: staged, planned, each job's attempt as it starts and finishes, the verdict, the landing. The pip beside the store's host reads `live` while the tab is subscribed to the ship; if the connection drops it reads `polling`, the list re-reads every ten seconds, and **Refresh** reads it now.

Open a candidate for its jobs: each row shows the job, its `runs-on`, the runner that ran it, timing, and a **log** link once the log is in the store. **Approve and run trusted** appears on an untrusted candidate (the tooltip names the repository's policy); **Re-run** stages the same head again.

A repository with CI required and no runner shows *No runner is enrolled. Mint a token in Settings → Runners and install the daemon* with a link to this guide.

## 6. Runners

**Settings → Runners** lists every daemon record: id, capacity, sandbox, labels, repositories, enrolled and last-seen ages, running jobs, and a state:

- `minted` — a token exists; no daemon has enrolled with it. **Expire** deletes it.
- `healthy` — seen within the last five minutes. The daemon polls even while it is busy at capacity, so this is liveness.
- `stale` — not seen for five minutes. The ship hands it nothing; any assignment it never fetched is offered to another runner. It reads `healthy` again at its next poll.
- `refused` — it abandoned an assignment because the assignment did not verify against the CI public key it pinned at enrollment (after **Rotate CI key**, or a wrong key in its config). It is offered no work until it re-enrolls; ordinary polls do not restore it. Recovery: stop it, delete its state file, mint a new token, start it. Then **Revoke** and **Remove** the old row. First check its **Retentions**: deleting the state file discards them (below).
- `revoked` — you revoked it. Its next poll was refused, it logged `revoked by the ship` and exited with status 5; every job it was running was offered to another runner. **Remove** deletes the row.

**Revoke** ends a runner's access at once: the bearer is cleared, its next poll answers 401, and a job it was running is re-offered. "Regenerate" is revoke (or expire) plus mint: two clicks.

**Rotate CI key** replaces the key the ship signs every assignment with. Every enrolled runner pinned the old key, so each one refuses its next assignment, shows as `refused`, and takes no work until you re-enroll it with a fresh token. Jobs already running finish.

**Labels.** A runner declares labels in its config, `labels = ["big-mem", "arm64"]`, and sends them on every poll. A job runs on a runner only when every label in its `runs-on` is one the runner declares or one of the implicit set every runner stands for: `self-hosted`, `linux`, `ubuntu-latest`, `ubuntu-22.04`, `ubuntu-24.04`, `x64`. So `runs-on: ubuntu-latest` runs anywhere, and `runs-on: [self-hosted, big-mem]` runs only on a runner that declared `big-mem`. A job no runner can take waits, and the candidate row says why: `no runner has labels [big-mem]`; it runs as soon as such a runner enrolls. `runs-on` is matched as literal labels; an expression there is refused at plan time.

**Repository binding.** The table's **Repositories** column is set on the ship — in the table, or in the mint dialog — never in the daemon's config: the machine does not get to declare which code it may run. *Any* is the pool. *Only …* restricts the runner to the named repositories; the ship never hands it a job of any other. The binding is ship state and survives the daemon's restart. This is how you keep one runner that only ever executes trusted code from named repositories while another takes everything, including fork pull requests run as restricted checks.

**When a runner dies mid-job.** A job whose runner gives up is offered to another runner at once; if no other runner exists the candidate fails with the runner's reason. A job whose runner goes silent — no result by the job's `timeout-minutes` plus two minutes (an hour when the job declares none) — is offered again once to another runner, and fails as `runner went silent` if that runner goes silent too. A late result from the first runner is refused as `attempt is closed`.

**An assignment delivered again.** The ship may hand an assignment over again while its job runs or waits here: after a lost reply, or as a copy, however its attempt id is written. The runner ignores it and says nothing about it to the ship, so the job is never offered to another runner while it runs here. The runner names every attempt as the ship writes it.

**An assignment runs at most once on a runner.** The runner records each attempt it takes in its execution ledger, `executions/` beside the state file, before anything of the job runs, and again when the job ends. A copy of a finished job's assignment is ignored. A copy of a job that a stop cut short is given back to the ship, which offers the job again as a new attempt, or closes it. If the ledger cannot be written (a full disk, a permissions change), new assignments are given back, not started, and nothing is deleted to make room. The ledger grows by two small files per attempt and is kept: nothing prunes it, and nothing in it may be deleted. The state file names it, and a start that finds the ledger gone, or missing `taken/` or `finished/`, is refused. Back it up, and restore it, together with the state file.

**Upgrading from a runner without the ledger.** What the earlier version ran left no record, so the upgraded runner runs nothing until you confirm its transition. At its first start it logs `EXECUTION PAUSED`, polls advertising no capacity, and gives back every assignment it receives. It still reports its retentions and carries out their release. In **Settings → Runners** it is marked *execution paused*: open **Transition…**, inspect its history and the proof, and confirm. The runner checks the transition, records it, and from then on refuses every assignment the ship signed before it, and runs new work. Nothing is released, reset or re-enrolled, and there is no drain or wait. A runner whose `executions/HISTORY` cannot be read waits the same way. So does one whose state file's `transition` is damaged, not a record this version wrote: it keeps the record as it is, says why in the panel, and waits for its transition to be confirmed again. A state file that names a field twice, which no runner writes, refuses the start and is left as it is. The ship must run this version too (QUESTIONS-SOURCE-01 §15; [`launcher/INTEGRATION.md`](launcher/INTEGRATION.md) §11.15).

**Quarantined slots.** When a sandbox's release cannot be proven — a VM the launcher could not stop or clean up in time, a container whose removal failed — the daemon withholds that slot (it offers the ship one fewer), records it in its state file under the sandbox's exact identity and its job's name, and logs `QUARANTINED slot ci-<attempt> …`. Cleanup may still run automatically and stop or remove what is provably that sandbox's, but nothing releases the slot by itself — not even a cleanup that finally succeeds after its deadline — and a restart never charges it twice. Cleanup and release are separate steps, each yours:

1. **A VM: the launcher's incident first.** As root, stop the launcher's `serve`, then run `urgit-vm-launcher recover -config /etc/urgit-vm-launcher-p4-opus.toml`. It lists the incidents by job (repository · workflow · job), each with its exact reservation (id and incarnation token; cid, created), whether its cleanup deadline was missed, what it still holds and the capacity it withholds. Pick one by its number to see its inspection: every cleanup attempt, what was stopped or removed and what is unresolved, and why release is refused or allowed. `r` retries its cleanup; that never releases it. `R` releases it once nothing is held, after you type `release`. A VM whose pid was never recorded is looked for by the retry, and stopped if found; a release is never taken as proof that it is gone. Scripts use `-select <id>/<incarnation>/<cid>/<created>/<rev> -action inspect|retry|release`, with the same checks (the incarnation is `-` for a reservation written before incarnation tokens). Start `serve` again.
2. **The daemon's retention.** Stop the daemon (the launcher's `serve` running: the runner asks it), then run `urgit-runner -config /etc/urgit-runner.toml -recover`. It lists the retentions the same way. A VM's slot is released only on the launcher's evidence of that exact reservation's release. A reserve request whose outcome the daemon never learnt (listed as `reserve request <token>, its outcome not settled`) is released only on the launcher's settlement of exactly that request: allowed when the launcher never admitted it and has closed it for good, or proves the release of what it admitted; refused when it admitted a reservation — the output names it, and it follows its own disposition. An empty list is never enough. Its inspection only reads; a retry has nothing to clean up. An older retention that a daemon before settled admission recorded without its reserve request (a *legacy* retention, listed with `legacy: released from Urgit`) is never released by `-recover`: it is released from Urgit, by the running daemon (below). One that names only its handle, from before retentions named their backend, stays withheld (QUESTIONS-SOURCE-01 §11). A container's objects can be removed from here (`r`, by their exact names; it stays retained), and it is released only when Docker answers, at the moment you release it, that none of them exists — its exact not-found reply about each object; any other answer refuses the release. Scripts use `-recover -select <selection> -action inspect|retry|release` (the selection is shown by the inspection): exit status 0 done; 1 refused or unresolved (the output says why); 2 the daemon is running, or the state file could not be read or saved — run it again.
3. **Start the daemon**: the slot is offered again.

Listing and inspecting work while the daemon and the launcher run. A retry or a release takes the lock of the process it acts for, so that process is stopped first: the launcher's `serve` for `urgit-vm-launcher recover`, the daemon for `urgit-runner -recover`, whose release of a VM's slot asks the running launcher. A released incident is kept as evidence: the launcher keeps it under `released/` in its state directory, and the daemon keeps it in its state file's `released` list.

**Reserve requests whose answer was lost.** Before the daemon asks the launcher for a VM, it records the request in its state file; if it cannot, it sends none. If the answer never arrives, or the launcher refuses, the daemon has the launcher settle exactly that request before it offers the slot again. The launcher either names the VM it admitted — the daemon then destroys or holds it like any leftover — or closes the request for good, so that a delayed or replayed copy of it can never be admitted later. Until then the slot stays withheld and the daemon logs `ADMISSION NOT SETTLED`. Every reconcile asks again — at each start, before any job, and every 30 seconds while it runs — and the slot returns by itself once the request is settled; nothing is yours to do. The launcher keeps one small file per request under `requests/` in its state directory and never deletes them (QUESTIONS-SOURCE-01 §10).

**Legacy retentions, from Urgit.** A runner before settled admission could record a retention with neither the launcher's identity of its reservation nor its reserve request (its reserve's answer lost while the launcher could not be asked). Nothing can settle such an entry by its request. This release marks it *legacy* when it first loads that state file, and saves the file in its own format, so no entry it writes itself is ever marked. It is released from **Settings → Runners → Retentions**:
1. The running daemon reports every retention to the ship, with how its slot returns. For a legacy one it also reports its evidence: the launcher asked (its version and protocol), whether the launcher's inventory is authoritative, and what it holds of the attempt.
2. Select the runner's **Retentions** and inspect the legacy one. A release is offered only when every condition holds:
   - it is legacy;
   - it is a launcher reservation;
   - the launcher speaks protocol 4, which refuses every reserve that names no request token — the only kind this entry's request could be;
   - its inventory is authoritative;
   - it holds no record of the attempt, in any state;
   - the attempt is not running here.
3. **Release this retention…** restates the exact entry, revision and evidence. Tick the acknowledgment, then release. The ship records a command for exactly that entry, revision and evidence. It signs the command for this daemon and hands it over on the daemon's own poll, within about 25 seconds.
4. The daemon checks everything again when the command runs. It refuses if anything changed, or if the command expired (fifteen minutes). Otherwise it saves the release in its state file, with the command and the evidence. The slot returns only once that save is durable.

The panel shows the command as one of:
- *queued*;
- *pending*;
- *released*;
- *refused*, with the reason: the slot stays withheld;
- *uncertain*: no final answer is recorded yet. A save's outcome is unknown, a refusal is not recorded yet, or the daemon cannot authenticate the command. The slot stays withheld, the daemon takes no new work while its state file is not durable, and the ship hands the command over again;
- *expired*.

The daemon records every release and every refusal in its state file before it answers. A lost reply, a command handed over again, however its id is written, or a daemon restart is answered from that record. Nothing is released twice, and a refused command is never carried out later. To try again after a refusal, confirm the release again: that is a new command.

Nothing at the launcher is touched. A record of the attempt there refuses the release; that reservation follows its own disposition. A daemon whose every slot is withheld keeps running while one of them is a legacy retention, so the command can reach it.

**Re-enrolling a runner that withholds slots.** Re-enrolling today means deleting the state file, which discards every retention it holds — real quarantines included — and returns their slots without proof. Do not delete the state file of a runner whose **Retentions** lists anything.

**Upgrading the ship.** An upgrade of `%urgit-ci` keeps its state, and with it every runner's enrollment and bearer (contract §8c; state-migration ruling 01). A runner goes on polling, and needs no re-enrollment. Its state file, with its retentions and recorded refusals, is never touched by the ship's upgrade. Do not nuke `%urgit-ci` to upgrade it: a nuke forgets every runner. A saved state of a shape the agent does not know is refused, not reset. No upgrade of an agent on a ship has been qualified yet.

**Late bookkeeping.** A sandbox whose cleanup finished before its deadline is released once the launcher has kept its evidence and durably withdrawn its record — even if that withdrawal is confirmed after the deadline, which is then said apart: serve logs `RELEASED, ACCOUNTING CONFIRMED LATE`, the evidence under `released/` records it, and `urgit-vm-launcher list` reports every such release. Until the withdrawal is confirmed the slot stays charged. The launcher keeps that evidence only in a real `released/` directory in its state directory, and certifies the directory's link at every release: if the state directory's fsync fails, or `released/` is not a directory, every release stays charged until it is repaired, and `urgit-vm-launcher list` reports a `released/` that is not a directory as unreadable evidence. `urgit-runner -recover` releases a VM's slot only on the launcher's evidence of that exact reservation's release: a reservation the launcher no longer holds, gone without evidence, is never taken for released.

**The launcher's unaccounted entries.** If the launcher's `serve` logs `UNSAFE STATE` — an entry in its records directory it cannot read or does not recognize — it keeps enforcing the reservations it could read, but admits nothing and treats no absence as a release until you resolve the entry by hand, with `serve` stopped, and start it again. Until then, `recover` shows no incident as released, `urgit-runner -recover` refuses a VM's release, and the daemon frees no slot. A daemon that starts meanwhile logs `the sandbox backend's records could not be reconciled` and exits with status 2. No enrollment was lost, so do not re-enroll. The entry is never deleted for you.

**State file not writable.** If the daemon cannot save a quarantine to its state file (a full disk, a permissions change), it logs `STATE NOT DURABLE`, keeps the slot withheld, keeps polling and finishing the work it runs, and answers every new assignment `runner state not durable`, so the ship offers it elsewhere, until a save succeeds.

## 7. Limitations in this release

- `microvm` (Firecracker + jailer) is the default backend and the one every privileged, trial, shadow and untrusted job needs; it requires the administrator-owned `urgit-vm-launcher` and a digest-verified guest image, and refuses to start without them — it never falls back to a container. `docker-rootless` remains for trusted compatibility work on a repository that opted in explicitly (`set-sandbox-requirement container`): its sandboxes are containers, not a VM boundary, and a network profile there is NAT egress as a whole rather than the launcher's per-destination enforcement.
- Execution is locked by default: no egress at all unless the repository's policy authorizes a profile for that job and the runner declares it.
- The VM path's image, kernel, guest helper and protocol are built from the recipes under [`guest`](guest) and proven by unjailed preflight boots; the jailed path — the launcher, its cgroups and its network policy — waits on the administrator's review of the launcher recipe and is not claimed qualified here.
- The promoted baseline's workflow and harness bytes are the required evidence, not the candidate's own. A candidate whose harness differs runs a trial beside the required run — unprivileged and never landable — and a branch with no promoted baseline waits with that reason.
- The web interface's **Approve** button is the owner's. A listed writer on another ship approves through the peer protocol from their own ship (`POST /peer/ci-approve` with the owner ship, the repository and the candidate id; the answer arrives as a forge request of kind `candidate`); the owner's ship admits exactly the ships that can write the repository. No button for it yet.
- Retrying or releasing a quarantined slot needs the daemon stopped, and for a VM the launcher's `serve` stopped for its own retry and release first (§6), which tears down every VM that launcher runs: there is no live administration channel. The one exception is a legacy retention, released from Urgit through the daemon's own poll (§6). No host listener is added for it.
- The checkout clones anonymously. A private repository refuses the clone and the plan fails with the clone's error.
- A credential value is at least eight characters and may span lines (a PEM key pastes as is). `act` masks a secret only where the whole value appears on one output line, so the runner and the ship scrub every released value whole and every line of it of at least eight characters from the relayed stream, the saved log and the recorded outputs.
- The CI public key is pinned at enrollment. A rotated key needs a fresh enrollment (or `ci_public_key` in the config, for testing only).
- A matrix strategy, a `runs-on` expression, and any job-level `if` other than `needs.<job>.outputs.<name> == '<literal>'` fail the plan with a diagnosed reason.
- The saved `act` stream is uploaded as `log.jsonl`. Step summaries and artifacts stay in the sandbox and are not uploaded.
- The store's `public-url-base` setting is not read: the endpoint `%storage` names is the one both the runner and every browser must reach.

## How it works

The daemon enrolls once, then long-polls the ship for assignments; the poll is also its heartbeat. A plan assignment means: check out the candidate, run `act -l` on every workflow file, and post the job list with each job's `needs`, compiled `if`, `environment`, `runs-on` and `timeout-minutes`. A job assignment means: check out the candidate, write a single-job projection of its workflow, and run `act push -j <job> --json` on it inside a fresh sandbox. The daemon relays every event line and claims the `jobResult` the stream carried; when `act` exits without one, it abandons the attempt with the reason. It never infers success.

Every assignment carries the ship's signature over the recipient, the attempt, the operation, an expiry and a nonce; the daemon verifies it with the CI public key it pinned at enrollment and refuses an unsigned or bad-signed assignment before any work. A trusted job's assignment also carries grants: the credentials the ship released to that attempt, each signed and bounded by the attempt's deadline, each passed to `act` as a secret.

After `act` exits the daemon uploads the saved stream to the object store under a ship-signed PUT, then posts the result naming the object's size and hash. A viewer reads the log through the ship, which answers a presigned link into the attempt's own trust class.

Every attempt gets its own Docker network, work volume and runner container on the rootless daemon. The checkout enters the container by copy, never by bind mount; the rootless socket is the only mount, and `act` is told to bind that same socket into each job container, so the job's Docker is the rootless daemon by construction. The daemon destroys the sandbox after every attempt; a failed teardown quarantines that slot and lowers the advertised capacity. After a restart the daemon asks the ship about each leftover sandbox and destroys every one whose attempt is closed or unknown; it never resumes a job.

`act -j <job>` would run the job's whole prerequisite chain and evaluate job-level `if` itself; the ship owns admission, so the daemon runs `act` on a projection of the workflow holding only the assigned job, with its `needs` and job-level `if` removed and the workflow's `name:` prefixed by the attempt id (so two attempts on one job never share a container name). The ship refuses any relayed event whose job is not the assigned one. The prerequisites' recorded outputs reach the job as `NEEDS_<JOB>_OUTPUTS_<NAME>` environment variables.

The daemon prints its sandbox disclosure at startup. A compatibility daemon prints `sandbox: docker-rootless (container compatibility mode; not a VM boundary; a network profile is NAT egress as a whole, destinations not narrowed)`; a VM daemon prints `sandbox: microvm (Firecracker + jailer; one disposable VM per attempt via urgit-vm-launcher; image <digest>)` followed by its guest and budget numbers.

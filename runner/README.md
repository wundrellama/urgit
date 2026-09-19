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

The toggle refuses a branch with no commit yet (`ref has no tip; push a commit before CI-protecting it`), a repository bound to a Clay desk, and any repository while `%storage` is unset. When you turn it on, the CI tab's storage check runs; a red result is a warning under the toggle, not a refusal — the runner may still reach the store even when your browser does not.

## 5. Watch

The repository's **CI** tab lists candidates newest first and updates as the ship works: staged, planned, each job's attempt as it starts and finishes, the verdict, the landing. The pip beside the store's host reads `live` while the tab is subscribed to the ship; if the connection drops it reads `polling`, the list re-reads every ten seconds, and **Refresh** reads it now.

Open a candidate for its jobs: each row shows the job, its `runs-on`, the runner that ran it, timing, and a **log** link once the log is in the store. **Approve and run trusted** appears on an untrusted candidate (the tooltip names the repository's policy); **Re-run** stages the same head again.

A repository with CI required and no runner shows *No runner is enrolled. Mint a token in Settings → Runners and install the daemon* with a link to this guide.

## 6. Runners

**Settings → Runners** lists every daemon record: id, capacity, sandbox, labels, repositories, enrolled and last-seen ages, running jobs, and a state:

- `minted` — a token exists; no daemon has enrolled with it. **Expire** deletes it.
- `healthy` — seen within the last five minutes. The daemon polls even while it is busy at capacity, so this is liveness.
- `stale` — not seen for five minutes. The ship hands it nothing; any assignment it never fetched is offered to another runner. It reads `healthy` again at its next poll.
- `refused` — it abandoned an assignment because the assignment did not verify against the CI public key it pinned at enrollment (after **Rotate CI key**, or a wrong key in its config). It is offered no work until it re-enrolls; ordinary polls do not restore it. Recovery: stop it, delete its state file, mint a new token, start it. Then **Revoke** and **Remove** the old row.
- `revoked` — you revoked it. Its next poll was refused, it logged `revoked by the ship` and exited with status 5; every job it was running was offered to another runner. **Remove** deletes the row.

**Revoke** ends a runner's access at once: the bearer is cleared, its next poll answers 401, and a job it was running is re-offered. "Regenerate" is revoke (or expire) plus mint: two clicks.

**Rotate CI key** replaces the key the ship signs every assignment with. Every enrolled runner pinned the old key, so each one refuses its next assignment, shows as `refused`, and takes no work until you re-enroll it with a fresh token. Jobs already running finish.

**Labels.** A runner declares labels in its config, `labels = ["big-mem", "arm64"]`, and sends them on every poll. A job runs on a runner only when every label in its `runs-on` is one the runner declares or one of the implicit set every runner stands for: `self-hosted`, `linux`, `ubuntu-latest`, `ubuntu-22.04`, `ubuntu-24.04`, `x64`. So `runs-on: ubuntu-latest` runs anywhere, and `runs-on: [self-hosted, big-mem]` runs only on a runner that declared `big-mem`. A job no runner can take waits, and the candidate row says why: `no runner has labels [big-mem]`; it runs as soon as such a runner enrolls. `runs-on` is matched as literal labels; an expression there is refused at plan time.

**Repository binding.** The table's **Repositories** column is set on the ship — in the table, or in the mint dialog — never in the daemon's config: the machine does not get to declare which code it may run. *Any* is the pool. *Only …* restricts the runner to the named repositories; the ship never hands it a job of any other. The binding is ship state and survives the daemon's restart. This is how you keep one runner that only ever executes trusted code from named repositories while another takes everything, including fork pull requests run as restricted checks.

**When a runner dies mid-job.** A job whose runner gives up is offered to another runner at once; if no other runner exists the candidate fails with the runner's reason. A job whose runner goes silent — no result by the job's `timeout-minutes` plus two minutes (an hour when the job declares none) — is offered again once to another runner, and fails as `runner went silent` if that runner goes silent too. A late result from the first runner is refused as `attempt is closed`.

## 7. Limitations in this release

- Sandboxes are containers on a rootless Docker daemon, not virtual machines. The `microvm` backend is the next phase's first item on the same interface. Egress from a sandbox is unrestricted NAT until then.
- The candidate's own workflow files are the required evidence. The ship records the commit it read them from, so a later release can pin an approved revision.
- Only this ship's owner can approve an untrusted candidate in this release; a listed writer on another ship cannot yet.
- The checkout clones anonymously. A private repository refuses the clone and the plan fails with the clone's error.
- A credential value must be a single line of at least eight characters; multi-line material (a PEM key) is stored base64-encoded, because `act` masks a secret only where the whole value appears on one output line. Every released value is scrubbed whole from the relayed stream and the saved log.
- The CI public key is pinned at enrollment. A rotated key needs a fresh enrollment (or `ci_public_key` in the config, for testing only).
- A repository bound to a Clay desk cannot be CI-protected; binding a CI-protected repository to a desk afterwards is not blocked, and such a candidate is refused at landing.
- A matrix strategy, a `runs-on` expression, and any job-level `if` other than `needs.<job>.outputs.<name> == '<literal>'` fail the plan with a diagnosed reason.
- The saved `act` stream is uploaded as `log.jsonl`. Step summaries and artifacts stay in the sandbox and are not uploaded.
- The store's `public-url-base` setting is not read: the endpoint `%storage` names is the one both the runner and every browser must reach.

## How it works

The daemon enrolls once, then long-polls the ship for assignments; the poll is also its heartbeat. A plan assignment means: check out the candidate, run `act -l` on every workflow file, and post the job list with each job's `needs`, compiled `if`, `environment`, `runs-on` and `timeout-minutes`. A job assignment means: check out the candidate, write a single-job projection of its workflow, and run `act push -j <job> --json` on it inside a fresh sandbox. The daemon relays every event line and claims the `jobResult` the stream carried; when `act` exits without one, it abandons the attempt with the reason. It never infers success.

Every assignment carries the ship's signature over the recipient, the attempt, the operation, an expiry and a nonce; the daemon verifies it with the CI public key it pinned at enrollment and refuses an unsigned or bad-signed assignment before any work. A trusted job's assignment also carries grants: the credentials the ship released to that attempt, each signed and bounded by the attempt's deadline, each passed to `act` as a secret.

After `act` exits the daemon uploads the saved stream to the object store under a ship-signed PUT, then posts the result naming the object's size and hash. A viewer reads the log through the ship, which answers a presigned link into the attempt's own trust class.

Every attempt gets its own Docker network, work volume and runner container on the rootless daemon. The checkout enters the container by copy, never by bind mount; the rootless socket is the only mount, and `act` is told to bind that same socket into each job container, so the job's Docker is the rootless daemon by construction. The daemon destroys the sandbox after every attempt; a failed teardown quarantines that slot and lowers the advertised capacity. After a restart the daemon asks the ship about each leftover sandbox and destroys every one whose attempt is closed or unknown; it never resumes a job.

`act -j <job>` would run the job's whole prerequisite chain and evaluate job-level `if` itself; the ship owns admission, so the daemon runs `act` on a projection of the workflow holding only the assigned job, with its `needs` and job-level `if` removed and the workflow's `name:` prefixed by the attempt id (so two attempts on one job never share a container name). The ship refuses any relayed event whose job is not the assigned one. The prerequisites' recorded outputs reach the job as `NEEDS_<JOB>_OUTPUTS_<NAME>` environment variables.

The daemon prints its sandbox disclosure at startup: `sandbox: docker-rootless (container isolation; microvm backend pending)`.

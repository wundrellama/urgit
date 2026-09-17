# `urgit-runner`

The runner daemon for `%urgit-ci`. A ship running `%urgit` schedules the jobs of a CI-protected push. This daemon runs each job under a pinned `act` inside a disposable sandbox and relays the result. The ship decides everything. The daemon carries out one assignment at a time per slot.

## What it does

The daemon enrolls once, then long-polls the ship for assignments. A plan assignment means: check out the candidate, run `act -l` on every workflow file, and post the job list. The list carries each job's `needs`, compiled `if` and `environment` name. A job assignment means: check out the candidate, write a single-job projection of its workflow, and run `act push -j <job> --json` on it. The daemon relays every event line and claims the `jobResult` the stream carried. When `act` exits without a `jobResult`, the daemon abandons the attempt with the reason. It never infers success.

After `act` exits, the daemon uploads the saved stream to the ship's object store before it posts the result. It asks the ship for a signed PUT of `log.jsonl`, sends the bytes with the ship's headers, and names the object's size and sha-256 in the result. The ship records the handle. A viewer reads the log through the ship, which answers a presigned link into the attempt's own trust class.

Every assignment carries the ship's signature over the recipient, the attempt, the operation, an expiry and a nonce. The daemon verifies it with the CI public key it pinned at enrollment and refuses an unsigned or bad-signed assignment before any work. The refusal is logged and the attempt is abandoned with the reason. A trusted job's assignment also carries grants: the credentials the ship released to that attempt, each signed and bounded by the attempt's deadline. Each grant becomes an `act --secret`. A grant past its expiry, or one whose signature does not verify, is never passed to `act`. The daemon scrubs every released value from the relayed lines and from the saved stream, and its own log shows `--secret NAME=***`.

Sandbox disclosure: `sandbox: docker-rootless (container isolation; microvm backend pending)`. The daemon prints this line at startup.

## Prerequisites

- A ship with `%urgit` installed and `%storage` configured. `%urgit-ci` refuses to CI-protect any ref until `%storage` names an S3-compatible endpoint with a bucket and credentials.
- Any Linux host. The daemon may share the ship's host or run elsewhere. It needs outbound HTTP to the ship.
- A rootless Docker daemon owned by the user the daemon runs as. Check with `docker --host unix://<socket> info --format '{{.SecurityOptions}}'`. The output must contain `name=rootless`.
- The runner image, `catthehacker/ubuntu:act-latest`, loaded into that rootless daemon.
- A statically linked `act` 0.2.89 binary. The daemon copies it into every sandbox. The Homebrew `act` links against Homebrew's libc and cannot run inside the container. Use the release tarball from `nektos/act`.
- Go 1.22 or later to build. The result is one static binary.

## Install in three commands

Build first, on any machine with Go:

```text
cd runner && CGO_ENABLED=0 go build -o urgit-runner ./cmd/urgit-runner
```

`zig build -Drunner` from the repository root does the same when Go is on the PATH. It says so when Go is absent.

Then on the runner host:

```text
install -m 0755 urgit-runner /usr/local/bin/urgit-runner
install -m 0600 urgit-runner.toml /etc/urgit-runner.toml
systemctl enable --now urgit-runner
```

`urgit-runner.service` is a `Type=simple` unit with `Restart=on-failure`. It runs as the unprivileged user `urgit-runner`. Create that user and give it the rootless Docker daemon. `EnvironmentFile=/etc/default/urgit-runner` is optional.

## Configuration

Copy `urgit-runner.toml.example` and set these keys. The spec names the first four.

| Key | Meaning |
|---|---|
| `ship_url` | The ship's HTTP origin. The daemon uses `/apps/urgit/api/ci` and `/git/<repo>`. |
| `enroll_token` | Minted on the ship with `:urgit-ci|mint-enroll-token`. Pasted once. |
| `sandbox` | `docker-rootless`, the one backend in this release. |
| `docker_host` | The rootless daemon's socket, as `unix:///run/user/<uid>/docker.sock`. |
| `act_binary` | Path to the static `act` 0.2.89 the daemon copies into each sandbox. |
| `act_image` | The image `act` maps `ubuntu-latest` to. |
| `capacity` | Concurrent attempts this daemon offers the ship. Default 1. |
| `work_dir` | Checkouts, projections and saved `act` streams, one directory per attempt. |
| `state_file` | Where `daemon_id`, the bearer and the pinned CI public key land after enrollment. Written with mode 0600. |
| `ci_public_key` | Optional. The CI public key to verify assignments and grants with, as hex. It overrides the key enrollment recorded. |

The `microvm` keys `image_path`, `cpus`, `memory_mib` and `disk_mib` parse into the sandbox specification. The `microvm` backend refuses to start in this release.

## Enrollment and the token

Mint a token on the ship:

```text
:urgit-ci|mint-enroll-token
```

Put it in the config file as `enroll_token`. On the first start the daemon enrolls with it, writes `daemon_id` and the bearer to `state_file`, and forgets the token. The daemon never writes the token to the state file or the log. Remove it from the config file after the first start. A later start with the state file present polls at once and never re-enrolls.

The daemon reports its capacity at enrollment and again on every poll. A change to `capacity` in the config file reaches the ship at the first poll after a restart. The daemon keeps its identity across restarts.

When the ship no longer knows the bearer, the daemon logs `enrollment lost; re-enroll with a fresh token` and exits with status 3. Mint a new token, delete the state file, and start again.

## The sandbox

Every attempt gets its own Docker network, work volume and runner container on the rootless daemon. The checkout enters the container by copy, never by bind mount. `act` runs inside that container with `--network` set to the attempt's own bridge. The rootless socket is the only mount, because `act` needs a Docker API to create job containers. That socket belongs to the rootless daemon, never to the host's rootful daemon. The daemon also passes `--container-daemon-socket` with the configured rootless socket on every `act` invocation, so the socket `act` binds into each job container is the rootless daemon's by construction. Stock `act` would bind `/var/run/docker.sock`, the host's rootful socket, and rely on its permissions to refuse the job.

The network is a user-defined bridge with NAT egress. Jobs reach the internet, because ERPit's workflows fetch `actions/cache@v4` and the urbit toolchain. The sandbox cannot reach the daemon's state file or config, the host's rootful Docker socket, or another attempt's network. Egress allow-listing arrives with the VM backend in P2.

The daemon destroys the sandbox after every attempt, whatever the outcome. When teardown fails, the daemon quarantines that slot: it lowers its capacity by one, logs the handle, and never reuses it. At capacity 0 it stops polling and exits with status 4. After a restart the daemon lists its leftover sandboxes, asks the ship about each attempt, and destroys every sandbox whose attempt is closed or unknown. It never resumes a job. The ship's deadline closes a job the daemon was running when it died.

## Single-job projection

`act -j <job>` runs the job's whole prerequisite chain and evaluates job-level `if` itself. The ship owns job admission. So the daemon writes a projection of the workflow with only the assigned job, minus its `needs` and job-level `if` keys. It runs `act` on that file. Every other line stays byte-identical. The ship refuses any relayed event whose job is not the assigned one, which proves the projection held.

The projection also rewrites the workflow's top-level `name:` to `<attempt id>/<original name>`. `act` names a job's container and volumes from a hash of the workflow name and the job name. It force-removes an existing container of that name. Two attempts on the same job on one Docker daemon would otherwise collide. Inside the job, `GITHUB_WORKFLOW` and `${{ github.workflow }}` read the prefixed form. No ERPit step reads them. The plan records the real workflow name, and each attempt records the name `act` ran under as `projection-name`.

The assignment carries the recorded outputs of the job's prerequisites as `prereq-outputs`. The daemon passes them to `act` as `--env NEEDS_<JOB>_OUTPUTS_<NAME>=<value>`. No ERPit step reads one in this release.

## Limitations in this release

- Sandboxes are containers on a rootless daemon, not virtual machines. The `microvm` backend is P2's first item on the same interface.
- Egress from a sandbox is unrestricted NAT. Allow-listing arrives with the VM backend.
- The candidate's own workflow files are the required evidence (CI-BASELINE-P1). The ship stores the OID it read them from as `plan-oid`, so a later baseline can pin an approved revision.
- A candidate is trusted when its actor can write the repository, and untrusted otherwise. An untrusted candidate waits for a writer's approval, or runs as a restricted check when the repository's policy says so. A restricted check receives no grants and never lands. Only this ship's owner can approve in this release.
- The checkout clones anonymously. A private repository refuses the clone and the plan fails with the clone's error. Per-attempt read tokens are later work.
- A credential value must be a single line of at least eight characters. `act` 0.2.89 masks a secret only where the whole value appears on one output line, so a multi-line value would print in clear.
- The CI public key is pinned at enrollment. A rotated CI key needs a fresh enrollment, or `ci_public_key` in the config. The daemon does not walk the certificate chain to the ship's networking key.
- A repository bound to a Clay desk cannot be CI-protected. A repository bound after protection passes CI and is refused at landing with the same reason. Binding a CI-protected repository to a desk is not blocked in this release.
- A matrix strategy, and any job-level `if` other than `needs.<job>.outputs.<name> == '<literal>'`, fail the plan with a diagnosed reason.
- The saved `act` stream is uploaded as `log.jsonl`. Step summaries and artifacts stay in the sandbox and are not uploaded in this release.

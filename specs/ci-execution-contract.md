# CI execution contract (P4, opus chair)

The contracts every P4 piece is built against: the signed execution manifest, the immutable input bundle, the guest image manifest and helper protocol, the sandbox selection and capacity/quarantine/ownership metadata, the policy generation and its state transitions, the HTTP/JSON routes, and the privileged launcher's operations and host recipe. Fixtures for each contract live where the tests are (`runner/internal/*/testdata`, `desk/gen/ci-*-vector.hoon`). Every number below is a ruling of rider 04 unless marked *proposed* (the helper frame bound and the export cap remain implementation bounds, disclosed).

Rulings applied: BRIEF-CI-P4 D1–D7; rider 01 (administrator-owned boundary, unprivileged daemon, separate narrow launcher, held execution); rider 02 (owner-implicit authority, persistent baselines, 15-minute single-use approvals and overrides, atomic consumption, change-driven invalidation, unknown-outcome); rider 03 (locked default, extensible network profiles selected by labels and authorized by ship policy ∩ runner ceiling ∩ launcher ceiling, bound into the manifest); rider 04 (`microvm` default, capacity 1 / 2 vCPU / 4096 MiB + 1024 MiB overhead / 20480 MiB disk, job timeout else 1 h, 120 s cleanup bound, explicit budgets counting every reservation state).

## 1. Sandboxes, selection and requirement

Two backends; an omitted `sandbox` selects `microvm` (rider 04), Docker is an explicit opt-in:

| `sandbox` | What it is | Disclosure string (banner, README) |
|---|---|---|
| `microvm` | One fresh Firecracker microVM per attempt, started by the privileged `urgit-vm-launcher` through the jailer; guest-owned Docker; input by bounded copy over vsock; no host filesystem sharing. | `microvm (Firecracker + jailer; one disposable VM per attempt via urgit-vm-launcher)` |
| `docker-rootless` | Compatibility mode: the P1–P3 container backend, unchanged in shape. Caps only the outer runner container; `disk_mib` ignored; egress unrestricted NAT. | `docker-rootless (container compatibility mode; not a VM boundary)` |

The daemon enrolls with its `sandbox` name and the ship records it (`daemon.sandbox`). Every job attempt carries a **sandbox requirement** `?(%vm %container)` computed by the ship: `%vm` when the repository's `sandbox-requirement` policy says so (default `%vm`; `%container` is the explicit trusted-compatibility opt-in of rider 04), and ALWAYS `%vm` for an untrusted candidate, a trial, a shadow run, or a job that names an environment (privileged). `select-daemon` refuses a `docker-rootless` daemon for a `%vm` attempt; a repository whose only runners are containers shows `no runner satisfies sandbox requirement vm` on the candidate row. The daemon re-checks the requirement in the signed manifest against its own backend and abandons a `%vm` attempt it cannot honour (`sandbox requirement vm; this runner is docker-rootless`) — never a downgrade. Labels stay placement hints; `microvm` as a label is not a requirement.

VM unavailable is an error at every layer: `NewMicrovm` fails when the launcher socket, the image manifest or a digest is missing/mismatched; the daemon then exits with the reason and enrolls nothing.

## 2. Resource contract (D2; rider 04)

| Key (default) | microvm | docker-rootless |
|---|---|---|
| `cpus` (2) | guest vCPU count; the launcher also sets the VMM cgroup `cpu.max = cpus × 100%` (host quota over every thread of the VMM, hence every guest descendant) | `--cpus` on the outer runner container only; act's job containers are siblings and uncapped |
| `memory_mib` (4096) | guest RAM (`machine-config.mem_size_mib`); the VMM cgroup `memory.max = memory_mib + 1024` — the host VMM/helper overhead reservation, bounded, documented, not a measurement claim | `--memory` on the outer container only |
| `disk_mib` (20480) | job-writable budget: the per-attempt drive is the golden image (baseline usage from the manifest) grown by `disk_mib`; the host backing file cannot exceed that size | ignored (disclosed) |
| `image_path` | directory holding `manifest.json` + the kernel + the golden rootfs named in it; digests verified at start | not used |
| `act_image` | the OCI job image act maps `ubuntu-latest` to; must equal the manifest's preloaded image reference | the image on the rootless daemon |
| `capacity` (1) | concurrent guests; admission also requires `Σ(memory_mib + 1024)` and `Σ cpus` over preparing + running + stopping + quarantined reservations ≤ the runner's explicit `budget_memory_mib` / `budget_cpus` (required keys for a VM runner, never derived from the host), and the launcher applies its own root-owned budget the same way; reservation is atomic before provisioning and persisted across crashes | as before |

Validation (S1): negative/zero/unsupported values, `image_path` absent or without a manifest, `act_image` unset for a VM runner, budgets unset or `capacity × (cpus, memory_mib + 1024)` over budget, are refusals at config load with one line per problem; the deadline is the job's `timeout-minutes` else one hour; every owned resource is stopped and cleaned, or explicitly quarantined with the failure visible, within 120 seconds of exit, cancellation or timeout, by the launcher whether or not the daemon lives (no five-minute grace; the daemon's two-minute reporting window never extends guest execution). Labels describe a runner for placement; resource limits constrain what its jobs may use; a label such as `big-mem` neither measures nor reserves RAM (README §6, beside Labels).

## 2b. Network profiles (rider 03)

Locked is the default and the only mode with no NIC. A **network profile** is an extensible record on both sides:

- Runner TOML: `[[network_profiles]] name = "integration"; destinations = ["tcp:192.168.1.229:8472"]` — the runner's ceiling for that profile; the launcher config holds its own ceiling (`allow` list) and refuses anything outside it. The daemon advertises its profile names to the ship (`x-ci-profiles`, at enrollment and every poll) beside its labels; a label like `networked` is placement only.
- Ship policy: `%set-network-policy repo workflow job profile destinations [environment]` (owner or `%ci-policy`), a standing scoped policy stored in `network-policies`; every job not named there is `locked`. Selection requires a daemon that advertises the job's authorized profile and satisfies its labels; none → `no runner supports network profile <p>` on the row (a visible wait). The assignment's manifest binds `network` and `network-scope`; the daemon computes effective = manifest scope ∩ its profile ceiling, passes that to the launcher `reserve`, and the launcher applies effective ∩ its ceiling as the host nft policy for that VM only. A workflow label cannot request a profile; a profile grant releases no credential and no landing/override authority; a policy change bumps the generation and invalidates unused authorizations and running networked attempts (cancellation requested).

## 3. Guest image manifest (`runner/guest/manifest.json`, built by `runner/guest/build.sh`)

```json
{
  "version": 1,
  "kernel":  {"name": "vmlinux-6.1.188", "sha256": "…", "source": {"tarball": "linux-6.1.188.tar.xz", "sha256": "…"}, "config": {"name": "microvm-kernel-ci-x86_64-6.1.config", "sha256": "…"}},
  "rootfs":  {"name": "urgit-guest.ext4", "sha256": "…", "baseline_mib": N, "base_image": "docker.io/library/debian:bookworm-slim@sha256:…", "apt_snapshot": "…"},
  "docker":  {"version": "29.8.1", "tarball_sha256": "…"},
  "act":     {"version": "0.2.89", "sha256": "…"},
  "act_image": {"reference": "docker.io/catthehacker/ubuntu:act-latest", "digest": "sha256:…"},
  "helper":  {"version": 1, "sha256": "…", "source": "runner/cmd/urgit-guest at <tree>"},
  "boot_args": "console=ttyS0 reboot=k panic=1 pci=off nomodule random.trust_cpu=on ipv6.disable=1 init=/sbin/urgit-guest",
  "notices": "runner/guest/NOTICES.md",
  "built": "…", "builder": {"host": "…", "tools": {…}}
}
```

The runner verifies `kernel.sha256` and `rootfs.sha256` at start and again before every Prepare (a changed file refuses, M10). The helper version in the manifest must equal the protocol version the daemon speaks. Bit-identity between two builds is never claimed unless `build.sh --compare` found two outputs equal; the rootfs contains Docker's data root (image layers loaded at build time), whose IDs are not deterministic.

## 4. Guest helper protocol (`runner/internal/guest`, version 1)

Transport: vsock, guest port 5000, exactly one session per boot; the host reaches it through the launcher (`connect`, §9), never through a socket path a job could name. Every frame: `"UG"` (2 bytes), version (1, `0x01`), type (1), big-endian u32 payload length, payload. Payload ≤ 1 MiB (proposed); a longer length, a bad magic or an unknown type closes the session with `ERROR` and the attempt fails `guest protocol violation`. Control payloads are JSON objects; data payloads are raw bytes.

| Type | Direction | Payload | Meaning |
|---|---|---|---|
| `HELLO` 0x01 | host→guest | `{attempt, nonce, bundle_sha256}` | opens the session; the guest answers `READY` or `ERROR` (a second HELLO is refused) |
| `READY` 0x81 | guest→host | `{helper, kernel, docker, machine_id, hostname}` | the helper is init, Docker answers, the job network exists |
| `PUT_BEGIN` 0x02 | host→guest | `{dest, max_bytes, max_entries}` | a bounded tar stream follows; `dest` is under `/work` |
| `PUT_DATA` 0x03 | host→guest | bytes | tar bytes |
| `PUT_END` 0x04 | host→guest | `{sha256}` | end of stream; the guest extracted it and answers `PUT_OK` with its own count/digest |
| `PUT_OK` 0x82 | guest→host | `{entries, bytes, sha256}` | |
| `EXEC` 0x05 | host→guest | `{id, argv, env, cwd}` | start a process; combined stdout+stderr stream as `OUTPUT`; one process at a time |
| `OUTPUT` 0x83 | guest→host | bytes (id in the first 4 bytes) | |
| `EXIT` 0x84 | guest→host | `{id, code}` | |
| `SIGNAL` 0x06 | host→guest | `{id, name, signal}` | deliver a signal to the running process (or by name, `pkill -x`) |
| `EXPORT` 0x07 | host→guest | `{path, max_bytes}` | bounded tar of a guest path under `/work`; answered by `EXPORT_DATA`* then `EXPORT_END {bytes, sha256, truncated}` |
| `SHUTDOWN` 0x08 | host→guest | `{}` | stop Docker, sync, power off |
| `ERROR` 0xff | guest→host | `{code, message}` | |

Tar validation on extraction (M09): no absolute path, no `..`, no component that resolves through a symlink created by the same stream, symlinks only with targets inside `dest`, no device/fifo/socket entries, hard links only to entries already extracted inside `dest`, `max_entries`/`max_bytes` enforced, exact byte lengths and mode bits (exec bits) preserved. The guest never trusts a path from the host beyond `/work`; the host never trusts a length, path or digest from the guest: exports are capped, paths validated, digests recomputed.

The helper is PID 1 (`init=/sbin/urgit-guest`): mounts `proc sys dev devpts shm run tmp cgroup2`, sets hostname `urgit-<attempt-prefix>` and a fresh `/etc/machine-id`, brings up `lo` (and `eth0` only when the kernel cmdline carries `urgit.ip=`), starts `dockerd` on `/run/docker.sock` with `--data-root /var/lib/docker`, waits for `docker info`, creates the job network `urgit-ci`, then listens. It has no host-shell, path-open, mint or "run host command" operation; `EXEC` runs inside the guest only.

## 5. Signed execution manifest (version 2; D5, P10)

The ship signs, with the CI key, the jam of the noun

```
[2 recipient attempt operation expiry nonce manifest]
manifest = [incarnation repo ref candidate oid baseline lock generation workflow job trust sandbox mode network network-scope]
```

| Field | Type | Meaning |
|---|---|---|
| `2` | @ud | message version; a v1 verifier fails closed with `manifest version 2 is not the version this runner speaks` (no shim) |
| recipient, attempt, nonce | @uv | as v1 |
| operation | @t | `assign`, `grant:<name>` or `read:<token-hash>` |
| expiry | @ud | unix seconds |
| incarnation | @uv | the repository incarnation (§7) |
| repo, ref | @t | |
| candidate | @uv | candidate id (0v0 for a resolve) |
| oid | @t | the exact candidate object id, 40 hex ('' for a resolve) |
| baseline | @t | the approved harness revision oid, 40 hex; '' for a trial (the candidate's own bytes) or a resolve |
| lock | @t | the lock digest, 64 hex; '' when none |
| generation | @ud | the repository's policy generation at signing |
| workflow, job | @t | '' for plan/resolve |
| trust | @t | `trusted` / `untrusted` |
| sandbox | @t | `vm` / `container` |
| mode | @t | `required` / `trial` / `shadow` / `plan` / `resolve` |
| network | @t | the network profile the ship authorized for this job: `locked` (no NIC) or a profile name from the repository's network policy (rider 03) |
| network-scope | @t | the ship-authorized destinations, canonical: sorted `proto:addr:port` entries joined by `,` ('' for `locked`); the daemon intersects with its own profile's ceiling, the launcher with its own, and neither may widen it |

The runner rebuilds the noun from the assignment JSON (`manifest` object) and verifies. Grants sign the same manifest with `operation = grant:<name>`; the mirror read capability signs it with `read:<sha256 of the token>`. Cross-language vectors: `desk/gen/ci-provenance-vector.hoon` prints, for a fixed seed key, the jam hex and signature of the reference manifest and asserts each single-field mutation fails; `runner/internal/sig/manifest_test.go` embeds the same bytes and asserts the same (P10).

**An assignment is its atoms** (`runner/launcher/INTEGRATION.md` §11.13). The @uv fields are signed as atoms: a separator anywhere, or leading zeros, in their text sign the same bytes. The runner binds the assignment's `attempt` and `candidate` to the signed ones by atom. Once the signature verifies, it names the assignment in the ship's spelling (`scot %uv`: no leading zero, groups of five from the right): its attempt and candidate in both fields, and the manifest's incarnation. It uses only that spelling from then on: in the claim, the sandbox, the launcher's record and every later message. A delivery of an attempt already claimed here, under any spelling, is ignored before anything is said to the ship. The recipient must still be the runner's own id as written.

**An assignment runs at most once on a runner** (`runner/launcher/INTEGRATION.md` §11.14; assignment-replay ruling 01). The signature stays valid until a minute past the attempt's deadline, and the ship signs each hand-over anew (a fresh nonce): a valid signature is no proof that an attempt has not run. The runner keeps an execution ledger beside its state file (`executions/`). It records each attempt it takes for execution, durably, before the attempt's work directory, sandbox, checkout, grants or run, and again when its run returns. A delivery of an attempt in the ledger — under any spelling, nonce, signature or expiry — is never run again. A finished one is ignored, and nothing is said to the ship: a result the ship did not receive is re-offered at the attempt's deadline, as for a silent runner (CI-DELIVERY-1.1 c). One a stop interrupted is abandoned once verified: the ship offers the job again as a new attempt, or closes it (a privileged one as unknown). A record that cannot be written leaves the attempt unstarted, and abandoned. A ledger that cannot be read, or opened, or that has lost its records, runs nothing. Records are never deleted, rewritten or expired (Q10), and none is evicted when the filesystem is full. A new attempt — a new atom, which the ship mints — runs as before. What a runner upgraded from an earlier version ran before its ledger existed is not known, so that runner runs nothing until its transition (below).

**A transitioned runner runs nothing its ship signed before its transition** (`runner/launcher/INTEGRATION.md` §11.15; legacy-replay-upgrade ruling 01). Every nonce the ship mints is sixteen bytes (`fresh-nonce`), so every assignment it has ever signed carries a nonce below 2^128. An assignment's nonce carries its runner's **authorization epoch** above bit 128 (`epoch-nonce`). The epoch is the number of history transitions the owner had requested for that runner when the assignment was created (`epoch-at`, over the recovery commands, which are never pruned). A runner whose execution history is incomplete runs nothing: its ledger began with a state file kept before the ledger, or its `HISTORY` cannot be read or is missing. Its polls say `x-ci-capacity: 0`, explicitly, and the ship offers it no work. It abandons every assignment it receives, once verified. Once it records the owner's transition to epoch E (§8b), it refuses every assignment whose signed nonce is below E·2^128: everything its ship signed before the transition, under any spelling, expiry or clock. It runs the rest. A runner enrolled with this version needs no transition. `desk/gen/ci-provenance-vector.hoon` and `runner/internal/sig/epoch_test.go` pin the bytes of an epoch-1 assignment's message.

## 6. Immutable input bundle (D4/D5; copied into the guest at `/work`)

```
/work/bundle.json      {attempt, candidate, oid, baseline, harness_paths, replaced_paths, lock, workspace_sha256, actions: [...], downloads: [...]}
/work/src/             the workspace: the exact candidate tree; every path matched by the branch policy's harness-paths replaced by the baseline's bytes
/work/actions/<safeFilename(uses)>/   act's action cache: a git checkout of the mirror at the locked commit, `refs/tags/<ref>` and `refs/heads/<ref>` pointing at it, `origin` = the upstream URL act computes (never fetched: --action-offline-mode)
/work/downloads/<sha256>              inventoried known downloads fetched from the store through the assignment's presigned GET and verified (sha256, size); `index.json` maps URL → sha256, size; mounted read-only into every job container at `/urgit/downloads` (`URGIT_CI_DOWNLOADS`)
/work/toolcache/<tool>/<version>/x64  RUNNER_TOOL_CACHE prepopulated from inventoried toolchains (setup-go finds Go here and downloads nothing)
/work/downloads/<sha256>              (also) the docker-archive of every locked container image (`container:`, `services.*.image`, `uses: docker://`), fetched and verified like a download; before act the runner `docker load`s it into the sandbox's own Docker under `urgit-locked/<path>:sha256-<digest>` (a name no registry serves) and verifies the loaded image against the archive (config id, or the rootfs diff ids on a containerd-backed store); the projection names the job's images by that name, so act (`--pull=false`) runs the locked bytes and never asks a registry (P03)
/work/projected/<workflow>.yml        the single-job projection (CI-PROJECT-1) of the BASELINE's workflow file (required/shadow) or the candidate's (trial)
/work/cache/ /work/artifacts/         act's cache and artifact servers
```

Identities are verified on the host before assembly (`git rev-parse HEAD` = oid and baseline, `git diff --quiet HEAD` on each checkout), the workspace digest (sha256 over sorted `path\0mode\0sha256(content)` lines, symlinks as `path\0l\0target`) is written to `bundle.json`, and the guest recomputes it after extraction and refuses a mismatch (P07). The bundle carries no runner config, bearer, CI key, host path or ship URL; the grants reach act as `--secret` arguments inside the guest only.

Harness paths are per branch policy (`harness-paths`, path prefixes; ERPit: `.github/`, `bin/test.sh`; urgit: `.github/`). A required run whose candidate differs from the baseline under a harness path runs with the baseline's bytes there (recorded in `replaced_paths`) AND the ship stages a trial twin of the candidate under its own harness (mode `trial`; never landable, no grants, untrusted-class storage).

## 7. Ship-side policy, generation, records and transitions (rider 02)

State-0 grows in place:

- `incarnations=(map repo=@t @uv)` — set at first sight, dropped on `%repository-deleted` (a re-created repository gets a fresh one); every binding carries it.
- `generations=(map repo=@t @ud)` — bumped by promotion, revocation, role change, environment change, harness-paths change, sandbox-requirement change, CI-protection change and repository deletion.
- `baselines=(map [repo ref] baseline)` — `[revision=oid harness-paths lock=(unit @ux) promoted=@da actor=@p reason=@t generation=@ud policy-repo=(unit @t)]`; persists until replaced or revoked.
- `locks=(map @ux lock)` — `[repo baseline-revision nodes=(list dep-node) resolved=@da resolver=daemon-id notices=(list @t) bytes=@ud]`; `dep-node = [uses origin kind commit tree subpath digest mirror license location refusal]`.
- `roles=(map repo (list binding))` — `binding = [role=?(%ci-policy %environment-approver %override) scope=(unit @t) ships=(set @p)]`; the authenticated OWNER (`%urgit`'s `ci-owner` peek) holds all roles implicitly.
- `environments=(map [repo name] environment)` — `[name description automation=?(%manual %automatic) credentials=(set @t) created actor]`; unknown environment → refusal.
- `approvals=(map @uv approval)` — bound to (incarnation candidate oid workflow job environment credential-scope baseline lock generation approver), `expires = at + 15 min`, `consumed=(unit attempt-id)`.
- `overrides=(map @uv override)` — (repo ref candidate oid expected-tip actor reason missing-evidence generation at expires=at+15min consumed=(unit @da)).
- `read-capabilities=(map token-hash=@ [attempt repos=(set @t) expires])`.
- `shadows=(map @uv shadow)` and `comparisons=(map @uv comparison)`.
- `audit=(list audit-entry)` — `[at actor kind repo detail before after]`, newest first, bounded (proposed 1000).
- `sandbox-requirements=(map repo ?(%vm %container))`.
- Candidate gains `mode generation baseline lock incarnation sandbox trial-of=(unit candidate-id)`; attempt gains `generation mode manifest=@uv sandbox approval=(unit @uv) outcome=?(%known %unknown)`.

Transitions:

- **Promote** (`%promote-baseline repo ref revision reason [policy-repo]`, owner or `%ci-policy`): requires a lock resolved for that revision (or none if the walk found no external dependency); records before/after; bumps generation; invalidates every pending/running required candidate of the ref (reset in place: plan cleared, attempts stay listed with their old generation, cancellation requested for running attempts through the daemon's next poll answer `cancel`), re-plans them.
- **Resolve** (`%resolve-dependencies repo revision [mappings]`, owner or `%ci-policy`): a `%resolve` assignment to a live resolver-capable daemon (one with a free slot first, else any: a busy resolver takes it at its next poll; only no live resolver at all refuses) carrying the operator's explicit mappings (`[from to]` prefixes: an origin or URL read from elsewhere, each applied one recorded as a lock notice) and, from the inventory answer, a write token for each Git mirror repository `%urgit-ci` creates (`ci-mirror-<owner>-<repo>`: `%create` + `%set-write-token`, cleared at close; every mirror commit stays reachable on its own branch `m-<commit>` after later updates) plus one header-signed store PUT per download sha256 (key `ci/downloads/<sha256>`, payload hash = the sha256, so the store refuses other bytes). A container image is pinned by `skopeo inspect` to its manifest digest and then copied (`skopeo copy`, linux/amd64) into a docker-archive whose sha256, size and config id (the node's `tree`) the lock records and whose bytes go to the store like a download (BRIEF-CI-P4: OCI images may use a digest-addressed object store); a container node without its archive in the store never validates. Downloads live in the ship's object store, never in Git objects (a pill is 210 MB). The posted lock is validated (bounds, refusals, `mirror = store` for downloads) and stored by digest — `shax (jam [repo revision nodes])`, so byte-identical workflows in two repositories are two locks (the P4 battery found the collision); a lock is never executed or refreshed. An explicit update is a later resolve of the same revision: both locks stay visible; the baseline keeps its lock until a promotion names the new one (or, unnamed, the newest).
- **Approve environment** (`%approve-environment candidate workflow job environment credentials`, owner or scoped approver): one approval, 15 min, consumed atomically at admission of exactly one privileged attempt (the assignment is created with `approval` set; the approval's `consumed` = that attempt). Automation (`%automatic`) admits without a manual approval within the environment's credential set. A test approval (`%approve-candidate`) releases nothing.
- **Override** (`%record-override repo ref candidate oid expected-tip reason`, owner or branch `%override` holder): recorded with the missing/failed evidence at that moment, 15 min, and the landing is asked at once; consumed on `%landed`; rechecked in `eligible-at` at every use.
- **Invalidate**: any generation bump marks unused approvals/overrides of the repository expired-by-change, resets required candidates, requests cancellation of their running attempts, and refuses further privilege/landing for the old evidence. A running privileged attempt that is cancelled, times out, loses its daemon or loses its result closes `%unknown` with `outcome %unknown` and `needs-review`; it is never re-offered.
- **Landed** (`%landed`): the candidate's reason is `landed`, the override it used is consumed, and every other open (pending/running) *required* candidate of the same object on the same ref is superseded — `%skipped`, "superseded: the same object landed as candidate …", its running attempts cancelled — since what it was for has happened; trials and shadows keep their own purpose.
- **Landing completion** (`%urgit`, a repository bound to a Clay desk): the parked landing completes onto the repository record as it is at completion — its objects merged, its one ref moved, its pull merged — never by putting back the snapshot taken when it was parked, so a push staged during the window keeps its objects (P11).
- **Delete** (`%repository-deleted`): everything keyed by the name goes (incarnation, generation, baselines, locks of that repo, roles, environments, approvals, overrides, read capabilities, shadows, comparisons, sandbox requirement), as P3 already drops candidates/attempts/assignments/protection/policy/credentials.

`eligible-at/<repo>/<ref>/<oid>/<tip>` (the one predicate the landing path `land-through` and the delayed Clay completion consume): true iff `landing-for` names an authority. That is a candidate of (repo, ref) that is landable (passed, trusted, materialized, mode required, generation and baseline and lock and incarnation current), with candidate oid = `<oid>` **and base = `<tip>`** (restart 01, Q5; `land-candidate` already demanded that tip), OR an unconsumed, unexpired override for (repo, ref) naming candidate oid `<oid>`, expected tip `<tip>` and the current generation, whose actor still holds the authority now. `landing-for/<repo>/<ref>/<oid>/<tip>` answers that authority's candidate id and pull, as a unit and never `[~ ~]`. Several landable candidates may share object and base (a rerun, an approval twin); they are one authority, and the first in map order is named. The push path lands an authorized object through `land-through` by that id, and never through the receive path. The old three-segment `eligible` answers landable-only (no override, no tip) and is kept for reads.

**Replay is refused where the ref moves (Q5).** Every change of a CI-protected ref goes through `land-through`: synchronously, or in the parked Clay completion. In that event `%urgit` requires `eligible-at` for the object over the tip, the ref still at that tip, and the object containing the tip through commit parent links (equality moves nothing). Every other writer stages or refuses; creation and deletion are refused (`native-ci.md`, Protected refs). So a protected ref only moves to descendants and never holds a tip it left, and an authority bound to a tip can never move it twice. That holds whether or not `%urgit-ci` has processed `%landed`. `%landed` is the record: candidate landed, override marked consumed, duplicates superseded, audit. It is not the replay guard, and it is not same-event consumption. An override of a candidate staged against another tip (its object does not contain the current tip) is refused: it would drop that tip's commits. Un-protecting a ref frees it; re-protecting bumps the generation and invalidates what was unused.

## 8. Routes (session-authorized unless noted)

| Route | Purpose |
|---|---|
| `GET ci/repository/<name>/policy` | grows: baselines, roles (owner shown implicit), environments, harness-paths, sandbox-requirement, generation |
| `GET ci/repository/<name>/lock/<digest>` | the lock's nodes, notices, refusals, source locations |
| `GET ci/repository/<name>/audit` | the audit list |
| `GET ci/candidate/<id>` | grows: mode, generation, baseline, lock, sandbox, trial-of, approvals, overrides |
| `GET ci/shadow/<id>` | the exportable comparison record |
| `POST ci/action` | grows the allow-list: `resolve-dependencies`, `promote-baseline`, `revoke-baseline`, `set-harness-paths`, `set-sandbox-requirement`, `set-role`, `clear-role`, `set-environment`, `delete-environment`, `approve-environment`, `record-override`, `stage-shadow`, `compare-shadow`, `cancel-attempt` |
| daemon: `POST ci/attempt/<id>/lock` | the resolver posts the lock (bearer) |
| daemon: `GET ci/attempt/<id>/cancel` | (folded into the poll answer: `{"cancel": [attempt…]}`) |
| `%urgit`: `GET /git/<mirror>/…` | accepts `Authorization: Basic x:<read-capability>` for the repositories the capability names (the narrow content-access handoff) |

Errors are the refusal's own words (409 with the reason), as P3's action route answers them; the UI shows them verbatim.

## 8b. Legacy recovery from the Runners panel (legacy-recovery UI ruling 01)

A retention that a runner before settled admission recorded without its reserve request is released from Urgit only. The running daemon carries the release out on its own evidence (`runner/launcher/INTEGRATION.md` §11.12; QUESTIONS-SOURCE-01 §11). The daemon's routes take its own bearer (`x-ci-bearer`) and refuse the ship's session: a request a browser made is never a runner's result.

| Route | Who | Purpose |
|---|---|---|
| daemon: `POST ci/daemon/<id>/retentions` | bearer only | the daemon's report: `{version, at, sandbox, capacity {configured, withheld, held, running, advertised}, retentions [...], truncated, released [...], history}`. `history` is `{selection 'history/<since>', revision <since>, since, known, complete, stateFormat, paused, epoch, evidence, transition? {epoch, command, at}, explanation}` (§11.15). Each retention is `{selection, revision, handle, attempt, label, backend, kind, identity, reason, retained, legacy?, release, explanation, eligible, evidence, conditions?, facts?}`. `kind` is one of `legacy`, `admission`, `vm`, `docker`, `unknown-backend`, `unmarked`, and `release` one of `urgit`, `settlement`, `cli`, `none`. The ship refuses a body over 1 MiB (413), or any entry without its selection and revision (422, whole). It keeps the latest report and authorizes nothing by it. |
| `GET ci/runners/<id>/recovery` | session | `{runner, now, reported, report, commands [...], history {epoch, next, assignments, running}, attempts {<attempt>: {repo, ref, workflow, job, kind, status}}}`: the latest report as posted, the runner's commands newest first, the runner's authorization epoch at the ship with what it holds for it, and what the ship knows of each retention's attempt |
| `POST ci/action` `{action: 'request-legacy-release', id, selection, revision, evidence}` | session (owner); not delegable | records a command for the runner. It is refused (409) unless the latest report shows that selection at that revision, `kind` legacy, `eligible`, and that evidence digest. At most one command per entry is open (queued, delivered or uncertain); a repeated request is that one. |
| `POST ci/action` `{action: 'request-history-transition', id, revision, evidence}` | session (owner); not delegable | records a `confirm-history` command for the runner, with selection `history/<since>` and evidence `epoch <E> history <digest>`, E the runner's next epoch. It is refused (409) unless the latest report shows the runner's history at that revision, `paused`, with that evidence digest. At most one transition per runner is open; a repeated request is that one. From its request on, every assignment the ship creates for the runner is signed with epoch E. |
| daemon: `GET ci/daemon/<id>/assignment` with `x-ci-recovery: 1` | bearer | when no assignment waits: `{recovery: {version 1, id, recipient, operation 'release-legacy' or 'confirm-history', selection, revision, evidence, label, expiry, nonce, sig}}`. The ship hands over the oldest queued command before its expiry, else one handed over at least two minutes earlier with no final answer. A queued command past its expiry becomes `expired`, never handed over. |
| daemon: `POST ci/daemon/<id>/recovery` `{command, status, detail, ...}` | bearer only | the daemon's answer: `completed`, `refused` or `uncertain`. `completed` and `refused` are final, and the daemon records each in its state file before it sends it. `uncertain` is not final: a release whose save was uncertain, a refusal not recorded yet, or a message the daemon cannot authenticate. The command stays open, and the ship hands it over again. `completed` stands against any earlier answer; a refusal or doubt changes only an open command. |

The CI key signs the jam of `['recovery' 1 recipient command operation expiry nonce selection revision evidence]`. `recovery` and `1` are the tag and version; the recipient, command and nonce are @uv; operation, selection and evidence are cords; expiry and revision are @ud. Its leading tag keeps it apart from §5's `[2 ...]`: no signature verifies as the other kind. `desk/gen/ci-recovery-vector.hoon` and `runner/internal/sig/recovery_test.go` pin the same bytes.

The daemon carries a command out only when:
- the signature verifies against its pinned key, for itself and `release-legacy` or `confirm-history`;
- the command has not expired by its own clock;
- the selection, identity and revision exactly name a legacy retention;
- its attempt is idle here;
- a fresh launcher `hello` and authoritative `list` give every condition, and the evidence digest confirmed.

It releases the entry in its state file with the command and the digest, and returns the slot only once that save is durable. It records every refusal of a command it can authenticate in the same file before it answers: the command, selection, revision, evidence and reason. A command it has decided, carried out or refused, is answered from that record, and is never judged again: not in the same life, not after a restart, and not after the command's expiry. A new confirmation is a new command. A message it cannot authenticate decides nothing. It is recorded nowhere, and answered `uncertain` (`runner/launcher/INTEGRATION.md` §11.12, "Every final answer is recorded first"). The panel reads `completed` only from the daemon's answer, never from the ship's receipt.

A command is its atom. The signature binds the `@uv`, not its text, so every spelling of the id (a separator anywhere, leading zeros) is the same command, with one decision. The daemon names the command in every answer and report as the ship writes it (`scot %uv`: no leading zero, groups of five from the right). It finds every record by the command's atom, whatever spelling an earlier version recorded it under, and keeps that record as written. The recipient must still be the daemon's own id as written (`runner/launcher/INTEGRATION.md` §11.12, "One command, one identity").

**A runner's history transition** (legacy-replay-upgrade ruling 01; `runner/launcher/INTEGRATION.md` §11.15) is a `confirm-history` command, authenticated and answered the same way. The daemon carries one out only when its selection is its history's now, its evidence names an epoch of 1 or more and its history's evidence digest now, and it is waiting for a transition. It records the transition in its state file (`transition {epoch, command, evidence, at}`) and answers completed only once that save is durable. An uncertain save answers `uncertain`, leaves it waiting, and is settled `refused`, not applied, once the state file is durable again. A transition releases nothing: a retention's slot stays withheld. The panel offers it only for a runner its latest report shows waiting, and reads `Transitioned` only from the daemon's answer.

**A transition record is a transition only when it is one this version writes** (`runner/launcher/INTEGRATION.md` §11.16; independent review 12). It is an object of exactly `epoch` (1 or more), `command` (a `@uv` in the ship's spelling), `evidence` (the sha256 of this runner's history as it is now) and `at` (its time), each named once. Anything else in the state file's `transition` is no transition: the state file loads, the runner waits, and nothing it holds lifts the pause or answers a command. It keeps the record as the file holds it, and its report carries `history.invalidTransition {problem, record}`. The owner's transition supersedes such a record and keeps it, as the file held it, in `superseded_transitions {record, problem, by, at}`. A state file that names a field twice anywhere else, its `transition` among them, is refused at the load and left as it is: no runner writes one, and which of the two is meant is not known.

## 8c. The controller's state: its versions and their migration (state-migration ruling 01)

This model was written before the code that implements it.

`%urgit-ci`'s persisted state is versioned. An upgrade preserves it in place: no export, nuke, import or re-enrollment. This follows state-migration ruling 01 (`.scratch/source-stage-01/orchestrator/state-migration-01/RULING.md`, Q12 A; QUESTIONS-SOURCE-01 §12). For this agent it supersedes AGENTS.md's disposable state-0 rule. No agent on a ship has been migrated.

**Supported shapes.** Three historical shapes are known from this worktree's own sources. They are distinct sources of evidence, and none is claimed to have been deployed. Each is frozen byte for byte, with every type it uses, as a sur file of its own:

| Shape | Its source | Frozen as | Fields after its tag |
|---|---|---|---|
| the committed base (P3, closed out) | `desk/sur/ci.hoon` at commit `c82a24e` | `desk/sur/ci-state-base.hoon` | 9 |
| review 06's schema (P4, before Q11) | the accepted snapshot, `orchestrator/legacy-recovery-ui-01/accepted-review06/` | `desk/sur/ci-state-review06.hoon` | 26 |
| the Q11 schema | `submissions/08` | `desk/sur/ci-state-q11.hoon` | 28 |

All three carry the tag `%0`, so the tag cannot tell them apart. The current state is `state-1`, tagged `%1`. It has the Q11 fields exactly, with the same types. Shapes that P4's development went through between the base and review 06 were never preserved; they are not supported and are refused.

**Loading.** `on-load` reads the saved state as a noun, never by its vase's type:
- `%1`: it must be exactly a `state-1`. It loads as it is.
- `%0`: it is checked against each frozen shape (`;;`, which must hold as a fixpoint). If exactly one fits, it is converted. If none fits (an unknown, partial or corrupt state), or more than one fits (an ambiguous one), it is refused.
- Any other tag, or an atom, is refused.

A refusal crashes `on-load` with its reason. Nothing is cast, reset or read another way. That Gall then keeps the running version and its saved state is expected, but it is to be qualified live.

**Conversions** (`desk/lib/ci-migrate.hoon`), each written out field by field:
- **From the Q11 schema**: its 28 fields as they are, under the tag `%1`.
- **From review 06's**: its 26 fields as they are. The Q11 stage's two maps start empty: no retention report yet, since a runner posts one at its next reconcile, and no recovery command, since none could exist.
- **From the committed base**: its 9 fields. Every record is completed with what P4 added, and every map, set and list that P4 and Q11 added starts empty. None of it existed.
  - **A daemon** keeps everything it had, unchanged: its id, token and bearer hashes, minting, enrollment, last contact, capacity, sandbox, running attempts, revocation, refusal, labels and repositories. A revoked daemon stays revoked, and an enrolled one keeps its bearer. It gets no network profiles, which a daemon declares again on every poll, and resolver false, since it enrolled without declaring one.
  - **A candidate** keeps everything it had. Its P4 bindings are none:
    - mode required, which is what a pre-P4 candidate was to its ref;
    - no baseline and no lock, generation 0 and incarnation 0v0;
    - the VM, no trial, and its harness difference not computed.

    `bindings-current` needs a candidate's baseline to be the promoted one, so a migrated candidate is never current evidence. Nothing migrated lands without being staged again under P4.
  - **An attempt** keeps everything it had, and a running one stays running. What its signed manifest would have carried is none: generation 0, mode required, no manifest (none was signed), the VM, no network recorded, and no approval. Its outcome is known: its status and result are what they were.
  - With no baseline promoted, a protected ref waits for the owner's promotion before anything lands, as P4 requires of every repository.
- **Every conversion** carries everything else over as it is:
  - the enrolled daemons, the signing key and ship keys;
  - the protected refs, policies and credentials;
  - the candidates, assignments and attempts;
  - whatever P4 and Q11 recorded: approvals with their expiry, consumption and invalidation; overrides; roles; environments; baselines and locks; the audit; recovery commands with their status and reports.

  An expired approval stays expired, a consumed one consumed, and a revoked runner revoked. Nothing is renewed or revived.
- **The audit** records each migration (`state-migration`, with the shape it came from). The runner is untouched: its state file, retentions and recorded refusals stay authoritative. Its enrollment carries over with its daemon record, so it goes on polling with its bearer.

**What checks it here**, and what does not:
- `desk/gen/ci-migration-vector.hoon` is the fail-able vector. It covers:
  - a populated state of each shape: enrollment and a revoked daemon, trust, policy and credentials, history, running attempts, expired and consumed approvals, and open and final recovery commands;
  - the current state reloaded unchanged;
  - an unknown version, an atom, a malformed `%0`, a corrupt field and an ambiguous classification, each refused.

  It needs a ship, and is NOT RUN here.
- `tools/q12_static_check.py` is a static check of this source, not a Gall migration. It checks that:
  - each frozen shape is byte-identical to its source;
  - each conversion reads every field of its source shape and constructs every field of `state-1`, in order.

  Its negative control drops a protected field, and it must fail.
- **Held** until a fake ship is available:
  - the Hoon compilation of the frozen shapes, the library, the agent and the vector;
  - the vector's run;
  - a persisted upgrade from each shape: that shape's own code installed with a populated state, then this desk committed over it;
  - a refused `on-load` observed keeping the running version.

## 9. Privileged launcher and host recipe (rider 01; execution HELD pending review)

`urgit-vm-launcher` (Go, static, built from `runner/cmd/urgit-vm-launcher`, sha256 recorded in RECORD-CI-P4.md) runs as root under systemd, owns `/var/lib/urgit-ci-p4-opus` and `/run/urgit-ci-p4-opus`, and speaks JSON lines on `/run/urgit-ci-p4-opus/launcher.sock` (root:`<runner uid's group>` 0660). Every connection is authenticated by `SO_PEERCRED` uid = the configured runner uid; every VM is owned by (uid, daemon id from `hello`) and only that owner may `inspect/connect/stop/destroy` it. The launcher accepts no path, mount, uid, network or cgroup value from a request: images are named by manifest digest and must already sit under `/var/lib/urgit-ci-p4-opus/images/<digest>/` (root-owned, copied there by the administrator after verifying the digests in `runner/guest/manifest.json`); sizes are bounded by its own config.

| Op | Request | Effect |
|---|---|---|
| `hello` | `{daemon}` | binds the connection to (uid, daemon) |
| `reserve` | `{attempt, image, cpus, memory_mib, disk_mib, deadline_unix, network}` | admission against the launcher's own budget (preparing, running, stopping and quarantined reservations all count; atomic; persisted before any provisioning); creates `/var/lib/urgit-ci-p4-opus/attempts/<id>/` with the ownership record; answers `{id, cid}` |
| `create` | `{id}` | reflink-copies the golden rootfs, `resize2fs` to baseline+disk_mib, `tune2fs -U random`; creates the cgroup `urgit-ci-p4-opus.slice/<id>` (`cpu.max`, `memory.max`, `pids.max`); with `network=policy` creates netns/veth/TAP and the host nft policy (§3 CIDRs, allow-list from the launcher config only); runs `jailer --id <id> --exec-file <fc> --uid <vm uid> --gid <vm gid> --chroot-base-dir /var/lib/urgit-ci-p4-opus/jail --cgroup-version 2 --parent-cgroup urgit-ci-p4-opus.slice --new-pid-ns --daemonize [--netns …]`; configures the VM over its API socket (kernel, boot args + `urgit.attempt=<id>`, drive, vsock cid, machine config, optional NIC); starts it; records the jailed pid and start time |
| `connect` | `{id, port}` | opens the VM's vsock UDS inside the jail, performs the `CONNECT <port>` handshake, and passes the connected fd back over `SCM_RIGHTS` |
| `inspect` | `{id}` | state, pid liveness (verified by `/proc/<pid>/cmdline` containing `--id <id>`), deadline, cgroup usage |
| `stop` | `{id}` | SIGTERM then SIGKILL the jailed VMM |
| `destroy` | `{id}` | stop, remove nft rules/veth/netns, rmdir cgroup, remove jail and attempt dir; idempotent; any failure leaves the record `quarantined` with the reason and keeps the reservation counted |
| `list` | `{}` | the caller's owned ids with states |

The launcher enforces the independent deadline itself: a VM past `deadline_unix` is stopped and its resources removed within 120 seconds or the record is marked `quarantined` with the failure visible (`deadline-reaped` / `cleanup-failed`), whatever the daemon is doing; on its own restart it re-reads every attempt record and reaps or quarantines. Quarantine is cleared only by the root CLI `urgit-vm-launcher clear <id>` after an operator inspected it.

**Host recipe (for the administrator; nothing here is run by this chair):**

```
# 1. protected roots (rider 01)
install -d -o root -g root -m 0755 /var/lib/urgit-ci-p4-opus /var/lib/urgit-ci-p4-opus/bin /var/lib/urgit-ci-p4-opus/images /var/lib/urgit-ci-p4-opus/jail /var/lib/urgit-ci-p4-opus/attempts
# 2. pinned binaries, hash-verified against runner/guest/manifest.json and RECORD-CI-P4.md
install -o root -g root -m 0755 <worktree>/.scratch/ci-p4/tools/release-v1.17.0-x86_64/firecracker-v1.17.0-x86_64 /var/lib/urgit-ci-p4-opus/bin/firecracker   # sha256 99ad0f5c…e7a5
install -o root -g root -m 0755 <worktree>/.scratch/ci-p4/tools/release-v1.17.0-x86_64/jailer-v1.17.0-x86_64      /var/lib/urgit-ci-p4-opus/bin/jailer        # sha256 65ef226e…434f
install -o root -g root -m 0755 <worktree>/runner/urgit-vm-launcher /var/lib/urgit-ci-p4-opus/bin/urgit-vm-launcher   # sha256 in RECORD
install -d -o root -g root -m 0755 /var/lib/urgit-ci-p4-opus/images/<manifest sha256>
install -o root -g root -m 0644 <worktree>/.scratch/ci-p4/images/{manifest.json,vmlinux-6.1.188,urgit-guest.ext4} /var/lib/urgit-ci-p4-opus/images/<manifest sha256>/
# 3. a dedicated unprivileged uid for the jailed VMM
useradd -r -s /usr/sbin/nologin -d /nonexistent urgit-vm
# 4. the launcher's root-owned config and unit
install -o root -g root -m 0600 <worktree>/runner/launcher/urgit-vm-launcher.toml /etc/urgit-vm-launcher-p4-opus.toml   # runner_uid=1000, vm_user=urgit-vm, budgets, cidr pool, allow-list
install -o root -g root -m 0644 <worktree>/runner/launcher/urgit-vm-launcher.service /etc/systemd/system/urgit-vm-launcher-p4-opus.service
systemctl daemon-reload && systemctl enable --now urgit-vm-launcher-p4-opus.service
```

Ownership-scoped cleanup: `systemctl stop` destroys every VM it owns and removes only `urgit-ci-p4-opus.slice`, `urgit-p4o-*` namespaces/interfaces and the `urgit-ci-p4-opus` nft table; nothing else on the host is touched.

## 10. Shadow comparison (D7)

`%stage-shadow repo ref head base event=@t external-id=@t` stages a candidate in mode `shadow`: planned and run like a trial (no grants, untrusted-class storage, never landable, never deploys). `GET ci/shadow/<id>` exports `{repo, ref, oid, event, baseline, lock, generation, verdict, jobs: [{workflow, job, status, reason}], complete}`. `%compare-shadow shadow=<id> oid event verdict jobs` records agreement only when oid, event, and the shadow's own inputs are complete and equal and every job's verdict matches; anything else is recorded as `mismatch` with the reason, never as agreement.

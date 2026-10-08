# `%urgit`

Install from `~matwet`:

```hoon
|install ~matwet %urgit
```

<img width="947" height="585" alt="Urgit repository view" src="https://github.com/user-attachments/assets/b13cdad5-ab95-4e9a-95af-e3fc81fbdd18" />

`%urgit` turns an Urbit ship into a Git remote and collaborative forge. Standard Git clients connect over Smart HTTP, while the ship itself owns repositories, objects, refs, permissions, and collaboration state. No Git executable or server-side sidecar is required.

## What it provides

### A native Git remote

Clone, fetch, and push with ordinary Git clients at a stable URL:

```text
https://ship.example/git/<repository>
```

Urgit stores canonical Git blobs, trees, commits, and tags and verifies their SHA-1 object IDs on ingestion. It supports branches, protected refs, force updates, tag and release management, shallow and partial clones, and Git LFS uploads, downloads, locking, and cleanup.

### A web forge

The authenticated app at `/apps/urgit` provides repository and branch management, file browsing and editing, Markdown READMEs, history, blame, search, diffs, patches, releases, issues, pull requests, webhooks, and repository settings. Public repositories have read-only pages, and `/urgit` publishes a profile and public-repository index using the ship's Landscape identity.

### Urbit-native collaboration

Ships can discover shared repositories, keep peers in the sidebar, fork repositories, pull updates from an origin, and send authorized fast-forward updates back. A ship can also ask a whole Tlon Groups group it belongs to for repositories at once; the sidebar's Groups menu lists the answers by ship and repository, marking each one a group's policy alone made readable with that group's name, and counts the members that answered, run no urgit, were unreachable within 30 seconds, or are pending. A pending member is one whose earlier request is still unacknowledged: at most one request rides to a ship at a time, and that hold lapses after an hour so the next ask goes out fresh. On the current kernel a member whose urgit is suspended or nuked is held rather than refused, so it reports as unreachable, not as running no urgit. Native issues, discussions, review comments, pull requests, merges, and Landscape notifications use ship identity rather than a separate account system. Private repositories keep read and write access as separate permissions.

### A Git–Clay bridge

A repository branch can be bound to a Clay desk. Git pushes to that branch are projected into Clay and only become authoritative after Clay and Ford accept the desk; failures are returned to the Git client without moving the ref. The reverse operation snapshots a live desk into canonical Git objects and records the relationship between Clay revisions and Git commits.

### GitHub, LFS, and automation

Urgit can import from and synchronize with GitHub over Git Smart HTTP, optionally using a server-side token for private repositories and GitHub API operations. Signed incoming and outgoing webhooks connect pushes, tags, reviews, issues, releases, and Clay synchronization to external automation. Large LFS payloads move directly through the ship's configured object storage using short-lived signed requests, keeping those bytes out of the loom.

## How it works

| Component | Responsibility |
|---|---|
| Eyre | Owns `/git/<repository>` Smart HTTP and web/API routing. |
| `%urgit` Gall agent | Authenticates requests; validates objects and packs; owns repositories, refs, policy, collaboration, and atomic updates. |
| React frontend | Presents authenticated, public, and peer repository views without receiving repository secrets. |
| Clay bridge | Projects a bound Git tree into a desk and publishes desk revisions back into Git. |
| Ames, Mesa, and Fine | Coordinate peer operations and transport verified repository snapshots between ships. |
| Object storage | Holds verified Git LFS payloads behind signed transfer actions. |

Git wire data is parsed and produced natively in Hoon, including pkt-lines, pack v2, zlib/DEFLATE, and delta objects. Incoming updates are staged and fully validated before objects and refs change together. Peer transfers follow the same rule: recompute object IDs, validate the referenced graph, then install atomically.

See [`specs/architecture.md`](specs/architecture.md) for protocol behavior and trust boundaries, and [`specs/roadmap.md`](specs/roadmap.md) for planned work.

## Access model

Native ship access distinguishes readers from writers:

| Access | Discover, browse, and fork | Open issues and join discussions | Send native branch updates |
|---|---:|---:|---:|
| Public visitor | Yes | Yes | No |
| Reader | Yes | Yes | No |
| Writer | Yes | Yes | Yes |
| Group member with a read role | Yes | Yes | No |
| Group member with a write role | Yes | Yes | Yes |
| Owner | Yes | Yes | Yes |

Writers also receive reader access, and branch protection still applies to their updates. Public pages and peer responses omit tokens, ACLs, and other administration fields.

A private repository can also take readers and writers from a Tlon Groups group the ship is a member of, hosted by the ship itself or by another ship. The owner picks one of the ship's groups and its roles by title from the settings panel (Groups' slugs and role ids are shown alongside, never typed), chooses what a seated member with no roles gets (nothing by default), and maps roles to read or write; a member with several roles gets the strongest one, and roles that are not mapped grant nothing. Group membership is checked against the running `%groups` agent on every request and never stored, so if the group cannot be read, only the explicit lists apply. A group hosted elsewhere is read from the local copy `%groups` keeps of it, and counts only while `%groups` reports that copy initialised and this ship still holds a seat in the group; otherwise, again, only the explicit lists apply. Repository administration stays with the owner regardless of group roles.

Git clients use a separate per-repository write token over HTTP Basic authentication. Any username is accepted; the token is the password. Public repositories allow unauthenticated fetches and LFS downloads, while private reads and all writes require the token.

## Getting started

1. Install Urgit and open `/apps/urgit` on the ship.
2. Create an empty repository, publish a mounted desk, fork from another ship, or import from GitHub.
3. Copy the repository's Smart HTTP URL into `git clone` or `git remote add`.
4. For pushes, create or rotate the write token in repository settings and give it to your Git credential helper when prompted.

To connect Git and Clay, bind a repository branch to a mounted desk in repository settings. The repository page reports whether the branch and desk are synchronized, ahead, or divergent, and exposes explicit actions to apply either side.

## Development

The build requires Git, Zig, and Node.js 22 (or Node.js 20.19+). Build the source desk and copy it onto a mounted ship desk with:

```sh
zig build -Ddesk=/path/to/pier/urgit
```

The Zig build installs the locked frontend dependencies and builds `fe/` into `desk/web/`. During frontend work, run its tests directly:

```sh
cd fe
npm test
npm run build
```

After updating and reviving the desk, run the relevant Hoon protocol vectors from the Dojo. The complete set lives in [`desk/gen`](desk/gen); for example:

```hoon
+urgit!git-codec-vector
+urgit!git-pack-vector
+urgit!git-clay-vector
```

## CI

The Go tests, the frontend tests and every Hoon vector generator run as three jobs of [`.github/workflows/urgit.yml`](.github/workflows/urgit.yml) on every push; the Hoon job boots a fresh fake ship inside the job with the composite actions under [`.github/actions`](.github/actions).
It runs on urgit-ci itself — the repository's own CI, on the ship that hosts it — as well as on any runner that takes a GitHub Actions workflow.

Urgit-ci (`%urgit-ci`) runs GitHub Actions workflows for CI-required branches: a push to such a branch is staged as a candidate, planned and run by an enrolled runner daemon, and lands the branch only when every required job passes under the branch's promoted policy. The design is in [`specs/native-ci.md`](specs/native-ci.md) and the execution contract in [`specs/ci-execution-contract.md`](specs/ci-execution-contract.md); [`specs/ci-compatibility.md`](specs/ci-compatibility.md) is the honest ledger of what is and is not supported.

### Policy: baselines, locks, promotion

Required checks come from a **promoted baseline** — an explicitly promoted revision of the branch's workflow files — and its **resolved lock**, never from the candidate's own YAML. A candidate that edits a harness path (`.github/` by default) gets an unprivileged **trial** of its own harness beside the required run; trial evidence never lands and never replaces required evidence. Resolving a revision (Settings → CI policy → Resolve) walks its workflows on a resolver-capable runner and inventories every `uses:`, container image, nested composite action and known download with an immutable identity, mirrors actions into repositories on the ship and downloads — and each container image, as a docker-archive of the digest it was pinned to — into the ship's object store, and stores the lock; nothing is executed, and execution later reads only those mirrors (a locked image is loaded into the sandbox's own Docker from its verified archive before the job, under a name no registry serves; the job network is locked by default). A mutable upstream tag moving changes nothing until an explicit re-resolve and a promotion adopt it; the two locks and their diff stay visible. Every policy change (a promotion, a role, an environment, harness paths, the sandbox requirement, a network policy) moves the repository's **policy generation**: unused approvals and overrides are invalidated and open candidates re-run under the new bindings, whether their landing is synchronous or parked on a Clay write. The repository owner holds every CI role; other ships act only through delegated, scoped role bindings (`ci-policy`, `environment-approver` per environment, `override` per branch) and post their requests from their own ship's interface, judged and audited here under their name. A job that names an `environment` is privileged: it needs an environment approval (fifteen minutes, single use, consumed at admission) or the environment's automation rule, and receives only the environment's credentials the approval released; an override advances a branch to an exact object without the missing evidence, is recorded with the actor, the reason and what was missing, and does not turn a failure green.

### Runners and sandboxes

A runner is the `urgit-runner` daemon, enrolled with a token the ship mints once. Copy [`runner/urgit-runner.toml.example`](runner/urgit-runner.toml.example) to `/etc/urgit-runner.toml` (mode 0600), paste the ship URL and the token, and start the daemon: it enrolls, pins the ship's CI signing key, and polls for signed assignments. Its banner names the backend it runs — `sandbox: microvm (Firecracker + jailer; one disposable VM per attempt via urgit-vm-launcher; image …)` or, only for a repository that opted in, `sandbox: docker-rootless (container compatibility mode; not a VM boundary; a network profile is NAT egress as a whole, destinations not narrowed)`. No dojo action is part of the setup.

| Backend | Boundary | Required work | Network | Note |
|---|---|---|---|---|
| `microvm` (Firecracker + jailer) | VM | the default; every privileged, trial, shadow and untrusted job | locked by default; profiles enforced per destination by the launcher | the product path; needs the administrator-owned `urgit-vm-launcher` and a verified guest image, else the daemon refuses to start |
| `docker-rootless` | container (not a VM boundary) | only a repository opted in with `set-sandbox-requirement container` | locked = an `--internal` bridge; a profile = NAT egress as a whole (destinations are not narrowed) | explicit trusted compatibility; never selected for VM-required work; a job's container images are loaded from the ship's store before the job, never pulled (the daemon itself has host network, which is why the runner never lets act ask it for an absent image) |

Labels are placement, not containment: a job's `runs-on` labels pick which enrolled runner may take it, and only the runner's backend and the repository's sandbox requirement decide what contains it. Resources are explicit: each guest gets `cpus`, `memory_mib` (plus 1024 MiB of host overhead the launcher reserves) and `disk_mib`, a runner declares `budget_cpus`/`budget_memory_mib` it may hold across every guest (preparing, running, stopping and quarantined ones count), the launcher admits every reservation against its own ceiling, and a job runs no longer than its `timeout-minutes` (one hour when unset); cleanup is bounded to 120 seconds, after which a guest that will not die is quarantined, visibly, and its capacity stays taken — across restarts, until an operator who inspected it releases it, cleanup and release being separate steps ([`runner/README.md`](runner/README.md) §6): `urgit-vm-launcher recover` for a guest's incident, `urgit-runner -recover` for the daemon's retention of it, and — for a retention a runner before settled admission recorded without its reserve request — **Settings → Runners → Retentions**, which the running daemon carries out on its evidence. A runner upgraded from a version without the execution ledger runs nothing until its owner confirms its transition there (**Transition…**): from then on it refuses every assignment its ship signed before, and runs new work ([`runner/launcher/INTEGRATION.md`](runner/launcher/INTEGRATION.md) §11.15). The product defaults are 1 guest × 2 vCPU × 4096 MiB × 20480 MiB; a test campaign's larger caps are configured explicitly per runner and launcher, never inferred from the host.

**Qualification status.** The container compatibility profile is exercised end to end by the P4 battery. The VM path's image, kernel, guest helper and protocol are built from the recipes under [`runner/guest`](runner/guest) and proven by unjailed preflight boots; the jailed path (launcher, cgroups, network policy) waits on the administrator's review of the launcher recipe and is not claimed here.

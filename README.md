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

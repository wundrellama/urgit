# CI P2 questions — astra

Stopped before S0 on `ci/p2-astra`, starting at
`370293857f82ce662bfdfcdd68ebce0e5f15d2d6`. The working tree was clean.
Read the brief, the named spec sections, the rulings, `desk/sur/ci.hoon`,
`desk/app/urgit-ci.hoon`, `desk/lib/ci-storage.hoon`, `runner/README.md`, and
`.scratch/p1-live-table.md` in the prescribed order.

These are source and harness inspection findings, not live test results. No
stage, negative RED/GREEN pair, or battery row is claimed complete. No ship or
Docker daemon was started. The footer pier
`/var/home/michael/piers/urgit-ci-p2-lup` does not exist. This questions-only
commit follows the brief's §6 box-and-stop instruction; it is not S0.

## §1 — Which first-commit requirement governs?

**Read:** `BRIEF-CI-P2.md:164–169` requires D7 in one commit, "spec only,
first in your sequence." The stage list at lines 237–240 makes S0 the spec
commit. The launch footer at lines 259–260 separately requires the two
harness environment files to be rewritten "as your first commit."

**Tried:** Inspected `git log`, the existing footer commit, and both environment
files without sourcing them. Commit `3702938` adds only the footer to the
brief; it does not configure the harness. `.scratch/ci-p0/env.sh` still names
`~peg`, port 8350, and the P1 pier; `.scratch/ci-p1/env.sh` still names
`/run/user/1000/ci-p1-closeout`. Thus neither instruction is already satisfied
by the planted footer commit. A spec-only first stage commit cannot also
contain the required environment edits.

**Question:** Where should the footer environment changes be committed when
the stage sequence resumes?

- **S0 stays spec-only (recommended):** commit D7 as S0, then put the footer
  environment changes in S1 before any harness execution. Clarify that the
  footer means before first boot, rather than in the first commit.
- **S0 includes the environment:** commit D7 and the two environment files
  together, with an explicit exception to D7's spec-only requirement.
- **A separate setup commit precedes S0:** commit the environment first, then
  do S0–S6, with an explicit exception to D7's first-in-sequence requirement.

## §2 — How does the required log redirect authenticate to the bucket?

**Read:** D2 (`BRIEF-CI-P2.md:68–76`) requires a session-authorized log route
that returns a 302 to the `sign-get` URL, and forbids changing `ci-storage`
beyond adding the read-for-viewer wrapper. Q2 requires following that URL to
return the JSONL. `desk/lib/ci-storage.hoon:22–26,52–80` explicitly describes
header-authorized requests and delegates GET signing to P0's signer.

**Tried:** Traced that call into `desk/lib/git-storage.hoon:126–172`.
`signed-request` is `[url headers]`. The URL is only scheme, host, bucket,
and object path (line 139); the signature is in the `authorization` header
(lines 162–170), alongside the required `x-amz-*` headers. The existing
`%sign-get` scry in `desk/app/urgit-ci.hoon:135–156` returns only `url`.
There is no authentication query in that URL. A browser following a 302
Location does not turn the signer’s separate header list into request
headers for the object store. A wrapper returning the existing URL therefore
cannot satisfy Q2 for a private bucket. No live request was attempted.

**Question:** Which read contract and signer fence should S1 implement?

- **Keep the 302 (recommended):** authorize a query-presigned SigV4 GET
  implementation in the viewer wrapper, with explicit expiry and trust
  checks, while preserving the existing header-authorized PUT/GET arms.
  This is additional signing behavior, not merely a wrapper around the
  existing URL; explicitly permit it and test it against the fixture store.
- **Keep the existing signer:** revise D2/Q2 to return the signed request as
  JSON and have the frontend fetch with its headers. Specify bucket CORS
  requirements for that browser request.
- **Use an authenticated ship proxy:** revise D2/Q2 to stream the object
  through a session-authorized ship route using the signed headers. Specify
  streaming and size bounds so log contents never enter persisted Gall state.

Making the bucket publicly readable would bypass the authorized-viewer
requirement and is not proposed.

## §3 — Where is the required object-store fixture?

**Read:** `BRIEF-CI-P2.md` §5 says the ship's `%storage` points at the harness
MinIO and names P0's `install-s3.sh`. The P1 live record's H15 observation
instead uses `http://127.0.0.1:1/ci-bucket/...`: it proves a signing result,
not an uploaded object returned by a running store.

**Tried:** Reading `.scratch/ci-p0/install-s3.sh` returned "No such file or
directory." A file-name search including ignored files under `.scratch`
found no `*s3*` or `*minio*` files. A content search for `MinIO|minio|install-s3|s3`
in `.scratch/ci-p0` and `.scratch/ci-p1` found no matches. No existing service
or other chair's environment was used to substitute for this fixture.

**Question:** Should the missing fixture be supplied, or built here?

- **Supply the intended P0 fixture:** use the supplied script and its
  documented endpoint, bucket, and lifecycle, adapting only the astra
  isolation settings before running Q2.
- **Authorize a new fixture in this worktree:** extend the existing harness
  with a MinIO setup script, isolated data and port allocation, a private
  bucket, `%storage` configuration, and verified shutdown. Include it in S1
  so Q2 can be proved before moving to S2; preserve the P0/P1 drivers.

Stopping here under the brief's §6. No push, merge, or rebase was performed.

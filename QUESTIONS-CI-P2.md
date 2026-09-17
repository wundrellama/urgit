# CI P2 questions — astra

Riders 1–3 resolve §1–§6. The answered sections are removed in their
requested stage commits. The brief body is re-frozen at `f7391cb`
(SHA-256 prefix `947595909726`), launch footer `c703034`.

Rider 4 resolves §7. Its historical text remains below until the requested
deletion in the S5 commit. The unchanged one-daemon retry passed 8/8 and
landed. **Only §8 is open.**

## §7 — ERPit's erasure workload fails before the S5/S6 eight-job acceptance

**Question:** how should the chair handle a failure in the read-only ERPit
workload while satisfying Q14 and Q18? I have stopped at S5; S4 is committed
as `f6555fc`. The S5 implementation is an uncommitted draft, not a completed
stage or a claimed GREEN row.

**Read:** BRIEF-CI-P2.md D6, Q14, Q18, the edit fence, and the launch footer;
`.scratch/ci-p1/p15.sh`; the P1 P15 record; the ERPit workflow's actual
erasure log. Q18 requires eight successful jobs and landing. The footer
requires cloning `/var/home/michael/workspace/urbit/erpit` as a read-only
source; changes to that project are outside the P2 fence. P1 recorded
eight green jobs at ERPit `d4f268e8…`; this chair's earlier S1 erasure
attempt also passed at `a6d15ed…` (Q2-missing subsequently removed its log).
This observation alone does not establish a deterministic ERPit regression.

**Tried:** for the S5 UI fixture, extended the existing P15 driver to accept
a fixture repository and checkpoint name. Cloned ERPit HEAD
`a6d15eddefa898250bf5f656441a55a80273f751` (1,067 commits; the source's sole
untracked `.scratch/grubbery/` was not copied), seeded `erpit-p2-web`,
CI-protected master, and pushed a README-only fixture commit
`8e0bbe3e2b6570896172537880719be3779b0e42`. Both existing signed daemons
ran at capacity 3. Candidate `0v2.j267u.99k0l.k7fin.smul4.8u9n8` planned
all eight expected jobs. No ERPit source files, sandbox, projection, or
scheduler were changed.

At 2026-09-17 04:22:12 UTC, `fixtures.yml/erasure` attempt
`0v3.o726k.08jf8.imcoa.90gc5.uacep` exited 1. Its own post-cadence audit
first reported correct accounting and unchanged log length, then its
bit-offset-aware final sweep found:

```text
HIT idx=4 file=/tmp/erasewes/.urb/log/0i139/image.bin offset=325078804 pat=fwd:1
HIT idx=7 file=/tmp/erasewes/.urb/log/0i139/image.bin offset=408810124 pat=fwd:0
HIT idx=4 file=/tmp/erasewes/.urb/log/0i158/image.bin offset=325078804 pat=fwd:1
HIT idx=7 file=/tmp/erasewes/.urb/log/0i158/image.bin offset=408810124 pat=fwd:0
HIT idx=4 file=/tmp/erasewes/.urb/chk/image.bin offset=325078804 pat=fwd:1
HIT idx=7 file=/tmp/erasewes/.urb/chk/image.bin offset=408810124 pat=fwd:0
erasure-test: RESIDUE — sentinel s5 still present post-cadence (hits=3); offsets above
erasure-test: FAIL — sentinel residue post-cadence (see offsets)
```

The controller correctly recorded **failed**, reason
`job erasure in fixtures.yml failed`, `landed=false`. Master stayed at the
seed `a6d15eddefa898250bf5f656441a55a80273f751`. A plain session-authenticated
`curl -L` through the log route fetched the private RustFS object:
**67,561 bytes**, SHA-256
`59937e7423b448aeadeb7c05d54866fd87b6c0343c8a3e685b204775a957eb80`, matching
the attempt's log metadata. This is a workflow-reported failure, not a
signature refusal, lost result, missing log, or successful landing.

Evidence retained in `.scratch/tmp/p2-web-erpit.log`,
`.scratch/tmp/p2-web-candidate-box-response.txt` (before stopping),
`.scratch/tmp/p2-erasure-box.jsonl`, and both runner logs. Before shutdown,
six job attempts had passed, erasure had failed, and suite was still
running. After capturing the failed verdict, both runners were stopped
through their `/proc`-verified PIDs; the unfinished suite was abandoned
and its sandbox destroyed. That later cancellation is not the cause of
the erasure failure. The two fixture ships and private RustFS remain up
for inspection, with both piers retained. No sabotage is applied.

**Under each answer:**

- **Retry the unchanged workload under the recorded P1 topology.** Run
  one daemon at capacity 3, retain this failed run, and require a fresh
  eight-job run to pass without editing or weakening the erasure test.
  If it fails again, leave the workload finding open. Recommended first
  diagnostic, since the same ERPit revision has passed erasure before.
- **Wait for an ERPit correction or a specified accepted revision.** Keep
  the source read-only here, clone the operator-named revision after it
  is available, and resume S5/Q14 followed by S6/Q17–Q18 against it.
- **Revise the acceptance requirement.** Record this failure propagation
  and non-landing honestly; do not label Q18 8/8 without an explicit
  replacement ruling. This would change the brief, not fix the workload.

The S5 draft already compiles on the ship and its new read routes return
200. Browser/Q14–Q16 acceptance has not run. The frontend suite is 131/132:
the remaining existing group-access source-slice test expects the helper
order from before the ratified S2 lift; correcting that test's arm bounds
is still ordinary S5 work. No S5 or S6 completion is claimed.

## §8 — act child containers bind the host's rootful Docker socket

**Question:** does Rider 4 authorize correcting this socket path in S5,
with a live RED→GREEN mount check? It is a distinct finding from `/tmp`.
The original brief limits runner changes to uploads, grants/scrubbing,
and signing, and says execution is unchanged. Rider 4 explicitly expands
the fence for a shared-`/tmp` fix; the inspection found no shared `/tmp`.
An asynchronous scope question was sent before changing runner code.

**Read:** `runner/internal/sandbox/docker.go` Prepare/Destroy;
`runner/internal/daemon/daemon.go`'s actual `act push` arguments;
`runner/internal/plan/plan.go`'s projection; pinned act 0.2.89's
`pkg/runner/run_context.go`, `GetBindsAndMounts` (127–184). The parent
runner correctly mounts the configured rootless socket into its own
`/var/run/docker.sock`. Stock act then defaults its *child mount source*
to `/var/run/docker.sock`, a host path resolved by the Docker daemon.
The daemon invocation does not set `--container-daemon-socket`.

**Measured:** the actual retry erasure job container for attempt
`0vseick.vfrio.3fong.495l5.nmvmm` has:

```text
Binds=["/var/run/docker.sock:/var/run/docker.sock"]
Mount Type=bind Source=/var/run/docker.sock Destination=/var/run/docker.sock
```

The host socket is mode 660, owner `root:docker`. A read-only host
`docker --host unix:///var/run/docker.sock info` reports
`SecurityOptions=[name=seccomp,profile=builtin name=cgroupns]`,
`DockerRootDir=/var/lib/docker`: it is the rootful daemon. In the job
container the socket is mode 660, owner `nobody:nogroup`. A read-only
connection attempt from that container's root user returns **permission
denied**. No access to the rootful daemon from the job is claimed; the
incorrect bind is independently established by Docker inspect.

The intended source is `/run/user/1000/ci-p2-astra/docker.sock`, whose
parent runner config is `docker_host =
"unix:///run/user/1000/ci-p2-astra/docker.sock"`. The actual mounts are
retained in `.scratch/tmp/p2-sandbox-retry-mounts.log`; Prepare/Destroy
lines and clean `/tmp` probes are in `.scratch/tmp/p2-sandbox-inspect.log`.

**Proposed bounded fix if authorized:** add act's
`--container-daemon-socket` argument using the configured rootless
endpoint, while retaining `DOCKER_HOST=unix:///var/run/docker.sock` for
act's own connection inside the parent runner. No sandbox interface,
projection, scheduler, or poll-loop change. Prove the existing wrong
mount RED, then prove the child mount source equals the configured
rootless socket and a child `docker info` reports `name=rootless` GREEN;
test an independent one-line sabotage that restores act's wrong default.

**If not authorized in S5:** retain this measured finding and stop
without changing execution behavior. Do not count file permissions as
compliance with the "never host rootful socket" boundary.

The Rider 4 ERPit retry was left intact to finish once. It passed **8/8**
in **1,109 seconds** and landed candidate
`0vdm40u.i3bb4.1cd5s.ivm60.4r5ml`, OID
`a866bea33e955befc03487250830ba9423518a0c`. All eight trusted logs were
fetched from the private bucket with matching sizes and SHA-256 hashes
(`.scratch/tmp/q18-retry.log`). The full driver transcript is
`.scratch/tmp/p2-web-retry.log`; the live table retains the earlier
failed run separately. No workload pin or ERPit edit was necessary.

Q14 and Q15 passed in Chromium, and frontend tests pass 132/132 after
the arm-bound correction. Q16 has not run RED→GREEN, and the S6/Q17
composed battery has not run. S5 remains an uncommitted draft. No runner
execution code has changed.

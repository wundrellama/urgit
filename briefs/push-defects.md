# Urgit: two `receive-pack` defects that break stock `git push`

## Status

Both were found by the `apply-delta` measurement run while trying to push a
real repository for an external timing. Both are recorded in `QUESTIONS.md` §8
with reproductions. Neither has been fixed.

They are unrelated to `apply-delta` and to `join-all`. Nothing on this branch
caused them; they reproduce on the base commit.

**Read `AGENTS.md` first.** `JOIN-ALL-PERFORMANCE.md` is the shape of report
this project expects.

## Defect 1 — a push updating more than one ref returns HTTP 400

### Root cause, confirmed at source

`parse-receive-command` (`desk/lib/git-protocol.hoon:176-189`):

```hoon
=/  cursor=@ud  82
|-
?:  (gte cursor p.payload)  ~
=/  byte=@ud  (byte-at:git-codec payload cursor)
?:  ?|  =(byte 0)
        =(byte 10)
    ==
  =/  length=@ud  (sub cursor 82)
  ...
$(cursor +(cursor))
```

A `receive-pack` command line is `<old-oid> SP <new-oid> SP <ref>`, so the ref
starts at byte 82. The loop walks forward from 82 looking for a NUL or a line
feed to terminate the ref, and **returns `~` if it reaches the end without
finding one**.

Git appends the capability list after a NUL on the **first** command line only.
Every subsequent command line is `<old> SP <new> SP <ref>` and simply ends. So
the second and later commands fall off the end, `parse-receive-command` returns
`~`, and `parse-receive-request:204` fails the entire request.

`handle-receive-pack` (`desk/app/urgit.hoon:8001`) answers 400.

### Reproduction

```
git -c protocol.version=0 -c http.postBuffer=524288000 \
    push ship '+refs/heads/a:refs/heads/a' '+refs/heads/b:refs/heads/b'
→ error: RPC failed; HTTP 400
```

The same two refs pushed one at a time both succeed. This also means
`git push --all`, `git push --tags`, and any push of a branch plus its tag are
broken.

### Decided direction

End-of-payload is a valid ref terminator. Treat reaching `p.payload` as the
terminator instead of a parse failure, keeping the NUL and LF cases as they are.

Do not special-case "first line versus later lines" — the terminator set is the
fix, and it is the same rule for every line.

## Defect 2 — a push larger than `http.postBuffer` returns HTTP 400

### Root cause, confirmed at source

When the request body exceeds `http.postBuffer` (1 MB by default), git does not
send the pack straight away. It sends a **probe request first** whose body is a
single flush packet — four bytes, `0000` — to check auth and readiness before
committing to a large upload.

`parse-receive-request` (`desk/lib/git-protocol.hoon:207-209`):

```hoon
    %flush
  ?:  =(0 (lent commands))  ~
  `[(flop commands) rest.u.next]
```

A flush with no preceding commands returns `~`, and `handle-receive-pack`
answers 400. Git aborts before sending anything.

### Reproduction

Any push of more than about 1 MB with default settings. Workaround today is
`-c http.postBuffer=524288000`.

**This is very likely the same root cause as the `protocol.version=2` push
returning HTTP 400** that `JOIN-ALL-PERFORMANCE.md` §6 recorded as unexplained.
Check whether fixing this resolves that too, and say either way.

### Decided direction

An empty command list from a flush-only body is a **valid probe**, not a bad
request. Answer it the way a Git server does: `200` with an empty
`report-status` body, so git proceeds to send the real request.

Do not simply make `parse-receive-request` return an empty command list to the
caller — `handle-receive-pack` would then run policy and ref-update logic over
zero commands. Distinguish the probe explicitly and answer it before any of
that.

Confirm the exact expected probe response against stock `git`'s behavior rather
than guessing at the status line — the test is whether real `git push` proceeds.

## Deliverables

### D1 — Reproduce both before changing anything

Boot your own pier, push into it with stock `git`, and capture the 400 for each
defect independently. **If either does not reproduce, stop and say so** — do not
fix a defect you could not first observe.

### D2 — Fix both

Two commits, one per defect, each with its reproduction in the message.

### D3 — Prove the fixes

- `git push --all` of a repository with **at least three branches and one
  annotated tag**, in one command, succeeding.
- A push of **more than 1 MB with default `http.postBuffer`** (i.e. without the
  workaround) succeeding.
- Clone the result back with stock `git` and confirm
  `git rev-list --objects --all | sort | sha256sum` matches the source
  repository, and `git fsck --full` is clean.
- Re-test the `protocol.version=2` push and report whether defect 2 explains it.
- The conformance vectors in `desk/gen/` must still pass, at minimum
  `git-pack-vector`, `git-pack-decode-vector`, `git-stock-pack-vector`,
  `git-codec-vector`.

### D4 — Regression-pin both

A single-ref push and a small push must both still work. State explicitly that
you tested them; these are the paths that work today and the ones a careless
fix would break.

### D5 — Verify the uncommitted-risk arm on this branch

`desk/lib/git-delta.hoon` was changed on this branch in `bf3dbea`
(`apply-delta` now collects chunks and makes one `join-all`). It was verified
byte-identical on a pier by the previous run, **but not re-verified in this
tree**. Your push tests exercise it on every deltified object.

Report explicitly whether the round trip in D3 came back byte-identical. If it
did not, that is a defect in `bf3dbea`, not in your fix — stop and report it
rather than working around it.

### D6 — Report

`PUSH-DEFECTS.md` at the repository root: root cause for each, the fix, the
reproduction before and after, D3 evidence, the v2 answer, and a section titled
**"What I could not measure and why."**

## Fences

- **An erpit test battery may be running on `~/piers/fakezod` and
  `~/piers/fakenec` (ports 8080/8081, tmux sessions `fakezod`/`fakenec`), and
  three piers `t4-opus-zod`, `t4-sol-zod`, `t4-fable-zod` are live on ports
  31411-31413. Do not touch any of them.**
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only processes you started, by a PID you recorded when you started them.
- Ports 8000, 8080-8084, 8090 are taken. 8093+ is free. `~/piers/joinall-bus`
  (port 8092) is a stopped pier from a previous run; reuse or ignore it.
- **This project ships nothing outside `desk/`.** No kernel edits.
- **Do not touch `desk/lib/git-codec.hoon` or `desk/lib/git-delta.hoon`.** Both
  carry landed performance work. D5 asks you to *observe* `git-delta`, not
  change it.
- Do not touch `desk/sur/git-peer.hoon` or the `%archive` and `%rate` paths in
  `desk/app/urgit.hoon` — a parallel task owns those.
- **Verify running source by scrying, never by reading the mount.** A detached
  mount discards edits while `%kiln-commit` still answers `%committed`. Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/git-protocol/hoon)` and
  grep for a token unique to your change, **immediately after your first
  install**.
- Git wire data is binary. Represent it as `octs`; never infer lengths with
  `met` after parsing (`AGENTS.md`).
- Commit on `feat/progress-and-joinall`. **Do not push.**
- `.claude/skills/urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
  now exists in the repository. Read it before writing any probe.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. If either
defect does not reproduce, if the correct probe response is genuinely ambiguous
against stock `git`, or if a fix would change the wire contract in a way this
brief does not authorize — write `QUESTIONS-PUSH.md` naming the problem, the
options, and what each one breaks. Then stop.

Do not report a number you did not measure.

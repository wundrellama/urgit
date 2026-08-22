# D1 harness — does `%prog` on a Mesa chum give a usable `%rate` gift

Six khan-eval threads. They answer the question in `briefs/archive-progress-tier3.md`
without touching `desk/` and without touching the kernel. Results are in
`ARCHIVE-PROGRESS.md` at the repository root.

Run each with:

```
<bin>/click -k -i <file> <pier>
```

## The ships

Two fake galaxies, booted from `pills/brass-408k-1.pill` on Vere 4.6:

| ship | pier | HTTP | Ames |
|---|---|---|---|
| `~med` | `/home/michael/piers/vm3` | 8100 | 31464 |
| `~pec` | `/home/michael/piers/vp3` | 8101 | 31589 |

`~med` holds `~pec` in its `chums` map as `%known`, so `find-peer ~pec` takes the
Mesa branch. The pair was built by the earlier run's recipe: `[%load %mesa]` to Ames
on each ship **before** first contact, then one `%helm-hi`. A pair that is already in
`peers` is never promoted, so the order matters.

**Use the Ames port Vere derives for the galaxy, not an arbitrary one.** With
`-p 31341` for `~med` and `-p 31342` for `~pec`, both ships bind and boot, `~med`
prints `push %peek on lanes=[~pec, ...]`, and nothing arrives: `~med` addresses
`~pec` at the derived port 31589, which nothing is listening on. The boot banner
says which port is derived (`ames: czar: overriding port 31589 with -p 31342`).
Booting on 31464/31589 makes the pair talk.

## The files

| file | ship | what it does |
|---|---|---|
| `chums.hoon` | either | prints the `chums` map, to confirm the pair |
| `grow-blob.hoon` | `~pec` | `%grow`s a large list into spider's scry namespace at `/probe/blob3` |
| `probe-chum-prog.hoon` | `~med` | `%chum` + `%prog [%chum ~] feq=1` in one event; logs every sign until a deadline |
| `probe-chum-only.hoon` | `~med` | `%chum` alone — the control that shows the `%rate` gift comes from `%prog` |
| `probe-keen-prog.hoon` | `~med` | the same test on the public `%keen`/`publ` namespace |
| `running-ames-lines.hoon` | either | reads `sys/vane/ames.hoon` out of the **running** ship and reports the line of each arm the report cites |

The probes send the peek card and the `%prog` card in one strand step, so Arvo
handles the peek first and the `pit` entry exists before `%prog` asks to attach to
it. Sending `%prog` first would find no entry and log `missing path for rate`.

The read path is `/g/x/1/spider//1/probe/blob3`. The empty knot is Gall's marker for
"this peek is for an agent, not for the vane" (`sys/vane/gall.hoon:2892`); the `1`
after it is the namespace version that `+scry:gall` strips next.

## Reading the output

`click` prints the jammed result. `%leaf` byte lists in a crash tang decode with:

```
python3 -c "import sys,re;t=sys.stdin.read();print(re.sub(r'%leaf ([0-9 ]+)',lambda m:chr(39)+''.join(chr(int(x)) for x in m.group(1).split() if int(x))+chr(39),t))"
```

# Writing click probe threads that actually parse

Measurement work on transfers is mostly `click -k -i <file>.hoon <pier>` probes.
The parser in that path rejects several constructs that are fine in a normal
Hoon file, and every rejection looks identical:

```
[0 %avow 1 %thread-fail %crash [%leaf 115 121 110 116 97 120 32 101 114 114 111 114 ...]
```

That byte soup decodes to `syntax error`. It tells you nothing about *where*.
Each rediscovery costs a full round trip to a ship, so the list below is worth
more than it looks.

Verified across a 2026-08-20/21 session that hit the first item four separate
times.

---

## The one that keeps biting: an inline gate inside turn / roll / levy

Not the `!,(*hoon …)` slap case. This is an ordinary `-k -i` thread file with no
slap anywhere in it. All three of these fail:

```hoon
=/  rows  (turn ~(tap by m) |=([a=@p b=*] [a b]))                 :: FAILS
=/  n     (roll rows |=([[* t=@tas] c=@ud] ?:(=(%x t) +(c) c)))   :: FAILS
=/  ok    (levy out |=([* m=? * *] m))                            :: FAILS
```

**Fix: do not transform collections inside the thread.** Scry, return the raw
noun, and post-process outside the ship:

```hoon
=/  m  (strand ,vase)
;<  our=@p  bind:m  get-our
;<  now=@da  bind:m  get-time
=/  all
  .^  (map @p [?(%peer %chum) ?(%alien %known)])
      %ax
      /(scot %p our)/$/(scot %da now)/chums/all
  ==
(pure:m !>(all))
```

Then parse the printed noun in Python. A probe that only scries and returns is
also much easier to debug when it *does* fail, because there is one thing in it.

If the result would be too large to print, write the fold as an explicit
`|-`/`?~` recursion over a `=/`-bound list rather than a `turn`. Expect that to
be finicky too — prefer narrowing the scry.

## Other constructs that produce the same opaque error

- **`=- -` / bare backstep placeholders.** Rewrite as a plain `=/` binding.
- **Non-ASCII in `::` comments.** Em dashes and arrows inside a probe file break
  the parse. Keep probe files pure ASCII, comments included.
- **Malformed `0v` literals.** Base32 needs dot-separated groups of exactly five
  characters after the first group. `0v6e.7aa11` is valid, `0v6e.77.0011` is not.

## Recovery procedure

When a probe fails to parse, do not stare at the tang — it blames the thread
machinery, never your line.

1. Cut the probe to a stub that returns a constant: `(pure:m !>(%ok))`. Confirm
   it runs.
2. Add the scry. Confirm.
3. Add the result expression last, one piece at a time.

Two-step isolation finds it in two round trips. Reading the error finds it in
none.

## Timing probes

`~>  %bout.[1 %label]` around an expression prints `label: took ms/N.NNN` to the
**pier's stdout log**, not to the click return value. Capture it with
`tmux capture-pane -p -t <session>` or by tailing the boot log. The click
response carries only what `pure:m` returns.

Units vary per call — `µs/`, `ms/`, `s/` — and the `s/` form uses dot separators
for thousands (`s/47.137.915` is 47.14 seconds, not 47 million). Parse
accordingly, and aggregate every call rather than eyeballing the last few lines;
a run where most calls are microseconds and 52 are ~0.9s each will look fast at
the tail and be slow in total.

**`%bout` hints nest.** An inner label is charged its own cost plus everything
its callers do around it. Before concluding "arm X is 98% of the cost", check the
deltas between enclosing labels. If `outer − inner` is near zero at every level,
the attribution is real. If the deltas are large, the inner number is inflated.
Both mistakes — trusting a nested number and dismissing a correct one — cost
about the same.

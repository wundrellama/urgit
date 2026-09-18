#!/bin/bash
# The ship's terminal: one tmux session per ship (the close-out footer:
# ships boot in tmux, never a herdr pane), named from the footer
# ($TTY, set in env.sh). Every driver reads and types through these and
# nothing else; the herdr pane calls of the P0/P1/P2 harness map onto them:
#   herdr pane run <id> '<line>'                  -> tty_send '<line>'
#   herdr pane send-keys <id> 'ctrl+a' 'ctrl+k'   -> tty_keys C-a C-k
#   herdr pane wait-output --lines 1 --regex R    -> tty_wait 'R' <seconds>
#   herdr pane read --source recent-unwrapped     -> tty_read <lines>
# tty_read returns LOGICAL lines: `capture-pane -J` joins the hard wraps
# a 200-column pane rarely makes anyway, trailing blank rows and trailing
# spaces are dropped, and every line is newline-terminated (a snapshot
# without one glued a row's next echo onto the prompt). The dojo's own
# pretty-printing over lines is kept as it is; the readers join it.
# A target is written `=NAME:` — the `=` makes the session name exact,
# so a sibling session with the same prefix can never be typed into.
tty_target() { printf '=%s:' "$TTY"; }
tty_alive() { tmux has-session -t "=$TTY" 2>/dev/null; }
# tty_read [lines]: the last N logical lines (default 40)
tty_read() {
  tmux capture-pane -p -J -S -2000 -t "$(tty_target)" 2>/dev/null \
    | sed 's/[[:space:]]*$//' \
    | awk -v lim="${1:-40}" '{ l[NR] = $0 } END { n = NR; while (n > 0 && l[n] == "") n--; i = (n > lim) ? n - lim + 1 : 1; for (; i <= n; i++) print l[i] }'
}
# tty_last: the last non-blank line
tty_last() { tty_read 1; }
# tty_wait '<regex>' [seconds]: until the last line matches (default 60);
# 1 on timeout
tty_wait() {
  local re="$1" n=$(( ${2:-60} * 4 )) i
  for ((i = 0; i < n; i++)); do
    tty_last | grep -qE -- "$re" && return 0
    sleep 0.25
  done
  return 1
}
# tty_send '<line>': type the line literally, then Enter
tty_send() { tmux send-keys -t "$(tty_target)" -l -- "$1" && tmux send-keys -t "$(tty_target)" Enter; }
# tty_keys <key...>: raw keys by tmux name (C-a C-k, C-d, y, Enter)
tty_keys() { tmux send-keys -t "$(tty_target)" "$@"; }

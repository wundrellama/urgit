#!/bin/bash
source "$(dirname "$0")/env.sh"
set -euo pipefail
case "${1:-}" in
  setup|actions-setup)
    python3 - "$1" <<'PY'
import os,pathlib,runpy,sys,types
t=types.SimpleNamespace(**runpy.run_path(str(pathlib.Path(os.environ['P1'])/'p2-trust.py')))
if sys.argv[1] == 'setup':
    state=t.create('q15-web')
    t.save('web-settings',state)
    print('Settings fixture:',state['repo'])
else:
    state=t.peer_pull('web-actions')
    t.merge(state)
    t.save('web-actions',state)
    print('Actions fixture:',state['repo'],state['cid'])
PY
    ;;
  install-browser)
    npm install --prefix "$TMP/browser" --no-save playwright@1.63.0
    node "$TMP/browser/node_modules/playwright/cli.js" install --only-shell chromium
    ;;
  q14|q15|q16|q18|actions)
    exec node "$P1/p2-web.mjs" "$@"
    ;;
  *) echo 'usage: p2-web.sh setup|actions-setup|install-browser|q14 <candidate>|q15|q16 red|green <candidate>|q18 <candidate>|actions' >&2; exit 2 ;;
esac

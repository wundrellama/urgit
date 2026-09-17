#!/bin/bash
# One-line P2 sabotages. Snapshot the current source so a stage can prove
# RED before its first commit without discarding its uncommitted work.
source "$(dirname "$0")/env.sh"
cd "$ROOT"
git rev-parse --is-inside-work-tree >/dev/null
python3 - "${1:-status}" "${2:-}" "$TMP/q-mutant.json" <<'PY'
import json, pathlib, sys
op, row, file = sys.argv[1:]
snapshot = pathlib.Path(file)
edits = {
 'Q12': ('runner/internal/signing/verify.go',
         'if !ed25519.Verify(pub, message, b) {',
         'if len(b) == 0 && !ed25519.Verify(pub, message, b) {',
         'Q12 RED: wrong-pub daemon started signed work'),
 'Q13': ('runner/internal/daemon/signatures.go',
         'return g.Expiry > now',
         'return true',
         'Q13 RED: expired signed grant reached act'),
 'Q10': ('desk/app/urgit-ci.hoon',
         '  ?&(=(%trusted trust) =(%job kind))\n',
         '  =(%job kind)\n',
         'Q10 RED: untrusted job received a credential grant'),
 'Q11': ('desk/app/urgit-ci.hoon',
         '  `[name.key scope.cred envs.cred created.cred]\n',
         '  `[value.cred scope.cred envs.cred created.cred]\n',
         'Q11 RED: credential value appeared in a read'),
 'Q5a': ('desk/app/urgit.hoon',
         '    ?:  p.protected\n',
         '    ?:  %.n\n',
         'Q5a RED: protected master moved before checks'),
 'Q5': ('desk/app/urgit-ci.hoon',
        '          ?:(u.writer %trusted %untrusted)  pull.act  now.bowl  now.bowl\n',
        '          %trusted  pull.act  now.bowl  now.bowl\n',
        'Q5 RED: non-writer PR offered trusted work'),
 'Q6': ('desk/app/urgit-ci.hoon',
        '      =(%trusted trust.c)\n',
        '      %.y\n',
        'Q6 RED: untrusted candidate landed'),
 'Q8': ('desk/app/urgit-ci.hoon',
        '  =/  refusal=(unit @t)  (writer-refusal repo.u.found actor)\n',
        '  =/  refusal=(unit @t)  ~\n',
        'Q8 RED: non-writer approval accepted by the action handler'),
 'Q4': ('desk/app/urgit-ci.hoon',
        '  ?.  ?&(?=(^ name) (upload-name u.name))\n',
        '  ?.  ?=(^ name)\n',
        'upload ../x -> 200'),
 'Q3': ('desk/lib/ci-storage.hoon',
        '  ?.  =(requester trust)  ~\n',
        '  ?.  %.y  ~\n',
        'cross-trust read refused: FAIL'),
 'Q2-missing': ('desk/app/urgit-ci.hoon',
        '    =.  attempts  (~(put by attempts) id u.found(log ~))\n',
        '    =.  attempts  (~(put by attempts) id u.found)\n',
        'missing object cleared: FAIL'),
}
if op == 'tripwire':
    print(edits[row][3])
elif op == 'status':
    print('MUTATED '+json.loads(snapshot.read_text())['row'] if snapshot.exists() else 'real build')
elif op == 'apply':
    if snapshot.exists(): raise SystemExit('a mutant is already applied; revert it first')
    path, old, new, _ = edits[row]
    p = pathlib.Path(path); original = p.read_text()
    # Q3 targets sign-get, leaving the independent viewer guard in place.
    expected = 2 if row == 'Q3' else 1
    if original.count(old) != expected: raise SystemExit(f'{row}: source anchor changed; nothing written')
    changed = original.replace(old, new, 1)
    snapshot.write_text(json.dumps({'row': row, 'path': path, 'original': original, 'changed': changed}))
    p.write_text(changed)
    print('applied '+row)
elif op == 'revert':
    if snapshot.exists():
        saved = json.loads(snapshot.read_text()); p = pathlib.Path(saved['path'])
        if p.read_text() not in (saved['original'], saved['changed']):
            raise SystemExit('source changed while mutant was applied; refusing to overwrite it')
        p.write_text(saved['original']); snapshot.unlink()
        print('reverted '+saved['row'])
else: raise SystemExit('usage: q-mutants.sh apply <row>|revert|status|tripwire <row>')
PY

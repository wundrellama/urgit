"""S3 live rows; a loopback pass-through captures actual assignment replies."""
import datetime
import gzip
import http.client
import http.server
import json
import os
import pathlib
import re
import runpy
import secrets
import subprocess
import sys
import threading
import time
import types

t = types.SimpleNamespace(**runpy.run_path(str(pathlib.Path(__file__).with_name('p2-trust.py'))))
ROOT, TMP, P1, P0 = t.ROOT, t.TMP, t.P1, t.P0
ROW, *ARGS = sys.argv[1:]


class Capture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def forward(self):
        conn = http.client.HTTPConnection('127.0.0.1', int(os.environ['PORT']), timeout=90)
        try:
            body = self.rfile.read(int(self.headers.get('content-length', '0')))
            headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection')}
            headers['Host'] = '127.0.0.1:' + os.environ['PORT']
            conn.request(self.command, self.path, body=body, headers=headers)
            response = conn.getresponse()
            data = response.read()
            if response.status == 200 and self.path.endswith('/assignment'):
                decoded = gzip.decompress(data) if response.getheader('Content-Encoding') == 'gzip' else data
                parsed = json.loads(decoded)
                a = parsed.get('assignment')
                if a:
                    t.save('grant-offer-' + a['attempt'], a)
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in ('connection', 'transfer-encoding', 'content-length'):
                    self.send_header(key, value)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (OSError, http.client.HTTPException):
            # A daemon stopping its poll closes the downstream socket.
            pass
        finally:
            conn.close()

    do_GET = do_POST = do_HEAD = forward


def runner(cmd):
    subprocess.run([str(P1 / 'runner.sh'), cmd, 'a'], check=True)


def start(fresh=False):
    if fresh:
        file = TMP / 'runner/a/state.json'
        known = False
        if file.exists():
            daemon = json.loads(file.read_text())['daemon_id']
            absent = t.dojo(f'=(~ .^((unit daemon:ci) %gx /=urgit-ci=/daemon/{daemon}/noun))')
            assert absent in ('%.y', '%.n'), 'daemon lookup failed'
            known = absent == '%.n'
        if not known:
            t.start_runner()
    runner('stop')
    config = TMP / 'runner/a/config.toml'
    data = config.read_text()
    data = re.sub(r'^ship_url = .*$', 'ship_url = "http://127.0.0.1:' + str(server.server_port) + '"', data, flags=re.M)
    config.write_text(data)
    config.chmod(0o600)
    runner('start')


def values(state):
    return [state['secret'], state['production'], state['testing']]


def set_credentials(state):
    state.update(secret='p2-ci-' + secrets.token_hex(16), production='p2-prod-' + secrets.token_hex(16), testing='p2-test-' + secrets.token_hex(16))
    repo = state['repo']
    t.poke(f"[%set-credential '{repo}' 'CI_TOKEN' '{state['secret']}' %job ~]")
    t.poke(f"[%set-credential '{repo}' 'PROD_TOKEN' '{state['production']}' %env (silt ~['production'])]")
    t.poke(f"[%set-credential '{repo}' 'TEST_ONLY' '{state['testing']}' %env (silt ~['testing'])]")


WORKFLOW = '''name: credential-fixture
on: push
jobs:
  pass:
    runs-on: ubuntu-latest
    environment: production
    env:
      NAME: ${{ secrets.CI_TOKEN }}
      PROD: ${{ secrets.PROD_TOKEN }}
    steps:
      - id: reveal
        run: |
          echo "$NAME"
          echo "$PROD"
          echo "token=$NAME" >> "$GITHUB_OUTPUT"
          sleep 12
'''


def writer_fixture():
    state = t.create('q9')
    clone = pathlib.Path(state['clone'])
    def git(*args):
        return t.run(['git', '-C', clone, *args])
    git('checkout', '-qb', 'credential-topic')
    (clone / '.github/workflows/fixture-pass.yml').write_text(WORKFLOW)
    git('add', '-A'); git('commit', '-qm', 'credential fixture')
    state['head'] = git('rev-parse', 'HEAD')
    git('push', '-q', 'origin', 'credential-topic')
    state['pull'] = t.ok('POST', '/repository/' + state['repo'] + '/pulls', {
        'title': 'credential fixture', 'sourceBranch': 'refs/heads/credential-topic', 'targetBranch': 'refs/heads/master'
    }, status=201)['number']
    return state


def offer(cid, after=None):
    def find():
        for path in TMP.glob('p2-grant-offer-*.json'):
            a = json.loads(path.read_text())
            if a['candidate'] == cid and a['kind'] == 'job' and a['attempt'] not in (after if isinstance(after, set) else {after}):
                return a
    return t.wait_until(find, 180)


def metadata(repo):
    return t.dojo(f'.^((list credential-info:ci) %gx /=urgit-ci=/credentials/{repo}/noun)')


def no_secret(state, text, label):
    assert all(value not in text for value in values(state)), 'credential value leaked to ' + label


def q9():
    state = t.load('q9') if ARGS == ['resume'] else writer_fixture()
    if ARGS != ['resume']:
        set_credentials(state)
    t.save('q9', state)
    # An actual refused owner poke. Capture Khan's nack directly rather
    # than relying on an asynchronous Gall message in the dojo pane.
    negative = TMP / 'q9-newline.hoon'
    negative.touch(mode=0o600, exist_ok=True); negative.chmod(0o600)
    negative.write_text("=/  m  (strand ,vase)\n;<  our=@p  bind:m  get-our\n;<  ~  bind:m  (poke [our %urgit-ci] %ci-action !>([%set-credential '" + state['repo'] + "' 'MULTILINE' (rap 3 ~['first-line' 10 'second-line']) %job ~]))\n(pure:m !>(%accepted))\n")
    result = subprocess.run([os.environ['CLICK'], '-k', '-i', str(negative), os.environ['PIER']], capture_output=True, text=True, timeout=120)
    refusal = result.stdout + result.stderr
    assert result.returncode != 0 or '%accepted' not in refusal, 'newline credential was accepted'
    leaves = re.findall(r'\[%leaf ((?:[0-9]+ )*)0\]', refusal)
    decoded = ' '.join(''.join(chr(int(n)) for n in leaf.split()) for leaf in leaves)
    assert 'credential values must be a single line' in decoded, 'newline refusal did not name the rule'
    info = metadata(state['repo'])
    assert 'MULTILINE' not in info and 'CI_TOKEN' in info
    no_secret(state, info, 'credential metadata')
    print('Q9: newline refused: credential values must be a single line', flush=True)
    t.save('q9', state)
    start(fresh=True)
    t.merge(state)
    t.save('q9', state)
    a = offer(state['cid'])
    assert {g['name'] for g in a['grants']} == {'CI_TOKEN', 'PROD_TOKEN'}, 'job/environment grant scope mismatch'
    assert {g['value'] for g in a['grants']} == {state['secret'], state['production']}
    assert all(time.time() < g['expiry'] <= time.time() + 901 for g in a['grants'])
    # Bypass the daemon scrub deliberately. The ship must mask set-output
    # itself; the running fixture's sleep holds this event window open.
    code, _ = t.api('POST', '/ci/attempt/' + a['attempt'] + '/event', {
        'job': a['attempt'] + '/credential-fixture/pass', 'jobID': 'pass',
        'time': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'command': 'set-output', 'name': 'ship-scrub', 'arg': state['secret'], 'msg': state['secret']
    })
    assert code == 202, 'raw ship scrub probe was not accepted'
    outputs = t.dojo(f'outputs:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/{a["attempt"]}/noun))')
    no_secret(state, outputs, 'recorded output')
    assert 'ship-scrub' in outputs and '***' in outputs
    t.finished(state, True)
    log = (TMP / 'runner/a/work' / (a['attempt'] + '.act.jsonl')).read_text()
    no_secret(state, log, 'daemon jsonl')
    no_secret(state, (TMP / 'runner/a/daemon.log').read_text(), 'daemon diagnostics')
    assert '***' in log
    recorded = [json.loads(line) for line in log.splitlines() if line.startswith('{')]
    assert any(e.get('command') == 'set-output' and e.get('arg') == '***' for e in recorded)
    state['attempt'] = a['attempt']
    t.save('q9', state)
    print('Q9 PASS: real trusted job has job and matching environment grants; local log and ship outputs masked', flush=True)


def q10(phase):
    t.mutant('Q10', phase)
    state = t.peer_pull('q10-' + phase)
    set_credentials(state)
    t.poke(f"[%set-untrusted-policy '{state['repo']}' %restricted]")
    start()
    t.merge(state)
    a = offer(state['cid'])
    assert a['trust'] == 'untrusted'
    if phase == 'red':
        assert any(g['value'] == state['secret'] for g in a['grants']), 'Q10 was not RED'
        print('Q10 RED: untrusted job received a credential grant', flush=True)
    else:
        assert a['grants'] == [], 'untrusted grant list was not empty'
        t.finished(state, False)
        print('Q10 GREEN: credentials stored; real untrusted job assignment grants=[]', flush=True)
    t.save('q10-' + phase, state)


read_probe = None


def prepare_read_probe():
    global read_probe
    source = """|=  [value=* needle=@]
^-  ?
=/  size=@ud  (met 3 needle)
?>  (gth size 0)
=/  atom-has
  |=  atom=@
  ^-  ?
  |-
  ?:  =(0 atom)  %.n
  ?:  =(needle (end [3 size] atom))  %.y
  $(atom (rsh [3 1] atom))
=/  seek
  |=  node=*
  ^-  ?
  ?@  node  (atom-has node)
  |($(node -.node) $(node +.node))
(seek value)
"""
    path = pathlib.Path(os.environ['PIER']) / 'urgit/gen/p2-credential-read.hoon'
    path.write_text(source)
    t.run([P0 / 'dojo.sh', '|commit %urgit', '120', '4'])
    read_probe = 'q11-read-' + str(time.time_ns())
    t.run([P0 / 'dojo.sh', '=' + read_probe + ' -build-file /=urgit=/gen/p2-credential-read/hoon', '120', '8'])


def secret_in_scry(path, secret):
    raw = format(int.from_bytes(secret.encode(), 'little'), 'x')
    needle = '0x' + '.'.join(reversed([raw[max(0, i-4):i] for i in range(len(raw), 0, -4)]))
    found = t.dojo(f'({read_probe} .^(* %gx /=urgit-ci=/{path}/noun) {needle})')
    assert found in ('%.y', '%.n'), 'credential read probe did not return a boolean'
    return found == '%.y'


def q11(phase):
    t.mutant('Q11', phase)
    prepare_read_probe()
    state = t.load('q9')
    info = metadata(state['repo'])
    if phase == 'green' and info == '~':
        for name, value, scope, envs in [('CI_TOKEN', state['secret'], '%job', '~'), ('PROD_TOKEN', state['production'], '%env', "(silt ~['production'])"), ('TEST_ONLY', state['testing'], '%env', "(silt ~['testing'])")]:
            t.poke(f"[%set-credential '{state['repo']}' '{name}' '{value}' {scope} {envs}]")
        info = metadata(state['repo'])
    if phase == 'red':
        assert state['secret'] in info, 'Q11 was not RED'
        assert secret_in_scry('credentials/' + state['repo'], state['secret']), 'complete-noun probe missed RED'
        print('Q11 RED: credential value appeared in a read', flush=True)
        return
    reads = {'credentials': info}
    # Ask for a boolean over the complete noun, so terminal-tail limits
    # cannot turn a truncated record into a false GREEN. The needle is an
    # atom literal; fixture credential text is never printed by the probe.
    paths = ['candidates', 'attempts', 'assignments', 'daemons', 'polls', 'state/version',
             'candidate/' + state['cid'], 'attempt/' + state['attempt'],
             'landing/' + state['cid'], 'untrusted-policy/' + state['repo'],
             'credentials/' + state['repo']]
    daemon = json.loads((TMP / 'runner/a/state.json').read_text())['daemon_id']
    paths += ['daemon/' + daemon,
              'ci-protected/' + state['repo'] + "/(scot %t 'refs/heads/master')",
              'eligible/' + state['repo'] + "/(scot %t 'refs/heads/master')/(scot %t '" + state['head'] + "')",
              'sign-get/' + state['attempt'] + "/trusted/(scot %t 'log.jsonl')",
              'sign-get/' + state['attempt'] + "/untrusted/(scot %t 'log.jsonl')"]
    for path in paths:
        for secret in values(state):
            assert not secret_in_scry(path, secret), 'credential value appeared in ' + path
    reads['attempt-json'] = json.dumps(t.ok('GET', '/ci/attempt/' + state['attempt']))
    reads['repository-json'] = json.dumps(t.repository(state['repo']))
    for label, body in reads.items():
        no_secret(state, body, label)
    for name in ['CI_TOKEN', 'PROD_TOKEN', 'TEST_ONLY']:
        t.poke(f"[%delete-credential '{state['repo']}' '{name}']")
    assert metadata(state['repo']) == '~'
    start()
    daemon = json.loads((TMP / 'runner/a/state.json').read_text())['daemon_id']
    previous = {json.loads(p.read_text())['attempt'] for p in TMP.glob('p2-grant-offer-*.json')}
    t.poke(f"[%assign {state['cid']} {daemon} %job `'fixture-pass.yml' `'pass' ~]")
    a = offer(state['cid'], previous)
    assert a['grants'] == []
    t.wait_until(lambda: t.dojo(f'status:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/{a["attempt"]}/noun))') == '%passed')
    t.save('q11', {'repo': state['repo'], 'cid': state['cid'], 'attempt': a['attempt'], 'readPaths': paths})
    print('Q11 GREEN: credential absent from public scries/JSON; deletion leaves a real rerun grants=[]', flush=True)


server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Capture)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    if ROW == 'q9':
        q9()
    elif ROW == 'q10':
        q10(ARGS[0])
    elif ROW == 'q11':
        q11(ARGS[0])
    else:
        raise SystemExit('usage: p2-credentials.sh q9 | q10 red|green | q11 red|green')
finally:
    runner('stop')
    server.shutdown()
    if ARGS and ARGS[0] == 'red':
        subprocess.run([str(P1 / 'q-mutants.sh'), 'revert'], check=True)

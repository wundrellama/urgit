"""S2 live drivers. Assertions read the ship, real peer protocol and real daemon."""
import json
import os
import pathlib
import re
import subprocess
import sys
import time

ROOT = pathlib.Path(os.environ['ROOT'])
TMP = pathlib.Path(os.environ['TMP'])
P0 = ROOT / '.scratch/ci-p0'
P1 = ROOT / '.scratch/ci-p1'
URL = os.environ['URL']
SHIP = os.environ['SHIP']
PEER = os.environ['SHIP2']
ROW, *ARGS = sys.argv[1:]


def run(argv, **kwargs):
    return subprocess.check_output([str(x) for x in argv], text=True, **kwargs).strip()


def api(method, path, body=None, bearer=None):
    args = [P0 / 'api.sh', method, path, '' if body is None else json.dumps(body)]
    if bearer is not None:
        args.append(bearer)
    out = run(args, timeout=40)
    status, _, data = out.partition(' ')
    return int(status), json.loads(data) if data else None


def ok(method, path, body=None, status=200):
    got, data = api(method, path, body)
    assert got == status, (got, data)
    return data


def dojo(expr):
    return run(['bash', '-c', 'source "$1"; dojo_value "$2"', 'row', P1 / 'lib.sh', expr], timeout=180)


def poke(noun):
    text = run([P0 / 'poke.sh', 'urgit-ci', 'ci-action', noun], timeout=120)
    assert '%poked %urgit-ci %ci-action' in text, text
    return text


def field(cid, name):
    return dojo(f'{name}:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/{cid}/noun))')


def wait_until(test, seconds=180):
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        value = test()
        if value:
            return value
        time.sleep(1)
    raise AssertionError('timed out waiting for ' + test.__name__)


def save(name, value):
    file = TMP / ('p2-' + name + '.json')
    file.touch(mode=0o600, exist_ok=True)
    file.chmod(0o600)
    file.write_text(json.dumps(value, indent=2) + '\n')


def load(name):
    return json.loads((TMP / ('p2-' + name + '.json')).read_text())


def repository(repo):
    return ok('GET', '/repository/' + repo)


def ref(repo, name='refs/heads/master'):
    return next(r['oid'] for r in repository(repo)['refs'] if r['name'] == name)


def pull(repo, number):
    return next(p for p in repository(repo)['pullRequests'] if p['number'] == number)


def create(label):
    repo = label + '-' + str(time.time_ns())[-12:]
    ok('POST', '/repositories', {'name': repo, 'publicRead': True}, status=201)
    clone = TMP / ('clone-' + repo)
    clone.mkdir()
    def git(*args):
        return run(['git', '-C', clone, *args])
    git('init', '-q', '-b', 'master')
    git('config', 'user.name', 'p2'); git('config', 'user.email', 'p2@example')
    git('config', 'http.cookieFile', os.environ['JAR'])
    git('remote', 'add', 'origin', URL + '/git/' + repo)
    workflow = clone / '.github/workflows/fixture-pass.yml'
    workflow.parent.mkdir(parents=True)
    workflow.write_bytes((ROOT / 'desk/tests/ci/fixture-pass.yml').read_bytes())
    git('add', '-A'); git('commit', '-qm', 'p2 trust fixture seed')
    base = git('rev-parse', 'HEAD'); git('push', '-q', 'origin', 'master')
    # A fresh repository defaults to main; the peer transfer requires its
    # advertised default branch to exist. These fixtures explicitly use master.
    answer = run([P0 / 'poke.sh', 'urgit', 'git-action', f"[%set-head '{repo}' 'refs/heads/master']"], timeout=120)
    assert '%poked %urgit %git-action' in answer, answer
    poke(f"[%set-ci-protected '{repo}' 'refs/heads/master' %.y]")
    return {'repo': repo, 'base': base, 'clone': str(clone)}


def writer_pull(label):
    state = create(label)
    clone, repo = pathlib.Path(state['clone']), state['repo']
    def git(*args):
        return run(['git', '-C', clone, *args])
    git('checkout', '-qb', 'topic')
    (clone / 'change.txt').write_text('writer change\n')
    git('add', 'change.txt'); git('commit', '-qm', 'writer PR')
    state['head'] = git('rev-parse', 'HEAD')
    git('push', '-q', 'origin', 'topic')
    state['pull'] = ok('POST', f'/repository/{repo}/pulls', {
        'title': 'writer PR', 'sourceBranch': 'refs/heads/topic', 'targetBranch': 'refs/heads/master'
    }, status=201)['number']
    return state


def peer_pull(label):
    state = create(label)
    fork = state['repo'] + '-fork'
    subprocess.run([str(P1 / 'peer.sh'), SHIP, state['repo'], fork], env=dict(os.environ, CI_PEER='1'), check=True)
    state.update(load('peer-' + fork))
    return state


def merge(state, expected=202):
    code, data = api('POST', f"/repository/{state['repo']}/pulls/{state['pull']}/merge", {})
    print('web merge ->', code, data, flush=True)
    assert code == expected, (code, data)
    if code == 202:
        state['cid'] = data['candidate']
        wait_until(lambda: field(state['cid'], 'status').startswith('%'))
    return data


def start_runner():
    subprocess.run([str(P1 / 'runner.sh'), 'stop', 'a'], check=True)
    runner = TMP / 'runner/a'
    if runner.exists():
        runner.rename(TMP / ('runner-a-before-' + str(time.time_ns())))
    token = run([P1 / 'mint.sh'])
    subprocess.run([str(P1 / 'runner.sh'), 'start', 'a', '1', token], check=True)
    wait_until(lambda: (runner / 'state.json').exists())


def finished(state, lands):
    cid, repo = state['cid'], state['repo']
    wait_until(lambda: field(cid, 'status') in ('%passed', '%failed', '%unknown'), 240)
    assert field(cid, 'status') == '%passed', (field(cid, 'status'), field(cid, 'verdict-reason'))
    expected = 'landed' if lands else 'trust is untrusted'
    wait_until(lambda: expected in field(cid, 'verdict-reason'))
    assert ref(repo) == (state['head'] if lands else state['base']), repository(repo)
    assert pull(repo, state['pull'])['state'] == ('merged' if lands else 'open')
    print(cid, 'passed;', field(cid, 'verdict-reason'), 'master', ref(repo), flush=True)


def mutant(row, phase, agent='urgit-ci'):
    subprocess.run([str(P1 / 'q-mutants.sh'), 'apply', row] if phase == 'red' else [str(P1 / 'q-mutants.sh'), 'revert'], check=True)
    subprocess.run([str(P1 / 'p2-reload.sh'), agent], check=True)


if ROW == 'q5a':
    phase = ARGS[0]
    mutant('Q5a', phase, 'urgit')
    try:
        state = writer_pull('q5a-' + phase)
        merge(state, 200 if phase == 'red' else 202)
        if phase == 'red':
            assert ref(state['repo']) == state['head']
            assert pull(state['repo'], state['pull'])['state'] == 'merged'
            print('Q5a RED: protected master moved before checks', flush=True)
        else:
            assert ref(state['repo']) == state['base']
            assert field(state['cid'], 'trust') == '%trusted'
            assert pull(state['repo'], state['pull'])['state'] == 'open'
            start_runner(); finished(state, True)
            clone = pathlib.Path(state['clone'])
            run(['git', '-C', clone, 'push', '-q', 'origin', state['base'] + ':refs/heads/free'])
            free_pull = ok('POST', f"/repository/{state['repo']}/pulls", {'title': 'unprotected PR', 'sourceBranch': 'refs/heads/topic', 'targetBranch': 'refs/heads/free'}, status=201)['number']
            ok('POST', f"/repository/{state['repo']}/pulls/{free_pull}/merge", {})
            assert ref(state['repo'], 'refs/heads/free') == state['head']
            print('Q5a GREEN: protected merge staged, trusted candidate landed, pull merged; unprotected merge writes directly', flush=True)
        save('q5a-' + phase, state)
    finally:
        if phase == 'red': subprocess.run([str(P1 / 'q-mutants.sh'), 'revert'], check=True)
elif ROW == 'q5-setup':
    subprocess.run([str(P1 / 'runner.sh'), 'stop', 'a'], check=True)
    subprocess.run([str(P1 / 'nuke-revive.sh'), 'q5'], check=True)
    token = run([P1 / 'mint.sh'])
    code, data = api('POST', '/ci/daemon/enroll', {'token': token, 'capacity': 1, 'sandbox': 'q5-offer-probe'}, '-')
    assert code == 200, ('enroll status', code)
    save('q5-daemon', data)
elif ROW == 'q5':
    phase = ARGS[0]
    mutant('Q5', phase)
    try:
        state = peer_pull('q5-' + phase)
        merge(state)
        cid = state['cid']
        assert field(cid, 'actor') == '~' + PEER
        assert ref(state['repo']) == state['base']
        daemon = load('q5-daemon')
        code, assignment = api('GET', '/ci/daemon/' + daemon['daemon-id'] + '/assignment', bearer=daemon['bearer'])
        print('assignment poll ->', code, flush=True)
        if phase == 'red':
            assert field(cid, 'trust') == '%trusted'
            assert code == 200 and assignment['assignment']['candidate'] == cid, assignment
            print('Q5 RED: non-writer PR offered trusted work', flush=True)
            aid = assignment['assignment']['attempt']
            code, result = api('POST', '/ci/attempt/' + aid + '/abandon', {'reason': 'Q5 RED probe complete'}, daemon['bearer'])
            assert code == 200, (code, result)
        else:
            assert field(cid, 'trust') == '%untrusted'
            assert field(cid, 'status') == '%pending'
            assert field(cid, 'attempts') == '~' and field(cid, 'plan') == '~'
            assert code == 204, assignment
            print('Q5 GREEN: real peer actor, untrusted pending, no plan, no attempts, no offered work', flush=True)
        save('q5-' + phase, state)
    finally:
        if phase == 'red': subprocess.run([str(P1 / 'q-mutants.sh'), 'revert'], check=True)
elif ROW == 'q6-setup':
    # P1 ignores a zero capacity header. Retire the Q5 capture-only daemon
    # by waiting out its existing five-minute liveness window.
    print('Waiting five minutes for the Q5 probe enrollment to become stale', flush=True)
    time.sleep(301)
    start_runner()
    subprocess.run([str(P1 / 'runner.sh'), 'stop', 'a'], check=True)
elif ROW == 'q6':
    phase = ARGS[0]
    subprocess.run([str(P1 / 'runner.sh'), 'stop', 'a'], check=True)
    mutant('Q6', phase)
    try:
        state = peer_pull('q6-red') if phase == 'red' else load('q5-green')
        if phase == 'red': merge(state)
        assert field(state['cid'], 'trust') == '%untrusted'
        poke(f"[%set-untrusted-policy '{state['repo']}' %restricted]")
        daemon = json.loads((TMP / 'runner/a/state.json').read_text())['daemon_id']
        code, data = api('GET', '/ci/daemon/' + daemon + '/assignment')
        assert code == 200, code
        offer = data['assignment']
        assert offer['candidate'] == state['cid'] and offer['trust'] == 'untrusted' and offer['grants'] == [], offer
        save('q6-' + phase + '-offer', data)
        print('Captured real assignment: trust=untrusted grants=[]; starting daemon for its two-minute redelivery', flush=True)
        subprocess.run([str(P1 / 'runner.sh'), 'start', 'a'], check=True)
        finished(state, phase == 'red')
        attempts = re.findall(r'0v[0-9a-v.]+', field(state['cid'], 'attempts'))
        assert attempts
        for aid in attempts:
            assert dojo(f'trust:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/{aid}/noun))') == '%untrusted'
        assert '--cache-server-path /work/cache/untrusted' in (TMP / 'runner/a/daemon.log').read_text()
        print('Q6 RED: untrusted candidate landed' if phase == 'red' else 'Q6 GREEN: untrusted attempts passed; landing refused naming trust', flush=True)
        save('q6-' + phase, state)
    finally:
        if phase == 'red': subprocess.run([str(P1 / 'q-mutants.sh'), 'revert'], check=True)
elif ROW == 'q7':
    state = load('q5-green'); old = state['cid']
    if field(old, 'status') != '%skipped':
        poke('[%approve-candidate ' + old + ']')
    assert field(old, 'status') == '%skipped'
    assert 'superseded by approval' in field(old, 'verdict-reason')
    expr = "=/  cs=(map candidate-id:ci candidate:ci)  .^((map candidate-id:ci candidate:ci) %gx /=urgit-ci=/candidates/noun)  (murn ~(tap by cs) |=([id=candidate-id:ci c=candidate:ci] ?:(?&(=('" + state['repo'] + "' repo.c) =(%trusted trust.c)) `id ~)))"
    ids = re.findall(r'0v[0-9a-v.]+', dojo(expr))
    assert len(ids) == 1 and ids[0] != old, ids
    state['cid'] = ids[0]
    assert field(old, 'head') == field(state['cid'], 'head')
    assert field(old, 'base') == field(state['cid'], 'base')
    finished(state, True)
    save('q7', state)
    print('Q7 GREEN: new trusted candidate, same head/base, old skipped; owner approval accepted and landed', flush=True)
else:
    raise SystemExit('usage: p2-trust.sh q5a red|green | q5-setup | q5 red|green | q6-setup | q6 red|green | q7')

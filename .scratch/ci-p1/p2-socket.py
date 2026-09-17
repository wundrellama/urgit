"""Q19's observations come from the live job, not from source arguments."""
import json
import os
import pathlib
import runpy
import subprocess
import sys
import time
import types

t = types.SimpleNamespace(**runpy.run_path(str(pathlib.Path(os.environ['P1']) / 'p2-trust.py')))
phase = sys.argv[1]
assert phase in ('red', 'green')
docker = ['docker', '--host', 'unix://' + os.environ['DOCKER_SOCK']]
state = t.create('q19-' + phase)
clone = pathlib.Path(state['clone'])
(clone / '.github/workflows/fixture-pass.yml').unlink()
(clone / '.github/workflows/socket.yml').write_text('''name: socket boundary
on: push
jobs:
  socket:
    runs-on: ubuntu-latest
    steps:
      - name: inspect socket boundary
        run: |
          for i in $(seq 1 120); do
            test ! -e /tmp/q19-inspected || break
            sleep 1
          done
          test -e /tmp/q19-inspected
          docker --host unix:///var/run/docker.sock info --format '{{json .SecurityOptions}}'
''')
t.run(['git', '-C', clone, 'add', '-A'])
t.run(['git', '-C', clone, 'commit', '-qm', 'Q19 socket observation'])
state['head'] = t.run(['git', '-C', clone, 'rev-parse', 'HEAD'])
t.run([t.P1 / 'runner.sh', 'start', 'a'])
push = subprocess.run(['git', '-C', str(clone), 'push', '-q', 'origin', 'master'], text=True, capture_output=True)
assert push.returncode == 1 and 'staged as ci candidate' in push.stderr, push.stderr
candidate_path = '/ci/repository/' + state['repo'] + '/candidates'
candidate = t.wait_until(lambda: next(iter(t.ok('GET', candidate_path)['candidates']), None))
cid = state['cid'] = candidate['id']
print('Q19', phase, 'candidate', cid, flush=True)

def child():
    candidate = t.ok('GET', '/ci/candidate/' + cid)
    for attempt in candidate['attempts']:
        if attempt['kind'] != 'job':
            continue
        ids = t.run(docker + ['ps', '-q', '--filter', 'network=ci-' + attempt['attempt']]).split()
        for container in ids:
            data = json.loads(t.run(docker + ['inspect', container]))[0]
            if data['Name'].startswith('/act-'):
                return attempt, container, data
    return None

attempt, container, data = t.wait_until(child, 240)
mount = next(m for m in data['Mounts'] if m['Destination'] == '/var/run/docker.sock')
source = mount['Source']
print('Job', attempt['attempt'], data['Name'], flush=True)
print('Binds=' + json.dumps(data['HostConfig']['Binds']), flush=True)
print('Mount Type=' + mount['Type'] + ' Source=' + source + ' Destination=' + mount['Destination'], flush=True)
probe = subprocess.run(docker + ['exec', container, 'docker', '--host', 'unix:///var/run/docker.sock',
    'info', '--format', '{{json .SecurityOptions}}'], text=True, capture_output=True)
print('Inside job docker info:', probe.returncode, (probe.stdout + probe.stderr).strip(), flush=True)
t.run(docker + ['exec', container, 'touch', '/tmp/q19-inspected'])
assert mount['Type'] == 'bind'
if phase == 'red':
    assert source == '/var/run/docker.sock', source
else:
    assert source == os.environ['DOCKER_SOCK'], source
    assert probe.returncode == 0 and 'name=rootless' in json.loads(probe.stdout)
done = t.wait_until(lambda: (c if (c := t.ok('GET', '/ci/candidate/' + cid))['status'] in ('passed', 'failed', 'unknown') else None), 240)
if phase == 'green':
    assert done['status'] == 'passed', done['verdictReason']
    t.wait_until(lambda: t.ok('GET', '/ci/candidate/' + cid)['landed'])
state.update(attempt=attempt['attempt'], source=source, infoExit=probe.returncode, status=done['status'])
t.save('q19-' + phase, state)
print('Q19 RED: act child binds the host rootful socket' if phase == 'red' else
      'Q19 GREEN: child mount source equals configured rootless socket; job docker info says name=rootless', flush=True)

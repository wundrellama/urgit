#!/usr/bin/env python3
"""Observe the retry's erasure job container without altering its workload."""
import json
import os
import pathlib
import re
import subprocess
import sys
import time

cid = sys.argv[1]
log = pathlib.Path(os.environ['RUNNER_HOME']) / 'a/daemon.log'
docker = ['docker', '--host', 'unix://' + os.environ['DOCKER_SOCK']]
until = time.monotonic() + 1200
while time.monotonic() < until:
    match = re.search(r'\[job ([^]]+)\] assignment [^\n]*candidate ' + re.escape(cid) + r' [^\n]*job "erasure"', log.read_text())
    if not match:
        time.sleep(1)
        continue
    aid = match[1]
    ids = subprocess.check_output(docker + ['ps', '-q', '--filter', 'network=ci-' + aid], text=True).split()
    for container in ids:
        info = json.loads(subprocess.check_output(docker + ['inspect', container]))[0]
        if not info['Name'].startswith('/act-'):
            continue
        mounts = info['Mounts']
        assert not any(m['Destination'] == '/tmp' or m['Destination'].startswith('/tmp/') for m in mounts), mounts
        assert not (info['HostConfig'].get('Tmpfs') or {}).get('/tmp'), info['HostConfig']['Tmpfs']
        print('Candidate', cid, 'erasure attempt', aid, flush=True)
        print('Actual act job container', info['Name'], flush=True)
        print('Mounts=' + json.dumps(mounts), flush=True)
        print('Binds=' + json.dumps(info['HostConfig']['Binds']) + ' Tmpfs=' + json.dumps(info['HostConfig'].get('Tmpfs')), flush=True)
        subprocess.run(docker + ['exec', container, 'sh', '-c',
            'test ! -e /tmp/erasewes || exit 1; echo "/tmp/erasewes: absent before erasure fixture"; findmnt -T /tmp -o TARGET,FSTYPE,SOURCE -n'], check=True)
        sys.exit(0)
    time.sleep(1)
raise SystemExit('erasure job container did not appear in the observation window')

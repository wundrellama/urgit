#!/usr/bin/env python3
"""S4: real signed deliveries, bad pin, and an honestly expired grant."""
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

class Proxy(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def forward(self):
        if self.command == 'GET' and self.path.endswith('/assignment') and self.server.replay:
            if self.server.replayed:
                time.sleep(0.25)
                self.send_response(204); self.send_header('Content-Length', '0'); self.end_headers(); return
            self.server.replayed = True
            data = json.dumps({'assignment': self.server.replay}).encode()
            self.send_response(200); self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)
            return
        conn = http.client.HTTPConnection('127.0.0.1', int(os.environ['PORT']), timeout=90)
        try:
            body = self.rfile.read(int(self.headers.get('content-length', '0')))
            headers = {k: v for k, v in self.headers.items() if k.lower() not in ('host', 'connection')}
            headers['Host'] = '127.0.0.1:' + os.environ['PORT']
            conn.request(self.command, self.path, body=body, headers=headers)
            resp = conn.getresponse(); data = resp.read(); code = resp.status
            if code == 200 and self.path.endswith('/assignment'):
                decoded = gzip.decompress(data) if resp.getheader('Content-Encoding') == 'gzip' else data
                a = json.loads(decoded)['assignment']; t.save('signed-offer-' + a['attempt'], a)
                self.server.offers.append(a)
                if self.server.hold and a['kind'] == 'job':
                    self.send_response(204); self.send_header('Content-Length', '0'); self.end_headers(); return
            self.send_response(code)
            for k, v in resp.getheaders():
                if k.lower() not in ('connection', 'transfer-encoding', 'content-length'): self.send_header(k, v)
            self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)
        except (OSError, http.client.HTTPException): pass
        finally: conn.close()
    do_GET = do_POST = forward

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Proxy)
server.offers = []; server.hold = False; server.replay = None; server.replayed = False
threading.Thread(target=server.serve_forever, daemon=True).start()

def runner(op, name):
    subprocess.run([str(P1/'runner.sh'), op, name], check=True)

def config(name, key, value):
    p = TMP/f'runner/{name}/config.toml'; data = p.read_text()
    data, count = re.subn('^'+re.escape(key)+r'\s*=.*$', key+' = '+json.dumps(value), data, flags=re.M)
    if not count: data += '\n'+key+' = '+json.dumps(value)+'\n'
    p.write_text(data); p.chmod(0o600)

def start(name, fresh=False):
    runner('stop', name)
    if fresh:
        folder = TMP/f'runner/{name}'
        if folder.exists(): folder.rename(TMP/f'runner-{name}-before-{time.time_ns()}')
        token = t.run([P1/'mint.sh'])
        subprocess.run([str(P1/'runner.sh'), 'config', name, '1', token], check=True, stdout=subprocess.DEVNULL)
    config(name, 'ship_url', f'http://127.0.0.1:{server.server_port}')
    runner('start', name)
    t.wait_until(lambda: (TMP/f'runner/{name}/state.json').exists(), 30)

def logs(name):
    return t.run(['bash','-c', 'source "$P1/lib.sh"; runner_log '+name])

def hold():
    server.hold = True
    state = t.writer_pull('q13')
    secret = 'p2-expiry-' + secrets.token_hex(16)
    state['secret'] = secret
    t.poke(f"[%set-credential '{state['repo']}' 'TOKEN' '{secret}' %job ~]")
    start('a', fresh=len(sys.argv)==2)
    t.merge(state)
    a = t.wait_until(lambda: next((a for a in server.offers if a['candidate']==state['cid'] and a['kind']=='job'),None), 180)
    assert len(a['grants']) == 1 and a['grants'][0]['sig'] != '0x0'
    state.update(attempt=a['attempt'], expiry=a['grants'][0]['expiry'], recipient=a['recipient'])
    t.save('q13-hold', state)
    print('Q13 held authentic signed job', a['attempt'], 'grant expires at Unix second', state['expiry'], flush=True)
    print('Job was captured at the ship channel and withheld from act; waiting preserves the original signed grant.', flush=True)

def build():
    subprocess.run(['go','build','-o',str(ROOT/'runner/urgit-runner'),'./cmd/urgit-runner'], cwd=ROOT/'runner', check=True, env=dict(os.environ, CGO_ENABLED='0'))

def q12(phase):
    if phase=='red':
        start('b', fresh=True); runner('stop','b')
        actual = t.ok('GET','/ci/key')
        # A different nonzero public key is an explicit pin. Enrollment
        # must not silently overwrite it and polling must not trust it.
        config('b','ci_pub','0x1')
        subprocess.run([str(P1/'q-mutants.sh'),'apply','Q12'],check=True); build()
    else:
        subprocess.run([str(P1/'q-mutants.sh'),'revert'],check=True); build()
    state=t.writer_pull('q12-red') if phase=='red' else t.load('q12-red')
    start('b')
    if phase=='red': t.merge(state)
    t.save('q12-'+phase,state)
    a=t.wait_until(lambda: next((a for a in server.offers if a['candidate']==state['cid']),None),180)
    assert a.get('sig') and a.get('recipient') and a.get('nonce')
    if phase=='red':
        t.wait_until(lambda:'sandbox ' in logs('b') and ' prepared ' in logs('b'),90)
        print('Q12 RED: wrong-pub daemon started signed work',a['attempt'],flush=True)
    else:
        t.wait_until(lambda:'signature does not verify with pinned CI public key' in logs('b'),30)
        assert ' prepared ' not in logs('b')
        assert not (TMP/'runner/b/work'/a['attempt']).exists()
        print('Q12 GREEN: wrong-pub daemon refused signed assignment before sandbox or checkout',a['attempt'],flush=True)
    key=t.ok('GET','/ci/key'); assert set(key)=={'pub','cert','ship-life'}
    assert key['pub']!='0x0' and key['cert']!='0x0'
    print('GET ci/key -> public key, certificate and life only:',key,flush=True)

def q13(phase):
    state=t.load('q13-hold')
    assert time.time()>state['expiry'], 'original grant has not expired yet'
    if phase=='red':
        subprocess.run([str(P1/'q-mutants.sh'),'apply','Q13'],check=True); build()
    else:
        subprocess.run([str(P1/'q-mutants.sh'),'revert'],check=True); build()
        server.replay=t.load('q13-redelivery')
        assert time.time()<server.replay['expiry'], 'outer assignment expired; replay must isolate grant expiry'
    start('a')
    if phase=='red':
        a=t.wait_until(lambda: next((a for a in server.offers if a['attempt']==state['attempt']),None),150)
        assert a['grants'][0]['expiry']==state['expiry'] and a['expiry']>time.time()
        t.save('q13-redelivery',a)
        t.wait_until(lambda:'--secret TOKEN=' in logs('a'),90)
        t.wait_until(lambda: t.dojo(f'status:(need .^((unit attempt:ci) %gx /=urgit-ci=/attempt/{state["attempt"]}/noun))') == '%passed', 120)
        print('Q13 RED: expired signed grant reached act',state['attempt'],flush=True)
    else:
        t.wait_until(lambda:'credential grant expired' in logs('a'),30)
        assert ' prepared ' not in logs('a') and '--secret TOKEN=' not in logs('a')
        print('Q13 GREEN: authentic expired grant refused before sandbox and act',state['attempt'],flush=True)
    print('grant expiry',state['expiry'],'observed time',int(time.time()),flush=True)

def positive():
    key=t.ok('GET','/ci/key'); config('b','ci_pub',key['pub'])
    state=t.writer_pull('q12-positive')
    secret='p2-signed-'+secrets.token_hex(16)
    state['secret']=secret
    t.poke(f"[%set-credential '{state['repo']}' 'TOKEN' '{secret}' %job ~]")
    start('b'); t.merge(state); t.save('q12-positive',state)
    t.finished(state,True)
    a=next(a for a in server.offers if a['candidate']==state['cid'] and a['kind']=='job')
    assert len(a['grants'])==1 and a['grants'][0]['sig']!='0x0'
    assert '--secret TOKEN=***' in logs('b')
    assert secret not in logs('b')
    local=TMP/'runner/b/work'/(a['attempt']+'.act.jsonl')
    assert local.exists() and secret not in local.read_text()
    print('S4 positive: correct pin verifies live assignment and grant; actual job passes and lands',a['attempt'],flush=True)


try:
    if sys.argv[1]=='hold': hold()
    elif sys.argv[1]=='q12': q12(sys.argv[2])
    elif sys.argv[1]=='q13': q13(sys.argv[2])
    elif sys.argv[1]=='positive': positive()
    else: raise SystemExit('usage: p2-signing.sh hold | q12 red|green | q13 red|green')
finally:
    runner('stop','b' if sys.argv[1] in ('q12','positive') else 'a')
    server.shutdown()
    if sys.argv[-1]=='red':
        subprocess.run([str(P1/'q-mutants.sh'),'revert'],check=True)

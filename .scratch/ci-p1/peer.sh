#!/bin/bash
# Real second-galaxy discovery -> fork -> local Git push -> peer PR.
# Every asynchronous read matches the id returned by its own request.
# usage: CI_PEER=1 peer.sh <upstream-ship> <upstream-repo> <fork-name>
source "$(dirname "$0")/env.sh"
[ "${CI_PEER:-0}" = 1 ] || { echo 'peer.sh must run with CI_PEER=1' >&2; exit 1; }
python3 - "$1" "$2" "$3" <<'PY'
import json, os, pathlib, re, subprocess, sys, time
upstream, repo, fork = sys.argv[1:]
root=pathlib.Path(os.environ['ROOT']); tmp=pathlib.Path(os.environ['TMP'])
api_script=root/'.scratch/ci-p0/api.sh'
def api(method, path, body=None, owner=False):
    env=dict(os.environ, CI_PEER='0' if owner else '1')
    out=subprocess.check_output([str(api_script),method,path,'' if body is None else json.dumps(body)],env=env,text=True,timeout=40)
    status, data=out.strip().split(' ',1)
    assert status in ('200','201','202'),out
    return json.loads(data)
def wait(path, collection, key, ident):
    until=time.monotonic()+180
    while time.monotonic()<until:
        found=[r for r in api('GET',path)[collection] if r[key]==ident]
        if found and not found[0]['active']:
            assert found[0]['ok'],found[0]
            print(key,ident,found[0].get('message',''),flush=True)
            return found[0]
        time.sleep(1)
    raise AssertionError(('peer timeout',path,ident,found))
discovery=api('POST','/peer/discover',{'ship':'~'+upstream})['request']
catalog=wait('/peer/discoveries','discoveries','request',discovery)
entry=next(r for r in catalog['repositories'] if r['name']==repo)
assert entry['writable'] is False,entry
transfer=api('POST','/peer/fork',{'ship':'~'+upstream,'repository':repo,'name':fork,'publicRead':True})['transfer']
wait('/peer/transfers','transfers','transfer',transfer)
clone=tmp/('clone-'+fork)
assert not clone.exists(),clone
subprocess.run(['git','-c','http.cookieFile='+os.environ['JAR'],'clone','-q',os.environ['URL']+'/git/'+fork,str(clone)],check=True)
def git(*args):
    return subprocess.check_output(['git','-C',str(clone),*args],text=True).strip()
git('config','user.name','p2-peer'); git('config','user.email','p2-peer@example')
git('config','http.cookieFile',os.environ['JAR'])
base=git('rev-parse','HEAD')
(clone/'peer-change.txt').write_text('authored by ~'+os.environ['SHIP']+' '+str(time.time_ns())+'\n')
git('add','peer-change.txt'); git('commit','-qm','p2 peer contribution')
head=git('rev-parse','HEAD'); git('push','-q','origin','HEAD:refs/heads/master')
pull_transfer=api('POST','/peer/pull-request',{'name':fork,'title':'p2 '+fork,'sourceBranch':'refs/heads/master','targetBranch':'refs/heads/master'})['transfer']
completed=wait('/peer/transfers','transfers','transfer',pull_transfer)
number=int(re.search(r'pull request #(\d+) opened',completed['message']).group(1))
pull=next(p for p in api('GET','/repository/'+repo,owner=True)['pullRequests'] if p['number']==number)
assert pull['sourceShip']=='~'+os.environ['SHIP'] and pull['sourceRepository']==fork,pull
assert pull['head']==head and pull['base']==base and pull['state']=='open',pull
result={'repo':repo,'fork':fork,'discovery':discovery,'forkTransfer':transfer,'pullTransfer':pull_transfer,'pull':number,'actor':pull['sourceShip'],'head':head,'base':base}
file=root/'.scratch/tmp'/('p2-peer-'+fork+'.json'); file.write_text(json.dumps(result,indent=2)+'\n')
print('peer PR verified:',json.dumps(result),flush=True)
PY

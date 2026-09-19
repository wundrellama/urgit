import test from 'node:test'
import assert from 'node:assert/strict'
import { approveHint, attemptPips, attemptRows, candidateRow, ciActions, credentialFormError, duration, lineText, noRunnerMessage, parseEnvs, renderLog } from './ci.js'

const jobA = { attempt: '0v1.a', kind: 'job', job: 'a', status: 'passed', started: 100, finished: 130, log: { key: 'k', size: 1, sha256: 'x' } }
const jobB = { attempt: '0v2.b', kind: 'job', job: 'b', status: 'running', started: 130, finished: null, log: null }
const plan = { attempt: '0v0.p', kind: 'plan', status: 'passed', started: 90, finished: 95, log: null }

test('a candidate row carries the ref, short head, actor, status, age and one pip per job attempt, oldest first', () => {
  const row = candidateRow({ id: '0vc', ref: 'refs/heads/master', head: '1e09de1959cf796d658f2cbda78b3e439d60815d', actor: '~dep', status: 'pending', trust: 'trusted', pull: 4, created: 1000, attempts: [jobB, jobA, plan] }, 1090)
  assert.equal(row.ref, 'master')
  assert.equal(row.head, '1e09de19')
  assert.equal(row.actor, '~dep')
  assert.equal(row.pull, 4)
  assert.equal(row.ageSeconds, 90)
  assert.deepEqual(row.pips.map((p) => p.status), ['passed', 'running'])
  assert.deepEqual(attemptPips([plan]), [])
})

test('attempt rows name the plan step, show the log link only when the ship recorded a log, compute durations, and carry runs-on from the plan', () => {
  const rows = attemptRows([jobB, jobA, plan], [{ id: 'a', workflow: 'w.yml', runsOn: ['self-hosted', 'big-mem'] }])
  assert.deepEqual(rows.map((r) => r.job), ['b', 'a', 'plan'])
  assert.deepEqual(rows.map((r) => r.hasLog), [false, true, false])
  assert.equal(rows[1].elapsed, '30s')
  assert.equal(rows[0].elapsed, '')
  assert.deepEqual(attemptRows([{ ...jobA, workflow: 'w.yml' }], [{ id: 'a', workflow: 'w.yml', runsOn: ['self-hosted', 'big-mem'] }])[0].runsOn, ['self-hosted', 'big-mem'])
  assert.deepEqual(rows[2].runsOn, [])
  assert.equal(duration(10, 3710), '1h 1m')
  assert.equal(duration(10, 5), '')
})

test('the first-run message shows only for a CI-required repository with no enrolled, unrevoked runner; the approve hint names the policy', () => {
  assert.equal(noRunnerMessage([], []), '')
  assert.match(noRunnerMessage(['refs/heads/master'], []), /No runner is enrolled/)
  assert.match(noRunnerMessage(['refs/heads/master'], [{ enrolled: 1, revoked: 5 }]), /No runner is enrolled/)
  assert.equal(noRunnerMessage(['refs/heads/master'], [{ enrolled: 1, revoked: null }]), '')
  assert.match(approveHint('restricted'), /^Policy: run restricted checks/)
  assert.match(approveHint('approval'), /^Policy: wait for approval/)
})

test('the log renders act jsonl as [job] step: msg lines grouped by group/endgroup, and keeps non-event lines as text', () => {
  const text = [
    '{"job":"w/a","jobID":"a","step":"Set up job","msg":"start","time":"t"}',
    '{"job":"w/a","jobID":"a","command":"group","arg":"Install deps","msg":"::group::Install deps","time":"t"}',
    '{"job":"w/a","jobID":"a","step":"deps","msg":"npm ci","time":"t"}',
    '{"job":"w/a","jobID":"a","command":"endgroup","msg":"::endgroup::","time":"t"}',
    'time="..." level=warning msg="not json"',
    '{"job":"w/a","jobID":"a","jobResult":"success","msg":"done","time":"t"}',
    '',
  ].join('\n')
  const groups = renderLog(text)
  assert.equal(groups.length, 3)
  assert.equal(groups[0].title, '')
  assert.equal(lineText(groups[0].lines[0]), '[a] Set up job: start')
  assert.equal(groups[1].title, 'Install deps')
  assert.equal(lineText(groups[1].lines[0]), '[a] deps: npm ci')
  assert.equal(groups[2].lines[0].msg, 'time="..." level=warning msg="not json"')
  assert.equal(groups[2].lines[1].result, 'success')
})

test('the log renderer coerces every field to text and survives hostile lines', () => {
  const groups = renderLog('{"job":1,"jobID":{"x":1},"msg":["a"],"step":null,"command":{"nope":1}}\n{"command":"group","arg":{"o":1}}\n[1,2]\n')
  assert.equal(typeof groups[0].lines[0].msg, 'string')
  assert.equal(groups[0].lines[0].step, '')
  assert.equal(groups[1].title, '[object Object]')
  assert.equal(groups[1].lines[0].msg, '[1,2]')
})

test('action bodies are the pokes as JSON', () => {
  assert.deepEqual(ciActions.approve('0v1'), { action: 'approve-candidate', id: '0v1' })
  assert.deepEqual(ciActions.setCiProtected('r', 'refs/heads/master', 1), { action: 'set-ci-protected', repo: 'r', ref: 'refs/heads/master', protected: true })
  assert.deepEqual(ciActions.setUntrustedPolicy('r', 'restricted'), { action: 'set-untrusted-policy', repo: 'r', policy: 'restricted' })
  assert.deepEqual(ciActions.setCredential('r', 'TOKEN', 'hunter2hunter2', 'env', ['staging']), { action: 'set-credential', repo: 'r', name: 'TOKEN', value: 'hunter2hunter2', scope: 'env', envs: ['staging'] })
  assert.deepEqual(ciActions.deleteCredential('r', 'TOKEN'), { action: 'delete-credential', repo: 'r', name: 'TOKEN' })
})

test('the credential form refuses a missing name, a bad name, a short value and an env scope without environments', () => {
  assert.match(credentialFormError({ name: '', value: 'hunter2hunter2', scope: 'job', envs: '' }), /name/)
  assert.match(credentialFormError({ name: 'my token', value: 'hunter2hunter2', scope: 'job', envs: '' }), /letters/)
  assert.match(credentialFormError({ name: 'TOKEN', value: 'short', scope: 'job', envs: '' }), /8 characters/)
  assert.match(credentialFormError({ name: 'TOKEN', value: 'line one\nline two', scope: 'job', envs: '' }), /single line/)
  assert.match(credentialFormError({ name: 'TOKEN', value: 'hunter2hunter2', scope: 'env', envs: ' ' }), /environment/)
  assert.equal(credentialFormError({ name: 'TOKEN', value: 'hunter2hunter2', scope: 'env', envs: 'staging, production' }), '')
  assert.deepEqual(parseEnvs('staging, production\nqa'), ['staging', 'production', 'qa'])
})

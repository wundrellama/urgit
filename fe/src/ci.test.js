import test from 'node:test'
import assert from 'node:assert/strict'
import { api } from './api.js'
import { ciAge, jobRows, parseLog, validateCredentialValue } from './ci.js'

test('logs preserve nested groups, ungrouped lines and malformed input as text', () => {
  const log = [
    { command: 'endgroup' }, { jobID: 'plan', step: 'Read', msg: 'ready' },
    { command: 'group', arg: 'Outer' }, { command: 'group', arg: 'Inner' },
    { job: 'suite', msg: '<img src=x onerror=alert(1)>' }, { command: 'endgroup' },
    { command: 'endgroup' }, { msg: 'done' },
  ].map(JSON.stringify).join('\n') + '\nmalformed\nnull\n'
  const nodes = parseLog(log)
  assert.equal(nodes[0].text, '[plan] Read: ready')
  assert.equal(nodes[1].group, 'Outer')
  assert.equal(nodes[1].children[0].group, 'Inner')
  assert.equal(nodes[1].children[0].children[0].text, '[suite] <img src=x onerror=alert(1)>')
  assert.deepEqual(nodes.slice(2).map((node) => node.text), ['[runner] done', '[runner] malformed', '[runner] null'])
  assert.deepEqual(parseLog(''), [])
})

test('job rows distinguish matching ids in separate workflows, waiting jobs and reruns', () => {
  const candidate = {
    plan: [{ workflow: 'a.yml', id: 'plan' }, { workflow: 'b.yml', id: 'plan' }, { workflow: 'b.yml', id: 'suite' }],
    attempts: [{ kind: 'plan', attempt: 'planner' }, { kind: 'job', workflow: 'b.yml', job: 'plan', attempt: 'b' }, { kind: 'job', workflow: 'a.yml', job: 'plan', attempt: 'a' }, { kind: 'job', workflow: 'a.yml', job: 'plan', attempt: 'rerun' }],
  }
  assert.deepEqual(jobRows(candidate).map((row) => row.attempt?.attempt || null), ['a', 'rerun', 'b', null])
})

test('credential values reject both newline forms without including the value in errors', () => {
  for (const value of ['private\nkey', 'private\rkey', 'private\r\nkey']) {
    assert.throws(() => validateCredentialValue(value), (error) => error.message.includes('single line') && !error.message.includes(value))
  }
  validateCredentialValue('single-line-value')
})

test('CI routes encode repository/cursor, post actions and reject multiline values before fetch', async () => {
  const saved = globalThis.fetch
  const calls = []
  globalThis.fetch = async (url, options) => {
    calls.push({ url, options })
    return { ok: true, text: async () => '{}' }
  }
  try {
    await api.ciCandidates('repo x', '0v1.a')
    assert.equal(calls[0].url, '/apps/urgit/api/ci/repository/repo%20x/candidates?before=0v1.a')
    assert.equal(calls[0].options.credentials, 'same-origin')
    await api.ciAction({ action: 'approve-candidate', id: '0v1.a' })
    assert.deepEqual(JSON.parse(calls[1].options.body), { action: 'approve-candidate', id: '0v1.a' })
    assert.equal(calls[1].options.method, 'POST')
    assert.throws(() => api.ciAction({ action: 'set-credential', value: 'secret\nline' }), /single line/)
    assert.equal(calls.length, 2)
  } finally { globalThis.fetch = saved }
})

test('log fetch returns text and diagnoses a missing object independently of the verdict', async () => {
  const saved = globalThis.fetch
  try {
    globalThis.fetch = async (_, options) => {
      assert.equal(options.credentials, 'same-origin')
      return { ok: true, text: async () => '{"msg":"hello"}\n' }
    }
    assert.equal(await api.ciLog('0v1'), '{"msg":"hello"}\n')
    globalThis.fetch = async () => ({ ok: false, status: 404 })
    await assert.rejects(api.ciLog('0v1'), /Log is unavailable/)
  } finally { globalThis.fetch = saved }
})

test('age is bounded at now and uses seconds, minutes, hours and days', () => {
  assert.equal(ciAge(120, 100000), '0s ago')
  assert.equal(ciAge(70, 100000), '30s ago')
  assert.equal(ciAge(0, 120000), '2m ago')
  assert.equal(ciAge(0, 7200000), '2h ago')
  assert.equal(ciAge(0, 172800000), '2d ago')
})

import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { canCancelRun, renderConcurrency, workflowRows } from './ciConcurrency.js'
import { all, h } from './runnerRecoveryFixtures.js'
import { candidateRow } from './ci.js'

const candidate = { id: 'run-a', status: 'pending', canCancel: true, runName: 'master at abcdef12', workflows: [
  { workflow: 'suite.yml', name: 'Suite', group: 'suite-branch', state: 'waiting', relatedRun: 'run-b', relatedName: 'feature at 12345678' },
  { workflow: 'fixtures.yml', name: 'Fixtures', group: 'fixtures-branch', state: 'running', reason: 'running in concurrency group' },
] }

test('waiting and cancellation explain the group and visible related run', () => {
  const [waiting] = workflowRows(candidate)
  assert.equal(waiting.detail, 'Waiting for feature at 12345678 in group suite-branch.')
  for (const [reason, expected] of [['cancel-in-progress', 'joined group suite-branch with cancel-in-progress'], ['newer waiting', 'is waiting in group suite-branch']]) {
    const [row] = workflowRows({ ...candidate, workflows: [{ ...candidate.workflows[0], state: 'cancelled', reason }] })
    assert.ok(row.detail.includes(expected), row.detail)
    assert.equal(row.canCancel, false)
    assert.ok(!row.detail.includes('run-b'))
  }
})

test('Cancel permissions fail closed; buttons invoke the exact whole-run and workflow actions', () => {
  for (const canCancel of [undefined, false, 'true']) {
    assert.equal(canCancelRun({ ...candidate, canCancel }), false)
    assert.equal(all(renderConcurrency(h, { ...candidate, canCancel }), n => n.type === 'button').length, 0)
  }
  for (const status of ['passed', 'failed', 'cancelled', 'unknown']) assert.equal(canCancelRun({ ...candidate, status }), false)
  assert.equal(canCancelRun({ ...candidate, status: 'running' }), true)
  const actions = []
  const buttons = all(renderConcurrency(h, candidate, { onCancel: a => actions.push(a) }), n => n.type === 'button')
  assert.equal(buttons.length, 3)
  buttons.forEach(b => b.props.onClick())
  assert.deepEqual(actions, [
    { action: 'cancel-workflow', id: 'run-a', reason: 'cancelled by the operator' },
    { action: 'cancel-workflow', id: 'run-a', workflow: 'suite.yml', reason: 'cancelled by the operator' },
    { action: 'cancel-workflow', id: 'run-a', workflow: 'fixtures.yml', reason: 'cancelled by the operator' },
  ])
  assert.ok(all(renderConcurrency(h, candidate, { busy: true }), n => n.type === 'button').every(n => n.props.disabled))
})

test('production rendering shows refusals, unknown locks and upgrade exclusions, escaping source text', () => {
  const data = { ...candidate, concurrencyNote: 'concurrency not applied: this run was recorded before state-8', workflows: [
    { workflow: 'bad.yml', name: 'Bad', state: 'refused', reason: 'job-level concurrency is not supported' },
    { workflow: 'expr.yml', state: 'refused', reason: 'concurrency group could not be evaluated: invalid fromJSON' },
    { workflow: 'old.yml', state: 'unknown', reason: 'concurrency not read for this lock: resolve or promote again to apply it', group: '<img src=x onerror=alert(1)>' },
  ] }
  const html = renderToStaticMarkup(renderConcurrency(createElement, data))
  for (const phrase of ['before state-8', 'job-level concurrency', 'invalid fromJSON', 'resolve or promote again', '&lt;img']) assert.ok(html.includes(phrase))
  assert.ok(!html.includes('<img'))
  assert.equal(candidateRow({ ...candidate, status: 'cancelled' }).statusText, 'cancelled')
})

test('320px layout can shrink, wrap long group names and keep controls usable (CSS contract, not browser geometry)', () => {
  const css = readFileSync(new URL('./style.css', import.meta.url), 'utf8')
  assert.match(css, /\.ci-concurrency \{[^}]*grid-template-columns: minmax\(0, 1fr\)/)
  assert.match(css, /\.ci-concurrency-header \{[^}]*flex-wrap: wrap/)
  assert.match(css, /\.ci-concurrency button \{[^}]*overflow-wrap: anywhere/)
  assert.match(css, /\.ci-concurrency button \{ min-height: 40px;/)
  const html = renderToStaticMarkup(renderConcurrency(createElement, { ...candidate, workflows: [{ ...candidate.workflows[0], group: 'x'.repeat(500) }] }))
  assert.ok(html.includes('x'.repeat(500)))
})

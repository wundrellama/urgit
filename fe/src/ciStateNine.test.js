import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { arm, deskText, type } from './hoonSource.js'

// State-9 (pull request labels): state-8 frozen whole, and the pull facts
// of every event run and recorded forge event given labels. Source reading,
// not a run: the populated ci-migration-vector and a live in-place upgrade
// are the parent's. State-9 is itself frozen now (CI-RECOVERY-1), as
// sur/ci-state-9.hoon: its shape is read there, and ciStateTen.test.js
// reads the state-10 that sur/ci.hoon holds.

const frozen = deskText('sur/ci-state-8.hoon')
const nine = deskText('sur/ci-state-9.hoon')
const current = deskText('sur/ci.hoon')
const migrate = deskText('lib/ci-migrate.hoon')
const controller = deskText('app/urgit-ci.hoon')

// the faces of a $: type's fields, in order (as ciStateEight.test.js reads
// them): the first after `$:  `, then each line at its column; a field
// written `=via` has the face via; the version tag is no field
function fields(typeText) {
  const lines = typeText.split('\n')
  const at = lines.findIndex((l) => /^\s*\$:\s{2}/.test(l))
  assert.ok(at >= 0, 'a $: type')
  const col = lines[at].indexOf('$:') + 4
  const out = []
  const push = (token) => {
    if (token.startsWith('%')) return
    const name = token.startsWith('=') ? token.slice(1) : token.slice(0, token.indexOf('='))
    assert.match(name, /^[a-z][a-z0-9-]*$/, `a field face in ${token}`)
    out.push(name)
  }
  push(lines[at].slice(col).split(/\s/)[0])
  for (const l of lines.slice(at + 1)) {
    if (!l.trim() || /^\s*::/.test(l)) continue
    const indent = l.length - l.trimStart().length
    if (indent < col) break
    if (indent === col) push(l.trim().split(/\s/)[0])
  }
  return out
}

// the fields a conversion's `:*` tuple writes, in order: each line's
// `<face>.<sample>`, or the line itself (a constant, or a binding's face)
function tupleFields(armText, sample) {
  const start = armText.indexOf(':*  ')
  assert.ok(start >= 0, 'a :* tuple')
  const end = armText.indexOf('\n  ==', start)
  return armText.slice(start + 4, end).split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
    const m = l.match(new RegExp(`([a-z][a-z0-9-]*)\\.${sample}\\b`))
    return m ? m[1] : l
  })
}

test('state-8 is frozen byte for byte: the baseline\'s sur/ci.hoon (d2876d6)', () => {
  assert.equal(createHash('sha256').update(frozen).digest('hex'), '1abf1efa4083f25f13cf4f60652141552a46da5b3a8894e0acc14326721fff2e')
  assert.match(frozen, /\+\$ {2}state-8\n {2}\$: {2}%8\n/)
  assert.match(nine, /\+\$ {2}state-9\n {2}\$: {2}%9\n/)
  assert.doesNotMatch(nine, /\+\$ {2}state-8\n/, 'sur/ci-state-9.hoon holds the state-9 shape only')
  assert.doesNotMatch(current, /\+\$ {2}state-(8|9)\n/, 'sur/ci.hoon holds the current state only')
})

test('state-9 keeps state-8\'s thirty-six fields in order, and the conversion writes each in its place', () => {
  const eight = fields(type(frozen, 'state-8'))
  assert.equal(eight.length, 36)
  assert.deepEqual(fields(type(nine, 'state-9')), eight)
  const written = tupleFields(arm(migrate, 'from-state-8'), 'o')
  assert.equal(written[0], '%9')
  assert.deepEqual(written.slice(1), eight)
  assert.match(arm(migrate, 'from-state-8'), /\(~\(run by candidates\.o\) candidate-from-8\)/)
  assert.match(arm(migrate, 'from-state-8'), /\(~\(run by forge-events\.o\) forge-event-from-8\)/)
})

test('pull facts gain labels and label last, and a recorded fact gets neither', () => {
  const old = fields(type(frozen, 'pull-facts'))
  assert.deepEqual(fields(type(nine, 'pull-facts')), [...old, 'labels', 'label'])
  assert.match(type(nine, 'pull-facts'), /labels=\(list @t\)\n {6}label=\(unit @t\)\n/)
  assert.deepEqual(tupleFields(arm(migrate, 'pull-facts-from-8'), 'p'), [...old, '~', '~'])
})

test('every candidate, run event and forge event field is written in its place; only the pull facts change', () => {
  for (const [name, conversion, sample, changed] of [
    ['candidate', 'candidate-from-8', 'c', '(trigger-from-8 trigger.c)'],
    ['run-event', 'run-event-from-8', 'e', 'pull'],
    ['forge-event', 'forge-event-from-8', 'e', 'pull'],
  ]) {
    const old = fields(type(frozen, name))
    assert.deepEqual(fields(type(nine, name)), old, `${name} keeps its fields`)
    assert.deepEqual(tupleFields(arm(migrate, conversion), sample), old, `${conversion} writes every ${name} field in its place`)
    assert.ok(arm(migrate, conversion).includes(changed), `${conversion} converts ${changed}`)
  }
  // a push and a manual run keep their triggers; an event run's pull facts
  // are converted, its payload text copied (never rewritten)
  const trigger = arm(migrate, 'trigger-from-8')
  assert.match(trigger, /%push {2}\[%push ~\]/)
  assert.match(trigger, /%dispatch {2}t\n/)
  assert.match(trigger, /%event {2}\[%event \(run-event-from-8 run-event\.t\)\]/)
  assert.match(arm(migrate, 'run-event-from-8'), /\n {6}payload\.e\n/)
})

test('the wire: a pull request update carries the state, the labels and the label last', () => {
  assert.deepEqual(fields(type(nine, 'pull-update')), [...fields(type(frozen, 'pull-update')), 'state', 'labels', 'label'])
})

test('every earlier state reaches state-9 through state-8, and the controller refuses a malformed update before reading it', () => {
  // the state-8 to state-9 conversion is +nine's, on the way to state-10
  assert.match(arm(migrate, 'nine'), /\(from-state-8 \(eight shape old\)\)/)
  assert.match(arm(migrate, 'nine'), /\^- {2}state-9:ci-state-9/)
  assert.match(arm(migrate, 'from-state-8'), /\^- {2}state-9:ci-state-9/)
  const admit = arm(controller, 'admit-pull')
  const order = ['(pull-update-error:ci-forge p)', '(verify-pull repo p)', '(label-outcome:ci-trigger p)', '(governing repo target-ref.p)'].map((s) => admit.indexOf(s))
  assert.ok(order.every((at) => at >= 0), `admit-pull reads ${order}`)
  assert.deepEqual([...order].sort((a, b) => a - b), order, 'malformed, then stale, then a closed or merged label change, then selection')
  assert.match(arm(controller, 'verify-pull'), /\(pull-stale:ci-forge p u\.u\.found\)/)
})

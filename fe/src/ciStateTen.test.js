import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { arm, deskText, type } from './hoonSource.js'

// State-10 (CI-RECOVERY-1): state-9 frozen whole, and one field added after
// its thirty-six, the recovery record. Source reading, not a run: the
// populated ci-migration-vector and a live in-place upgrade are the
// parent's.

const frozen = deskText('sur/ci-state-9.hoon')
const current = deskText('sur/ci.hoon')
const migrate = deskText('lib/ci-migrate.hoon')
const controller = deskText('app/urgit-ci.hoon')

// the faces of a $: type's fields, in order (as ciStateNine.test.js reads
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
// `<face>.<sample>`, or the line itself (a constant, or a bunt)
function tupleFields(armText, sample) {
  const start = armText.indexOf(':*  ')
  assert.ok(start >= 0, 'a :* tuple')
  const end = armText.indexOf('\n  ==', start)
  return armText.slice(start + 4, end).split('\n').map((l) => l.trim()).filter(Boolean).map((l) => {
    const m = l.match(new RegExp(`^([a-z][a-z0-9-]*)\\.${sample}$`))
    return m ? m[1] : l
  })
}

test('state-9 is frozen byte for byte: the baseline\'s sur/ci.hoon (cb3b4db)', () => {
  assert.equal(createHash('sha256').update(frozen).digest('hex'), 'edd5cad71a9048641382f9ef596409add2870f57d21a9e6b2ff70d975bb14e2c')
  assert.match(frozen, /\+\$ {2}state-9\n {2}\$: {2}%9\n/)
  assert.match(current, /\+\$ {2}state-10\n {2}\$: {2}%10\n/)
  assert.doesNotMatch(current, /\+\$ {2}state-9\n/, 'sur/ci.hoon holds the current state only')
})

test('state-10 keeps state-9\'s thirty-six fields in order and adds the recovery record last', () => {
  const nine = fields(type(frozen, 'state-9'))
  assert.equal(nine.length, 36)
  assert.deepEqual(fields(type(current, 'state-10')), [...nine, 'recovery'])
  assert.match(type(current, 'state-10'), /\n {6}recovery=recovery-state\n {2}==/)
})

test('the conversion writes every state-9 field in its place, then the bunt of the recovery record', () => {
  const nine = fields(type(frozen, 'state-9'))
  const written = tupleFields(arm(migrate, 'from-state-9'), 'o')
  assert.equal(written[0], '%10')
  assert.deepEqual(written.slice(1, 37), nine)
  assert.deepEqual(written.slice(37), ['*recovery-state:ci'])
  assert.match(arm(migrate, 'from-state-9'), /\|= {2}o=state-9:ci-state-9\n {2}\^- {2}state-10:ci\n/)
})

test('an old ship loads unpaused, with no checkpoint, no staged import and no history: the record\'s bunt is that', () => {
  const record = type(current, 'recovery-state')
  assert.deepEqual(fields(record), ['paused', 'checkpoints', 'staged', 'restores', 'history', 'sequence'])
  assert.match(record, /paused=\(unit \[at=@da actor=@p reason=@t\]\)/, 'a unit: its bunt is unpaused')
  assert.match(record, /checkpoints=\(list checkpoint\)/)
  assert.match(record, /staged=\(unit staged-checkpoint\)/)
  assert.match(record, /restores=\(list restore\)/)
  assert.match(record, /sequence=@ud/)
  for (const list of fields(type(current, 'restored-history'))) {
    assert.match(type(current, 'restored-history'), new RegExp(`${list}=\\(list `), `${list} is a list: its bunt is empty`)
  }
})

test('every saved shape reaches state-10 explicitly, a %10 is read as state-10 and a %9 as the frozen state-9, and nothing else', () => {
  const shapeOf = arm(migrate, 'shape-of')
  assert.match(shapeOf, /%10\n {4}\?:  !=\(~ \(mole \|\.\(;;\(state-10:ci old\)\)\)\)  &\+%current/)
  assert.match(shapeOf, /%9\n {4}\?:  !=\(~ \(mole \|\.\(;;\(state-9:ci-state-9 old\)\)\)\)  &\+%state-9/)
  assert.match(arm(migrate, 'load'), /\?:  =\(%current p\.found\)  ;;\(state-10:ci old\)\n {2}\(from-state-9 \(nine p\.found old\)\)/)
  assert.match(arm(migrate, 'nine'), /\?:  =\(%state-9 shape\)  ;;\(state-9:ci-state-9 old\)\n {2}\(from-state-8 \(eight shape old\)\)/)
  assert.match(migrate, /\+\$ {2}shape {2}\?\(%current %state-9 %state-8 /)
})

test('the controller loads state-10 and audits a conversion as such', () => {
  assert.match(controller, /^=\| {2}state-10:ci$/m)
  assert.doesNotMatch(controller, /state-9:ci\b(?!-)/, 'no arm of the controller names the old state')
  assert.match(arm(controller, 'on-load'), /converted to state-10/)
  assert.match(arm(controller, 'on-load'), /\(load:ci-migrate q\.old\)/)
})

test('the migration vector checks the state-9 conversion on a populated state-9, and the refusals', () => {
  const vector = deskText('gen/ci-migration-vector.hoon')
  assert.match(vector, /=\/  ninth  \(load-9:ci-migrate s8\)/)
  assert.match(vector, /'state-9 is tagged %10 once converted' %\.y =\(%10 -\.new\.tenth\)/)
  assert.match(vector, /'the recovery record is exactly the bunt' %\.y =\(\*recovery-state:ci recovery\.new\.tenth\)/)
  assert.match(vector, /'a state-9 tagged %10 is refused' %\.y \(refused \[%10 \+\.s9\]\)/)
  assert.match(vector, /'a state-10 tagged %9 is refused' %\.y \(refused \[%9 \+\.new\.tenth\]\)/)
  assert.match(vector, /:\(weld early added late eight nine ten\)/)
})

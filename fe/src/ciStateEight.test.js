import test from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { arm, deskText, type } from './hoonSource.js'

// State-8 (CI-CONCURRENCY-1): state-7 frozen whole, and every record of it
// converted field by field. Source reading, not a run: the populated
// ci-migration-vector and a live in-place upgrade are the parent's. State-9
// (pull request labels) froze state-8 whole as sur/ci-state-8, which is what
// the conversion from state-7 produces now (ciStateNine.test.js).

const frozen = deskText('sur/ci-state-7.hoon')
const current = deskText('sur/ci-state-8.hoon')
const migrate = deskText('lib/ci-migrate.hoon')

// the faces of a $: type's fields, in order: the first after `$:  `, then
// each line at its column (deeper lines continue a field's type); a field
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

// every `<face>.<sample>` a conversion reads, in the order it first reads it
function copied(armText, sample) {
  const seen = []
  for (const m of armText.matchAll(new RegExp(`([a-z][a-z0-9-]*)\\.${sample}\\b`, 'g'))) if (!seen.includes(m[1])) seen.push(m[1])
  return seen
}

test('state-7 is frozen byte for byte: the baseline\'s sur/ci.hoon (07bd3a0)', () => {
  assert.equal(createHash('sha256').update(frozen).digest('hex'), '3a7298ff5cd045066565a8fb8d238bed20cf8b43bc157ecd7e6e0f4b301fb3d1')
  assert.match(current, /\+\$ {2}state-8\n {2}\$: {2}%8\n/)
})

test('state-8 keeps state-7\'s thirty-six fields in order, and the conversion copies each one', () => {
  const seven = fields(type(frozen, 'state-7'))
  assert.equal(seven.length, 36)
  assert.deepEqual(fields(type(current, 'state-8')), seven)
  assert.deepEqual(copied(arm(migrate, 'from-state-7'), 'o'), seven)
})

test('every candidate, attempt, lock and catalog workflow field is copied in place; what state-8 adds comes last', () => {
  for (const [name, conversion, sample, added] of [
    ['candidate', 'candidate-from-7', 'c', ['concurrency', 'cancelled']],
    ['attempt', 'attempt-from-7', 'a', []],
    ['lock', 'lock-from-7', 'l', []],
    ['trigger-workflow', 'workflow-from-7', 'w', ['concurrency']],
  ]) {
    const old = fields(type(frozen, name))
    assert.deepEqual(copied(arm(migrate, conversion), sample), old, `${conversion} copies every ${name} field in its place`)
    assert.deepEqual(fields(type(current, name)), [...old, ...added], `state-8's ${name}`)
  }
  // an open run takes part in no group and says why; a closed one has none
  // recorded; no run starts cancelled; a catalog's concurrency is unknown
  const candidate = arm(migrate, 'candidate-from-7')
  assert.ok(candidate.includes('?:(=(%pending status.c) [%exempt in-flight] [%unknown before-groups])\n      ~\n  =='))
  assert.ok(arm(migrate, 'workflow-from-7').includes('[workflow.w name.w events.w refusal.w ~]'))
})

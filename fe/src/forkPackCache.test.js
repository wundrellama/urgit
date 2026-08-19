import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const agent = readFileSync(
  new URL('../../desk/app/urgit.hoon', import.meta.url),
  'utf8',
)
const surface = readFileSync(
  new URL('../../desk/sur/git.hoon', import.meta.url),
  'utf8',
)

function sourceBlock(source, start, end) {
  const startAt = source.indexOf(start)
  assert.notEqual(startAt, -1, `missing ${start}`)
  const endAt = source.indexOf(end, startAt + start.length)
  assert.notEqual(endAt, -1, `missing ${end} after ${start}`)
  return source.slice(startAt, endAt)
}

test('fork pack pages use coarse 32 MiB and 8192-object bounds', () => {
  const paging = sourceBlock(agent, '++  peer-object-pages', '++  peer-browse-pages')

  assert.match(paging, /=\(count 8\.192\)/)
  assert.match(paging, /33\.554\.432/)
  assert.doesNotMatch(paging, /=\(count 256\)/)
  assert.doesNotMatch(paging, /524\.288/)
})

test('one transient full-fork cache entry is outside persisted state', () => {
  const cacheMold = sourceBlock(agent, '+$  peer-fork-cache', '+$  peer-receive')
  assert.match(cacheMold, /repository=@t/)
  assert.match(cacheMold, /revision=@t/)
  assert.match(cacheMold, /fingerprint=@uv/)
  assert.match(cacheMold, /pages=\(list octs\)/)

  const transients = sourceBlock(agent, '=|  state-2:git', '^-  agent:gall')
  assert.match(transients, /=\/  fork-pack-cache\s+\*\(unit peer-fork-cache\)/)
  assert.equal((transients.match(/^=\/  fork-pack-cache/gm) || []).length, 1)

  const onSave = sourceBlock(agent, '++  on-save', '++  on-load')
  assert.match(onSave, /!>\(state\)/)
  assert.doesNotMatch(onSave, /fork-pack-cache/)
  assert.doesNotMatch(surface, /fork-pack-cache|peer-fork-cache/)
})

test('init and load explicitly reset the transient fork cache', () => {
  const onInit = sourceBlock(agent, '++  on-init', '++  on-save')
  const onLoad = sourceBlock(agent, '++  on-load', '++  on-poke')

  assert.match(onInit, /this\(fork-pack-cache ~\)/)
  assert.match(onLoad, /fork-pack-cache ~/)
})

test('cache fingerprint deterministically covers repository revision and sorted object keys', () => {
  const fingerprint = sourceBlock(
    agent,
    '++  peer-object-fingerprint',
    '++  peer-transfer-yawns',
  )

  assert.match(fingerprint, /\[revision=@t objects=\(map oid:git object:git\)\]/)
  assert.match(fingerprint, /ordered=\(list oid:git\)/)
  assert.match(fingerprint, /sort\s+keys/)
  assert.match(fingerprint, /\(lth a b\)/)
  assert.match(fingerprint, /shax \(jam \[revision ordered\]\)/)
})

test('authorized full forks alone consult and replace the one-entry cache', () => {
  const prepare = sourceBlock(agent, '++  peer-prepare', '++  peer-ready')
  const authorization = prepare.indexOf('repository-readable u.found target')
  const lookup = prepare.indexOf('cached-pages=(unit (list octs))')

  assert.ok(authorization >= 0, 'missing repository authorization')
  assert.ok(lookup > authorization, 'cache lookup must follow authorization')
  assert.match(prepare, /=\/  full-fork=\?\s+\?=\(~ haves\.req\)/)
  assert.match(prepare, /cached-pages=\(unit \(list octs\)\)[\s\S]*\?\.\s+full-fork\s+~/)
  assert.match(prepare, /\?&\(cacheable \?=\(~ cached-pages\)\)/)
})

test('cache hits reuse pages and full-fork misses replace the cache', () => {
  const prepare = sourceBlock(agent, '++  peer-prepare', '++  peer-ready')

  assert.match(
    prepare,
    /=\/  pages=\(list octs\)[\s\S]*\?~\s+cached-pages\s+\(peer-object-pages objects\)\s+u\.cached-pages/,
  )
  assert.match(
    prepare,
    /fork-pack-cache[\s\S]*`\[repository\.req revision fingerprint pages\]/,
  )
  assert.match(prepare, /=\(repository\.req repository\.u\.fork-pack-cache\)/)
  assert.match(prepare, /=\(revision revision\.u\.fork-pack-cache\)/)
  assert.match(prepare, /=\(fingerprint fingerprint\.u\.fork-pack-cache\)/)
})

test('cache stores only one pack no larger than 32 MiB', () => {
  const prepare = sourceBlock(agent, '++  peer-prepare', '++  peer-ready')

  assert.match(prepare, /=\/  cacheable=\?/)
  assert.match(prepare, /\?~  pages  %\.n/)
  assert.match(prepare, /=\(~ t\.pages\)/)
  assert.match(prepare, /\(lte p\.i\.pages 33\.554\.432\)/)
  assert.match(prepare, /\?&\(cacheable \?=\(~ cached-pages\)\)/)
})

test('every transfer still gets fresh capability-scoped grow paths', () => {
  const prepare = sourceBlock(agent, '++  peer-prepare', '++  peer-ready')

  assert.match(prepare, /=\/  snapshot-path=path\s+\/fine\/\(peer-fine-name transfer\.req\)/)
  assert.match(
    prepare,
    /\[%pass \/peer\/grow\/\(scot %uv transfer\.req\) %grow snapshot-path noun\+!>\(page\)\]/,
  )
  assert.doesNotMatch(prepare, /%grow \/fine\/(?!\(peer-fine-name transfer\.req\))/)
})

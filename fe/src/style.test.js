import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const css = readFileSync(new URL('./style.css', import.meta.url), 'utf8')

function declarations(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return [...css.matchAll(new RegExp(`${escaped}\\s*\\{([^}]+)\\}`, 'g'))]
    .map((match) => match[1].replace(/\s+/g, ' ').trim())
}

test('keeps upstream update text and actions in explicit desktop grid tracks', () => {
  const [row] = declarations('.upstream-update-item')
  const [actions] = declarations('.upstream-update-item > .row-actions')

  assert.match(row, /display:\s*grid/)
  assert.match(row, /grid-template-columns:\s*minmax\(0,\s*1fr\)\s+max-content/)
  assert.doesNotMatch(row, /flex-wrap/)
  assert.match(actions, /justify-self:\s*end/)
  assert.doesNotMatch(actions, /margin-left:\s*auto/)
})

test('stacks upstream update actions below text on narrow screens', () => {
  const rows = declarations('.upstream-update-item')
  const actions = declarations('.upstream-update-item > .row-actions')

  assert.ok(rows.some((rule) => /grid-template-columns:\s*minmax\(0,\s*1fr\)/.test(rule)))
  assert.ok(actions.some((rule) => /justify-self:\s*start/.test(rule)))
})

test('keeps file history columns aligned independently of size text', () => {
  const rows = declarations('.file-tree.with-history .table-head, .file-tree.with-history .table-row')

  assert.ok(rows.some((rule) => /grid-template-columns:\s*minmax\(220px,\s*36%\)\s+minmax\(0,\s*1fr\)\s+76px\s+90px/.test(rule)))
  assert.ok(rows.every((rule) => !/grid-template-columns:[^;]+\sauto(?:;|$)/.test(rule)))
})

test('stacks a group row as title over host, each line truncating inside the label column', () => {
  const [row] = declarations('.group-link')
  const [label] = declarations('.group-label')
  const [host] = declarations('.host-tag')
  const [truncate] = declarations('.truncate')

  // the label column takes the width the chevron leaves and may shrink below its content
  assert.match(label, /min-width:\s*0/)
  assert.match(label, /flex:\s*1 1 auto/)
  assert.match(label, /display:\s*grid/)
  // the host is a line of its own now, not a tag pushed to the row's right edge
  assert.doesNotMatch(host, /margin-left:\s*auto/)
  assert.doesNotMatch(host, /flex:\s*0 0 auto/)
  assert.match(host, /min-width:\s*0/)
  // two lines need more than the 36px one-line row
  assert.match(row, /min-height:\s*4\dpx/)
  // a long label cannot squeeze the chevron, so every title starts at the same x
  const [chevron] = declarations('.group-link .peer-chevron')
  assert.match(chevron, /flex:\s*0 0 auto/)
  // truncation stays the one rule both lines share
  assert.match(truncate, /white-space:\s*nowrap/)
  assert.match(truncate, /overflow:\s*hidden/)
  assert.match(truncate, /text-overflow:\s*ellipsis/)
  // the peer row's grid is untouched: label column then the 26px button column
  const [peerRow] = declarations('.peer-link-row')
  assert.match(peerRow, /grid-template-columns:\s*minmax\(0,\s*1fr\)\s+26px/)
})

test('keeps wide Markdown tables scrollable within the readme panel', () => {
  const [table] = declarations('.markdown-body table')

  assert.match(table, /display:\s*block/)
  assert.match(table, /max-width:\s*100%/)
  assert.match(table, /overflow-x:\s*auto/)
})

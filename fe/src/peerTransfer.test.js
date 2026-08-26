import test from 'node:test'
import assert from 'node:assert/strict'
import { peerTransferPresentation } from './peerTransfer.js'
import { formatBytes } from './format.js'

// Payloads captured from running ships; the tests hold the presentation to
// the shapes the backend actually emits.

const chunkedMidFlight = {
  stage: 'fine', received: 4198, expected: 8987, expectedBytes: 0,
  pages: 205, completedPages: 98,
  fineFragmentsReceived: 0, fineFragmentsTotal: 0,
}

const chunkedJustStarted = {
  stage: 'fine', received: 0, expected: 8987, expectedBytes: 0,
  pages: 205, completedPages: 0,
  fineFragmentsReceived: 0, fineFragmentsTotal: 0,
}

const archive = {
  stage: 'archive', received: 0, expected: 8987, expectedBytes: 203865397,
  pages: 1, completedPages: 0,
  fineFragmentsReceived: 0, fineFragmentsTotal: 0,
}

const request = {
  stage: 'request', received: 0, expected: 0, expectedBytes: 0,
  pages: 0, completedPages: 0,
  fineFragmentsReceived: 0, fineFragmentsTotal: 0,
}

test('a chunked transfer mid-flight renders a determinate page bar', () => {
  const view = peerTransferPresentation(chunkedMidFlight)
  assert.equal(view.determinate, true)
  assert.equal(view.max, 205)
  assert.equal(view.value, 98)
  assert.equal(view.label, 'Transferring repository · 98 of 205 pages…')
})

test('a chunked transfer at zero progress is determinate with an empty bar', () => {
  const view = peerTransferPresentation(chunkedJustStarted)
  assert.equal(view.determinate, true)
  assert.equal(view.max, 205)
  assert.equal(view.value, 0)
  assert.equal(view.label, 'Transferring repository · 0 of 205 pages…')
})

test('the label names the basis the ship actually reported', () => {
  const fragments = peerTransferPresentation({ stage: 'fine', fineFragmentsReceived: 3, fineFragmentsTotal: 12 })
  assert.equal(fragments.determinate, true)
  assert.equal(fragments.max, 12)
  assert.equal(fragments.value, 3)
  assert.match(fragments.label, /3 of 12 fragments/)
  const objects = peerTransferPresentation({ stage: 'fine', received: 9, expected: 18, pages: 0, fineFragmentsTotal: 0 })
  assert.equal(objects.determinate, true)
  assert.equal(objects.max, 18)
  assert.equal(objects.value, 9)
  assert.match(objects.label, /9 of 18 objects/)
})

test('an archive transfer stays indeterminate despite its pinned counters', () => {
  const view = peerTransferPresentation(archive)
  assert.equal(view.determinate, false)
  assert.equal(view.max, undefined)
  assert.equal(view.value, undefined)
  assert.equal(view.label, `Transferring repository archive · ${formatBytes(203865397)}…`)
})

test('the pre-transfer stages keep their indeterminate labels', () => {
  const early = peerTransferPresentation(request)
  assert.equal(early.determinate, false)
  assert.equal(early.label, 'Waiting for peer…')
  const preparing = peerTransferPresentation({ ...request, stage: 'prepare' })
  assert.equal(preparing.determinate, false)
  assert.equal(preparing.label, 'Peer is preparing repository archive…')
})

test('a missing transfer is still contacting the peer', () => {
  const view = peerTransferPresentation(null)
  assert.equal(view.determinate, false)
  assert.equal(view.label, 'Contacting peer…')
})

test('a chunked transfer with no denominators yet falls back to indeterminate', () => {
  const view = peerTransferPresentation({ stage: 'fine', received: 0, expected: 0, pages: 0, fineFragmentsTotal: 0 })
  assert.equal(view.determinate, false)
  assert.equal(view.label, 'Transferring repository…')
})

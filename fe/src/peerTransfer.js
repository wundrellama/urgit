import { formatBytes } from './format.js'
import { transferFraction, transferBasis } from './transferFeed.js'

const basisCounts = {
  fragments: (transfer) => [transfer.fineFragmentsReceived, transfer.fineFragmentsTotal],
  pages: (transfer) => [transfer.completedPages, transfer.pages],
  objects: (transfer) => [transfer.received, transfer.expected],
}

export function peerTransferPresentation(transfer) {
  if (!transfer) return { label: 'Contacting peer…', determinate: false }

  const stage = transfer.stage || ''
  const expectedBytes = Number(transfer.expectedBytes || 0)
  // An archive transfer reports expected/pages but never advances them, so a
  // bar would sit at 0% while claiming to measure something. The byte size is
  // the only real information it carries.
  const archive = stage === 'archive' || expectedBytes > 0
  if (archive) {
    const size = expectedBytes > 0 ? ` · ${formatBytes(expectedBytes)}` : ''
    return { label: `Transferring repository archive${size}…`, determinate: false }
  }

  if (stage === 'prepare') return { label: 'Peer is preparing repository archive…', determinate: false }
  if (stage === 'request') return { label: 'Waiting for peer…', determinate: false }

  const basis = transferBasis(transfer)
  if (basis !== null && transferFraction(transfer) !== null) {
    const [done, total] = basisCounts[basis](transfer).map(Number)
    return {
      label: `Transferring repository · ${done.toLocaleString()} of ${total.toLocaleString()} ${basis}…`,
      determinate: true,
      max: total,
      value: Math.max(0, Math.min(done, total)),
    }
  }
  return { label: 'Transferring repository…', determinate: false }
}

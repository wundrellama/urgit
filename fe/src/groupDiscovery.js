// Group-driven discovery for the Groups section.
//
// A member opens one of the groups this ship is in and the agent asks every
// seated member for its catalog; nobody types a ship or repository name.
// Each answer names, per repository, the group whose policy alone let this
// ship read it (`via`), or null when the owner, a public flag, or an explicit
// reader or writer entry did.  The answers are folded into one catalog keyed
// by (ship, repository), and an entry is badged only when the group being
// looked at is not what let this ship see it.

export const groupOptionLabel = (group) => `${group.title} (${group.flag})`

// `groups` is the normalised Groups list; a flag it does not know is shown as is
export function describeVia(via, groups) {
  if (!via) return ''
  const group = (groups || []).find((entry) => entry.flag === via)
  return `shared with ${group ? group.title : via}`
}

// the badge on an entry seen under the group `flag`: none when that group
// granted the read, the granting group when another did, and a note when a
// public flag or the owner's own lists did (the wire does not say which)
export function describeGrant(via, flag, groups) {
  if (via && via === flag) return ''
  return via ? describeVia(via, groups) : 'public or direct'
}

const byShipThenName = (a, b) => a.ship.localeCompare(b.ship) || a.name.localeCompare(b.name)

// how a member stands, as the agent reports it: answered, no-urgit (its
// gall nacked the request), unreachable (the timer ran out), pending (not
// asked, because an earlier request to it is still unacked) or waiting.
// an agent that predates the word is read from the flags it does send
export function discoveryStatus(discovery) {
  if (discovery.status) return discovery.status
  return discovery.active ? 'waiting' : discovery.ok ? 'answered' : 'unreachable'
}

const emptyCounts = () => ({ answered: 0, noUrgit: 0, unreachable: 0, pending: 0, waiting: 0 })
const countKey = { answered: 'answered', 'no-urgit': 'noUrgit', unreachable: 'unreachable', pending: 'pending', waiting: 'waiting' }

// when a pending entry's earlier request went out, as the agent reports
// it (ISO 8601); an agent that predates the word reports nothing
const heldSinceOf = (discovery) => {
  const when = Date.parse(discovery.heldSince || '')
  return Number.isFinite(when) ? when : null
}

// the discoveries one fan-out started, folded into one catalog.  a member
// still waiting keeps the fan-out open; every member is counted by how
// it stands; the ones that answered contribute their repositories; the
// oldest hold among the pending ones is kept, so the footer can say how
// long a member has gone unasked
export function mergeDiscoveries(discoveries, requests) {
  const wanted = new Set(requests || [])
  const mine = (discoveries || []).filter((item) => wanted.has(item.request))
  const entries = new Map()
  const counts = emptyCounts()
  let heldSince = null
  for (const discovery of mine) {
    const status = discoveryStatus(discovery)
    counts[countKey[status] || 'unreachable'] += 1
    if (status === 'pending') {
      const since = heldSinceOf(discovery)
      if (since !== null && (heldSince === null || since < heldSince)) heldSince = since
    }
    if (status !== 'answered' || !discovery.ok) continue
    for (const repo of discovery.repositories || []) {
      const key = `${discovery.ship}/${repo.name}`
      entries.set(key, { key, ship: discovery.ship, name: repo.name, writable: Boolean(repo.writable), via: repo.via || null })
    }
  }
  return { entries: [...entries.values()].sort(byShipThenName), counts, heldSince, pending: counts.waiting, settled: mine.length - counts.waiting }
}

// the footer under an open group: each count that is not zero, in the
// agent's order.  a pending member is not an error, only one not asked
export function countParts(counts) {
  const parts = []
  if (counts?.answered) parts.push({ key: 'answered', text: `${counts.answered} answered` })
  if (counts?.noUrgit) parts.push({ key: 'noUrgit', text: `${counts.noUrgit} without urgit` })
  if (counts?.unreachable) parts.push({ key: 'unreachable', text: `${counts.unreachable} unreachable` })
  if (counts?.pending) parts.push({ key: 'pending', text: `${counts.pending} pending` })
  return parts
}

export function describeCounts(counts) {
  return countParts(counts).map((part) => part.text).join(' · ')
}

// the tooltip on the pending count: how long the oldest hold has stood.
// the agent lets a hold go after an hour, so one older than that is a
// clock disagreement, not a member forgotten
export function describeHold(heldSince, now = Date.now()) {
  if (heldSince === null || heldSince === undefined) return ''
  const minutes = Math.floor(Math.max(0, now - heldSince) / 60_000)
  if (minutes < 1) return 'held less than a minute'
  if (minutes < 60) return `held ${minutes} min`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest ? `held ${hours} h ${rest} min` : `held ${hours} h`
}

// what the fan-out reached: every member asked, or the first 200 of them
export function describeReach(members, capped) {
  if (!members) return 'no other members'
  if (capped) return `asked 200 of ${members} members`
  return members === 1 ? 'asked 1 member' : `asked ${members} members`
}

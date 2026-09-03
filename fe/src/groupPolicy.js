// Group helpers for the Access settings panel and the Peers groups menu.
//
// Groups' light scry answers { "~host/slug": { meta: { title }, cabals: { id: { meta: { title } } } } };
// newer Groups versions name the role map `roles`. People know titles, Tlon
// mints the slugs and ids, so the panels show both and never ask anyone to type one.
// Every group the ship is in is listed, hosted here or joined; `ourShip` only
// tells the two apart, and two hosts may use the same slug, so groups are keyed by flag.

export function normalizeGroups(data, ourShip) {
  return Object.entries(data || {})
    .map(([flag, group]) => {
      const [host, slug = ''] = flag.split('/')
      const roleMap = group?.cabals || group?.roles || {}
      const roles = Object.entries(roleMap)
        .map(([id, role]) => ({ id, title: role?.meta?.title || id }))
        .sort((a, b) => a.title.localeCompare(b.title))
      return { flag, host, slug, title: group?.meta?.title || slug, roles, hostedHere: Boolean(ourShip) && host === ourShip }
    })
    .filter((group) => group.slug)
    .sort((a, b) => a.title.localeCompare(b.title) || a.flag.localeCompare(b.flag))
}

export const policyFlag = (policy) => (policy ? `${policy.host}/${policy.group}` : '')

export const findGroup = (groups, flag) => (groups || []).find((group) => group.flag === flag)

export const describeHost = (group) => (group.hostedHere ? 'hosted here' : `hosted by ${group.host}`)

// roles of the group not already mapped by another row; a row keeps its own role on offer
export function roleOptions(group, rows, own = '') {
  return (group?.roles || []).filter((role) => role.id === own || !rows.some((row) => row.role === role.id))
}

// `groups` is null until the Groups scry has answered; only a loaded list can call a group missing
export function describeGroup(groups, policy) {
  if (!policy) return ''
  const flag = policyFlag(policy)
  if (!groups) return flag
  const group = findGroup(groups, flag)
  return group ? `${group.title} (${flag}, ${describeHost(group)})` : `missing group \`${flag}\``
}

export function describeRole(group, id) {
  if (!group) return id
  const role = group.roles.find((entry) => entry.id === id)
  return role ? `${role.title} (${id})` : `missing role \`${id}\``
}

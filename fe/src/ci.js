export function validateCredentialValue(value) {
  if (/[\r\n]/.test(value)) throw new Error('Credential values must be a single line. Encode multiline material as base64.')
}

// Keep malformed lines visible, and leave all log text for React to escape.
export function parseLog(text) {
  const root = []
  const stack = [root]
  for (const [index, raw] of text.split(/\r?\n/).entries()) {
    if (!raw) continue
    let event
    try { event = JSON.parse(raw) } catch { event = { msg: raw } }
    if (!event || typeof event !== 'object') event = { msg: raw }
    const current = stack.at(-1)
    if (event.command === 'group') {
      const group = { id: index, group: String(event.arg ?? event.msg ?? 'Group'), children: [] }
      current.push(group)
      stack.push(group.children)
    } else if (event.command === 'endgroup') {
      if (stack.length > 1) stack.pop()
    } else {
      const job = event.jobID || event.job || 'runner'
      const step = event.step ? `${event.step}: ` : ''
      current.push({ id: index, text: `[${job}] ${step}${String(event.msg ?? '')}` })
    }
  }
  return root
}

export function jobRows(candidate) {
  const attempts = (candidate.attempts || []).filter((attempt) => attempt.kind === 'job')
  return (candidate.plan || []).flatMap((job) => {
    const found = attempts.filter((attempt) => attempt.workflow === job.workflow && attempt.job === job.id)
    return found.length ? found.map((attempt) => ({ ...job, attempt })) : [{ ...job, attempt: null }]
  })
}

export function ciAge(seconds, now = Date.now()) {
  const age = Math.max(0, Math.floor(now / 1000 - seconds))
  if (age < 60) return `${age}s ago`
  if (age < 3600) return `${Math.floor(age / 60)}m ago`
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`
  return `${Math.floor(age / 86400)}d ago`
}

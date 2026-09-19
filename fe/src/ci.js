// The CI tab's pure helpers (BRIEF-CI-P2 D6): the log renderer over the
// raw act jsonl the store serves, the shapes the candidate list and the
// candidate page derive from the ci/* routes, and the action bodies the
// settings controls post. Nothing here fetches; the components do.

export const shortOid = (oid) => (oid ? String(oid).slice(0, 8) : '—')

export const branchLabel = (ref) => (ref ? String(ref).replace('refs/heads/', '') : '—')

// the candidate statuses and the one word the list shows for each
export const statusLabel = {
  passed: 'passed',
  failed: 'failed',
  pending: 'pending',
  skipped: 'superseded',
  unknown: 'unknown',
  running: 'running',
  reoffered: 're-offered',
  'infrastructure-error': 'infra error',
}

// the Approve button's tooltip names the repository's policy for
// untrusted revisions (P3 D7)
export const approveHint = (policy) => policy === 'restricted'
  ? 'Policy: run restricted checks. This candidate ran with no credentials and cannot land; approving stages the same head as trusted and runs it again with credentials.'
  : 'Policy: wait for approval. An untrusted revision runs nothing until a writer approves it; approving stages the same head as trusted.'

// the first-run message (P3 D7): CI is required somewhere in this
// repository and no runner could take the work
export const noRunnerMessage = (protectedRefs = [], runners = []) =>
  protectedRefs.length > 0 && !runners.some((r) => r.enrolled && !r.revoked)
    ? 'No runner is enrolled. Mint a token in Settings → Runners and install the daemon; every candidate staged for a CI-required branch waits until one polls.'
    : ''

// the pips under a candidate: one per job attempt (plans are the ship's
// own step and are not shown), newest attempts last
export function attemptPips(attempts = []) {
  return attempts
    .filter((attempt) => attempt.kind === 'job')
    .slice()
    .reverse()
    .map((attempt) => ({ id: attempt.attempt, status: attempt.status, job: attempt.job || '' }))
}

// one row of the candidate list
export function candidateRow(candidate, nowSeconds = Date.now() / 1000) {
  const ageSeconds = Math.max(0, Math.floor(nowSeconds - Number(candidate.created || 0)))
  return {
    id: candidate.id,
    ref: branchLabel(candidate.ref),
    head: shortOid(candidate.head),
    actor: candidate.actor || '',
    status: candidate.status,
    trust: candidate.trust,
    pull: candidate.pull ?? null,
    reason: candidate.verdictReason || '',
    ageSeconds,
    pips: attemptPips(candidate.attempts || []),
  }
}

// a duration between two unix-second stamps, or '' while the second is
// missing
export function duration(started, finished) {
  const a = Number(started)
  const b = Number(finished)
  if (!Number.isFinite(a) || !Number.isFinite(b) || b < a || a <= 0) return ''
  const total = Math.floor(b - a)
  if (total < 60) return `${total}s`
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes < 60) return `${minutes}m ${seconds}s`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

// the candidate page's job rows: the plan attempts first (the ship's
// planning step), then every job attempt newest first, each with the
// fields the table shows, the job's runs-on from the plan (P3 D2b) and
// whether a log can be opened
export function attemptRows(attempts = [], plan = []) {
  const runsOn = new Map((Array.isArray(plan) ? plan : []).map((job) => [`${job.workflow}/${job.id}`, Array.isArray(job.runsOn) ? job.runsOn : []]))
  return attempts.map((attempt) => ({
    id: attempt.attempt,
    kind: attempt.kind,
    job: attempt.kind === 'plan' ? 'plan' : attempt.job || '',
    workflow: attempt.workflow || '',
    runsOn: attempt.kind === 'job' ? runsOn.get(`${attempt.workflow}/${attempt.job}`) || [] : [],
    daemon: attempt.daemon ? String(attempt.daemon).slice(0, 12) : (attempt.status === 'skipped' ? '—' : ''),
    status: attempt.status,
    trust: attempt.trust,
    started: attempt.started || null,
    finished: attempt.finished || null,
    elapsed: duration(attempt.started, attempt.finished),
    reason: attempt.reason || '',
    hasLog: Boolean(attempt.log),
  }))
}

// the raw act jsonl rendered client-side as `[job] step: msg` lines,
// grouped by the group/endgroup commands act emits; a line that is not
// an event line is shown as itself under no group. nothing is trusted:
// every field is coerced to text.
export function renderLog(text) {
  const groups = []
  let current = { title: '', lines: [] }
  const flush = () => {
    if (current.lines.length || current.title) groups.push(current)
    current = { title: '', lines: [] }
  }
  for (const raw of String(text || '').split('\n')) {
    if (!raw.trim()) continue
    let event = null
    try {
      event = raw[0] === '{' ? JSON.parse(raw) : null
    } catch {
      event = null
    }
    if (!event || typeof event !== 'object') {
      current.lines.push({ job: '', step: '', msg: raw, level: '' })
      continue
    }
    const command = typeof event.command === 'string' ? event.command : ''
    if (command === 'group') {
      flush()
      current = { title: String(event.arg ?? event.msg ?? ''), lines: [] }
      continue
    }
    if (command === 'endgroup') {
      flush()
      continue
    }
    current.lines.push({
      job: String(event.jobID ?? event.job ?? ''),
      step: String(event.step ?? ''),
      msg: String(event.msg ?? ''),
      level: String(event.level ?? ''),
      result: typeof event.jobResult === 'string' ? event.jobResult : typeof event.stepResult === 'string' ? event.stepResult : '',
    })
  }
  flush()
  return groups
}

export const lineText = (line) => `${line.job ? `[${line.job}] ` : ''}${line.step ? `${line.step}: ` : ''}${line.msg}`

// the bodies POST ci/action takes: the poke as JSON, one shape per action
export const ciActions = {
  approve: (id) => ({ action: 'approve-candidate', id }),
  rerun: (id) => ({ action: 'rerun-candidate', id }),
  setCiProtected: (repo, ref, protectedRef) => ({ action: 'set-ci-protected', repo, ref, protected: Boolean(protectedRef) }),
  setUntrustedPolicy: (repo, policy) => ({ action: 'set-untrusted-policy', repo, policy }),
  setCredential: (repo, name, value, scope, envs = []) => ({ action: 'set-credential', repo, name, value, scope, envs }),
  deleteCredential: (repo, name) => ({ action: 'delete-credential', repo, name }),
}

// a credential form's validity: a name, a value of at least eight
// characters (the ship's rule), and environments only for %env
export function credentialFormError({ name, value, scope, envs }) {
  if (!String(name || '').trim()) return 'A name is required.'
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(String(name).trim())) return 'Names are letters, digits and underscores, like GITHUB_TOKEN.'
  if (String(value || '').length < 8) return 'Values must be at least 8 characters.'
  if (/[\r\n]/.test(String(value || ''))) return 'Values must be a single line.'
  if (scope === 'env' && !parseEnvs(envs).length) return 'Name at least one environment for the env scope.'
  return ''
}

export const parseEnvs = (text) => String(text || '').split(/[\s,]+/).map((s) => s.trim()).filter(Boolean)

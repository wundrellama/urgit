import { useCallback, useEffect, useState } from 'react'
import { ci } from '../api'
import { attemptRows, candidateRow, ciActions, lineText, renderLog, statusLabel } from '../ci'
import { exactTime, relativeTime } from '../format'

// The repository page's CI tab (BRIEF-CI-P2 D6), read-first: the
// candidate list (ref, head, actor, status, age, attempt pips), the
// candidate page (plan jobs as rows with a log link when the ship
// recorded one, the verdict reason, the landing result) and the log
// view, rendered client-side from the raw jsonl the store serves.
// Approve and re-run are the two actions the page offers.

const statusClass = (status) => ({ passed: 'good', failed: 'bad', 'infrastructure-error': 'bad', unknown: 'warn', running: 'warn', pending: '' })[status] || ''

function Pips({ pips }) {
  if (!pips.length) return <small className="quiet">no jobs yet</small>
  return <span className="ci-pips">{pips.map((pip) => <span key={pip.id} className={`ci-pip ${statusClass(pip.status)}`} title={`${pip.job}: ${statusLabel[pip.status] || pip.status}`} />)}</span>
}

function CandidateList({ repo, onOpen, refreshKey }) {
  const [rows, setRows] = useState(null)
  const [error, setError] = useState('')
  const [cursor, setCursor] = useState('')
  const [more, setMore] = useState(false)
  const load = useCallback(async (before) => {
    try {
      const answer = await ci.candidates(repo.name, before)
      const now = Date.now() / 1000
      const next = (answer.candidates || []).map((candidate) => candidateRow(candidate, now))
      setRows((current) => before && current ? [...current, ...next] : next)
      setMore(Boolean(answer.more))
      setCursor(answer.candidates?.length ? answer.candidates[answer.candidates.length - 1].id : '')
      setError('')
    } catch (cause) {
      setError(cause.message)
    }
  }, [repo.name])
  useEffect(() => { load('') }, [load, refreshKey])
  if (error) return <div className="empty">CI unavailable: {error}</div>
  if (!rows) return <div className="empty">Loading candidates…</div>
  if (!rows.length) return <div className="empty">No CI candidates yet. Protect a branch with CI required in Settings, then push or merge to it.</div>
  return (
    <div className="ci-list">
      <div className="table-head ci-row"><span>Ref</span><span>Head</span><span>Actor</span><span>Status</span><span>Age</span><span>Jobs</span></div>
      {rows.map((row) => (
        <button type="button" className="ci-row ci-candidate" key={row.id} onClick={() => onOpen(row.id)}>
          <span><strong>{row.ref}</strong>{row.pull != null && <small className="quiet"> · PR #{row.pull}</small>}</span>
          <code>{row.head}</code>
          <span>{row.actor}{row.trust === 'untrusted' && <small className="quiet"> · untrusted</small>}</span>
          <span className={`status ${statusClass(row.status)}`}>{statusLabel[row.status] || row.status}</span>
          <span title={row.reason}>{relativeTime(Date.now() / 1000 - row.ageSeconds)}</span>
          <Pips pips={row.pips} />
        </button>
      ))}
      {more && <div className="form-actions"><button className="text-button" onClick={() => load(cursor)}>Older candidates</button></div>}
    </div>
  )
}

function LogView({ attempt, onClose }) {
  const [groups, setGroups] = useState(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    ci.log(attempt).then((text) => { if (live) setGroups(renderLog(text)) }).catch((cause) => { if (live) setError(cause.message) })
    return () => { live = false }
  }, [attempt])
  return (
    <div className="ci-log">
      <div className="form-actions split"><strong>Log of attempt <code>{attempt}</code></strong><div><a className="text-button" href={ci.logUrl(attempt)} target="_blank" rel="noreferrer">Raw</a> <button className="text-button" onClick={onClose}>Close</button></div></div>
      {error && <div className="empty">Log unavailable: {error}</div>}
      {!error && !groups && <div className="empty">Loading log…</div>}
      {groups && groups.map((group, index) => (
        <details key={index} open={!group.title || index === groups.length - 1}>
          <summary>{group.title || 'output'} <small className="quiet">{group.lines.length} lines</small></summary>
          <pre className="ci-log-lines">{group.lines.map((line, at) => <span key={at} className={line.result ? `ci-line ${statusClass(line.result === 'success' ? 'passed' : 'failed')}` : 'ci-line'}>{lineText(line)}{'\n'}</span>)}</pre>
        </details>
      ))}
    </div>
  )
}

function CandidatePage({ id, onBack, onMutate }) {
  const [data, setData] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [log, setLog] = useState('')
  const load = useCallback(async () => {
    try { setData(await ci.candidate(id)); setError('') } catch (cause) { setError(cause.message) }
  }, [id])
  useEffect(() => { load() }, [load])
  useEffect(() => {
    if (!data || !['pending'].includes(data.candidate?.status)) return undefined
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [data, load])
  async function act(label, body) {
    setBusy(label); setError('')
    try { await ci.action(body); await load(); await onMutate?.() } catch (cause) { setError(cause.message) } finally { setBusy('') }
  }
  if (error && !data) return <div className="empty">{error} <button className="text-button" onClick={onBack}>Back</button></div>
  if (!data) return <div className="empty">Loading candidate…</div>
  const c = data.candidate
  const rows = attemptRows(data.attempts || [])
  return (
    <div className="ci-candidate-page">
      <div className="form-actions split"><button className="text-button" onClick={onBack}>← Candidates</button><span className={`status ${statusClass(c.status)}`}>{statusLabel[c.status] || c.status}</span></div>
      <dl className="ci-facts">
        <div><dt>Ref</dt><dd>{c.ref}{c.pull != null && <> · pull request #{c.pull}</>}</dd></div>
        <div><dt>Head</dt><dd><code>{c.head}</code></dd></div>
        <div><dt>Base</dt><dd><code>{c.base}</code></dd></div>
        <div><dt>Candidate</dt><dd><code>{c.candidate || 'not materialized'}</code></dd></div>
        <div><dt>Actor</dt><dd>{c.actor} · {c.via} · {c.trust}</dd></div>
        <div><dt>Staged</dt><dd>{exactTime(c.created)}</dd></div>
        {c.verdictReason && <div><dt>Verdict</dt><dd>{c.verdictReason}</dd></div>}
      </dl>
      {error && <small className="field-error">{error}</small>}
      <div className="form-actions">
        {c.trust === 'untrusted' && c.status !== 'skipped' && <button className="button primary" disabled={busy !== ''} onClick={() => act('approve', ciActions.approve(c.id))}>{busy === 'approve' ? 'Approving…' : 'Approve and run trusted'}</button>}
        {c.status !== 'pending' && <button className="button" disabled={busy !== ''} onClick={() => act('rerun', ciActions.rerun(c.id))}>{busy === 'rerun' ? 'Staging…' : 'Re-run'}</button>}
      </div>
      <div className="ci-list">
        <div className="table-head ci-attempt-row"><span>Job</span><span>Daemon</span><span>Status</span><span>Started</span><span>Finished</span><span>Log</span></div>
        {rows.map((row) => (
          <div className="ci-attempt-row" key={row.id} title={row.reason}>
            <span><strong>{row.job}</strong>{row.workflow && <small className="quiet"> {row.workflow}</small>}</span>
            <code>{row.daemon}</code>
            <span className={`status ${statusClass(row.status)}`}>{statusLabel[row.status] || row.status}{row.reason && <small className="quiet"> · {row.reason}</small>}</span>
            <span>{row.started ? exactTime(row.started) : '—'}</span>
            <span>{row.finished ? `${exactTime(row.finished)}${row.elapsed ? ` (${row.elapsed})` : ''}` : '—'}</span>
            <span>{row.hasLog ? <button className="text-button" onClick={() => setLog(row.id)}>log</button> : <small className="quiet">—</small>}</span>
          </div>
        ))}
        {!rows.length && <div className="empty">No attempts yet{c.trust === 'untrusted' ? ': an untrusted revision waits for approval unless the repository runs restricted checks.' : '.'}</div>}
      </div>
      {log && <LogView attempt={log} onClose={() => setLog('')} />}
    </div>
  )
}

export default function CiTab({ repo, onMutate }) {
  const [open, setOpen] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)
  useEffect(() => { setOpen('') }, [repo.name])
  return (
    <section className="panel ci-tab">
      <div className="section-title"><div><h2>CI</h2><p>Candidates staged for the CI-protected branches of this repository, newest first. A candidate lands its branch when every required job passes.</p></div>{!open && <button className="text-button" onClick={() => setRefreshKey((k) => k + 1)}>Refresh</button>}</div>
      {open ? <CandidatePage id={open} onBack={() => { setOpen(''); setRefreshKey((k) => k + 1) }} onMutate={onMutate} /> : <CandidateList repo={repo} onOpen={setOpen} refreshKey={refreshKey} />}
    </section>
  )
}

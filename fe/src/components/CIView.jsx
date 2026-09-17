import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { ciAge, jobRows, parseLog } from '../ci'

const date = (at) => at ? new Date(at * 1000).toLocaleString() : '—'
const statusLabel = (value) => value === 'infrastructure-error' ? 'Infrastructure error' : value

function Status({ value }) {
  return <span className={`ci-status ci-status-${value}`}>{statusLabel(value)}</span>
}

function LogLines({ lines }) {
  return lines.map((line) => line.children
    ? <details className="ci-log-group" key={line.id} open><summary>{line.group}</summary><LogLines lines={line.children} /></details>
    : <div className="ci-log-line" key={line.id}>{line.text}</div>)
}

function LogView({ attempt, onBack }) {
  const [text, setText] = useState(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    setText(null)
    setError('')
    api.ciLog(attempt.attempt, controller.signal).then(setText).catch((cause) => {
      if (!controller.signal.aborted) setError(cause.message)
    })
    return () => controller.abort()
  }, [attempt.attempt])
  const lines = useMemo(() => text === null ? [] : parseLog(text), [text])
  return <section className="panel ci-log-panel">
    <div className="section-title"><div><button className="text-button" onClick={onBack}>← Jobs</button><h2>{attempt.workflow} / {attempt.job || 'Plan'}</h2></div><a href={api.ciLogUrl(attempt.attempt)} target="_blank" rel="noreferrer">Raw log ↗</a></div>
    {error ? <p className="field-error" role="alert">{error}</p> : text === null ? <p className="quiet">Loading log…</p> : <div className="ci-log" aria-label="Attempt log"><LogLines lines={lines} />{!lines.length && <span className="quiet">The log is empty.</span>}</div>}
  </section>
}

function JobTable({ candidate, onLog }) {
  const rows = jobRows(candidate)
  const planners = candidate.attempts.filter((attempt) => attempt.kind === 'plan')
  return <>
    {planners.map((attempt) => <p className="ci-planner" key={attempt.attempt}>Workflow plan <Status value={attempt.status} />{attempt.reason && <span>{attempt.reason}</span>}</p>)}
    {rows.length ? <div className="ci-table-scroll"><table className="ci-jobs"><thead><tr><th>Job</th><th>Runner</th><th>Status</th><th>Started</th><th>Finished</th><th>Duration</th><th>Log</th></tr></thead><tbody>
      {rows.map(({ workflow, id, attempt }) => <tr key={attempt?.attempt || `${workflow}/${id}`}>
        <td><strong>{id}</strong><small>{workflow}</small>{attempt?.reason && <small className="field-error">{attempt.reason}</small>}</td>
        <td><code title={attempt?.daemon}>{attempt?.daemon || '—'}</code></td>
        <td><Status value={attempt?.status || 'pending'} /></td><td>{date(attempt?.started)}</td><td>{date(attempt?.finished)}</td><td>{attempt ? `${attempt.duration}s` : '—'}</td>
        <td>{attempt?.log ? <button className="text-button" onClick={() => onLog(attempt.attempt)}>Log</button> : <span className="quiet">{attempt?.finished ? 'Unavailable' : '—'}</span>}</td>
      </tr>)}
    </tbody></table></div> : <p className="quiet">{candidate.trust === 'untrusted' && candidate.status === 'pending' ? 'Awaiting approval or restricted checks.' : 'No jobs have been planned.'}</p>}
  </>
}

export default function CIView({ repository, candidateId, logId, onNavigate }) {
  const [before, setBefore] = useState('')
  const [data, setData] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  const refresh = () => setRevision((value) => value + 1)
  const load = useCallback(() => candidateId ? api.ciCandidate(candidateId) : api.ciCandidates(repository, before), [repository, candidateId, before])
  useEffect(() => { setBefore('') }, [repository])
  useEffect(() => {
    let active = true
    let timer
    setData(null)
    setError('')
    const read = async () => {
      try {
        const next = await load()
        if (!active) return
        if (candidateId && next.repository !== repository) throw new Error('Candidate belongs to another repository.')
        setData(next)
        setError('')
        if (!candidateId || next.status === 'pending' || (next.status === 'passed' && !next.verdictReason)) timer = setTimeout(read, 5000)
      } catch (cause) { if (active) setError(cause.message) }
    }
    read()
    return () => { active = false; clearTimeout(timer) }
  }, [load, repository, candidateId, revision])

  async function act(action) {
    setBusy(true)
    setError('')
    try {
      const result = await api.ciAction({ action, id: candidateId })
      onNavigate({ ciCandidate: result.candidate, ciLog: '' })
      refresh()
    } catch (cause) { setError(cause.message) } finally { setBusy(false) }
  }

  if (candidateId) {
    const candidate = data?.id === candidateId ? data : null
    const attempt = candidate?.attempts.find((entry) => entry.attempt === logId)
    return <div className="ci-view">
      <div className="section-title"><button className="text-button" onClick={() => onNavigate({ ciCandidate: '', ciLog: '' })}>← Candidates</button><button className="text-button" onClick={refresh}>Refresh</button></div>
      {error && <p className="field-error" role="alert">{error}</p>}
      {!candidate ? !error && <p className="quiet">Loading candidate…</p> : <>
        <section className="panel">
          <div className="section-title"><div><h2>{candidate.ref.replace('refs/heads/', '')} · {candidate.head.slice(0, 8)}</h2><p>{candidate.actor} · {candidate.trust} · <time title={date(candidate.created)}>{ciAge(candidate.created)}</time></p></div><Status value={candidate.status} /></div>
          <p className="ci-verdict">{candidate.landed ? 'Landed' : candidate.verdictReason || (candidate.status === 'pending' ? 'Checks pending' : 'Not landed')}</p>
          {candidate.verdictReason && candidate.landed && <small className="quiet">{candidate.candidate}</small>}
          <div className="form-actions">
            {candidate.trust === 'untrusted' && candidate.status !== 'skipped' && <button className="button primary" disabled={busy} onClick={() => act('approve-candidate')}>Approve revision</button>}
            {candidate.status !== 'skipped' && <button className="button" disabled={busy} onClick={() => act('rerun-candidate')}>Rerun checks</button>}
          </div>
          {!logId && <JobTable candidate={candidate} onLog={(id) => onNavigate({ ciLog: id })} />}
        </section>
        {logId && (attempt ? <LogView attempt={attempt} onBack={() => onNavigate({ ciLog: '' })} /> : <p className="field-error">Attempt not found in this candidate.</p>)}
      </>}
    </div>
  }
  const listing = Array.isArray(data?.candidates) ? data : null
  return <section className="panel ci-view">
    <div className="section-title"><div><h2>CI candidates</h2><p>Checks for revisions staged on CI-required branches.</p></div><button className="text-button" onClick={refresh}>Refresh</button></div>
    {error && <p className="field-error" role="alert">{error}</p>}
    {!listing ? !error && <p className="quiet">Loading candidates…</p> : <>
      {!listing.candidates.length && <p className="quiet">No candidates yet.</p>}
      <div className="ci-candidates">{listing.candidates.map((candidate) => <button className="ci-candidate" key={candidate.id} onClick={() => onNavigate({ ciCandidate: candidate.id, ciLog: '' })}>
        <span><strong>{candidate.ref.replace('refs/heads/', '')} · {candidate.head.slice(0, 8)}</strong><small>{candidate.actor} · {candidate.trust} · {ciAge(candidate.created)}</small></span>
        <span className="ci-pips" aria-label="Job statuses">{jobRows(candidate).map(({ workflow, id, attempt }, index) => <span key={index} className={`ci-pip ci-status-${attempt?.status || 'pending'}`} title={`${workflow}/${id}: ${attempt?.status || 'pending'}`} />)}</span>
        <Status value={candidate.status} />
      </button>)}</div>
      <div className="form-actions split">{before ? <button className="text-button" onClick={() => setBefore('')}>Newest</button> : <span />}{listing.next && <button className="button" onClick={() => setBefore(listing.next)}>Older candidates →</button>}</div>
    </>}
  </section>
}

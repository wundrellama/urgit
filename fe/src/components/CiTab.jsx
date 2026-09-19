import { useCallback, useEffect, useRef, useState } from 'react'
import { ci, publicApi } from '../api'
import { approveHint, attemptRows, candidateRow, ciActions, lineText, noRunnerMessage, renderLog, statusLabel } from '../ci'
import { applyCandidateFactToPage, feedPip, mergeCandidateFact, mergeRunnerCount } from '../ciLive'
import { probeClass, probeMessage, probeStore } from '../storageProbe'
import { watchAgent } from '../channel'
import { exactTime, relativeTime } from '../format'
import SetupGuide from './SetupGuide'

// The repository page's CI tab (BRIEF-CI-P2 D6; P3 D4/D7), read-first
// and then live: the candidate list (ref, head, actor, status, age,
// attempt pips), the candidate page (plan jobs as rows with runs-on and a
// log link when the ship recorded one, the verdict reason, the landing
// result) and the log view, rendered client-side from the raw jsonl the
// store serves. The tab subscribes to %urgit-ci's /ci/repository/<name>
// through Eyre's channel (P3 D4): the first fact is the list, every later
// one a changed candidate's full row, replaced by id; Refresh and a
// polling timer carry the tab while the channel is down, and a pip says
// which. Approve and re-run are the two actions the page offers.

const statusClass = (status) => ({ passed: 'good', failed: 'bad', 'infrastructure-error': 'bad', unknown: 'warn', running: 'warn', reoffered: 'warn', pending: '' })[status] || ''

function Pips({ pips }) {
  if (!pips.length) return <small className="quiet">no jobs yet</small>
  return <span className="ci-pips">{pips.map((pip) => <span key={pip.id} className={`ci-pip ${statusClass(pip.status)}`} title={`${pip.job}: ${statusLabel[pip.status] || pip.status}`} />)}</span>
}

// the storage pip (P3 D5): the ship names the store's endpoint and a probe
// URL; this browser fetches it and says whether logs will open here
export function useStorageProbe() {
  const [probe, setProbe] = useState({ state: 'unknown', host: '' })
  const run = useCallback(async () => {
    try {
      const answer = await ci.storageProbe()
      if (!answer.configured) { setProbe({ state: 'unconfigured', host: '' }); return 'unconfigured' }
      const state = await probeStore(answer.url)
      setProbe({ state, host: answer.host || '' })
      return state
    } catch (cause) { setProbe({ state: 'unknown', host: '', error: cause.message }); return 'unknown' }
  }, [])
  useEffect(() => { run() }, [run])
  return { ...probe, run }
}

export function StoragePip({ probe }) {
  const text = probe.state === 'unknown' && probe.error ? `Storage check unavailable: ${probe.error}` : probeMessage(probe.state, probe.host)
  return <span className={`ci-storage status ${probeClass(probe.state)}`} title={text}><span className={`ci-pip ${probeClass(probe.state)}`} /> store {probe.host ? <code>{probe.host}</code> : probe.state}</span>
}

// the ship's own name, for the channel subscription
function useShip() {
  const [ship, setShip] = useState('')
  useEffect(() => { let live = true; publicApi.profile().then((p) => { if (live) setShip(p.ship || '') }).catch(() => {}); return () => { live = false } }, [])
  return ship
}

// the live feed for one repository: the raw candidate list, the runner
// list, the channel status; a read on demand (Refresh, the polling
// fallback) merges the same way a fact does
function useRepositoryFeed(repoName) {
  const ship = useShip()
  const [raw, setRaw] = useState(null)
  const [runners, setRunners] = useState([])
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')
  const [more, setMore] = useState(false)
  const load = useCallback(async (before) => {
    try {
      const answer = await ci.candidates(repoName, before)
      setRaw((current) => before && current ? [...current, ...(answer.candidates || [])] : (answer.candidates || []))
      setMore(Boolean(answer.more))
      if (!before) {
        try { const r = await ci.runners(); setRunners(r.runners || []) } catch { /* the message needs it; the list does not */ }
      }
      setError('')
    } catch (cause) { setError(cause.message) }
  }, [repoName])
  useEffect(() => { setRaw(null); setStatus(''); load('') }, [load])
  useEffect(() => {
    if (!ship) return undefined
    const channel = watchAgent({
      ship,
      app: 'urgit-ci',
      path: `/ci/repository/${repoName}`,
      onFact: (fact) => {
        setRaw((current) => mergeCandidateFact(current, fact))
        setRunners((current) => mergeRunnerCount(current, fact))
        if (fact?.kind === 'candidates') { setMore(Boolean(fact.more)); setError('') }
      },
      onStatus: setStatus,
    })
    return () => { channel.close(); setStatus('') }
  }, [ship, repoName])
  // while the channel is down the list polls, as the App does for transfers
  useEffect(() => {
    if (status === 'open') return undefined
    const timer = setInterval(() => load(''), 10000)
    return () => clearInterval(timer)
  }, [status, load])
  return { raw, runners, status, error, more, load }
}

function CandidateList({ feed, onOpen }) {
  const { raw, error, more, load } = feed
  const rows = raw ? raw.map((candidate) => candidateRow(candidate, Date.now() / 1000)) : null
  const cursor = raw?.length ? raw[raw.length - 1].id : ''
  if (error && !rows) return <div className="empty">CI unavailable: {error}</div>
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
          <span className={`status ${statusClass(row.status)}`} title={row.reason}>{statusLabel[row.status] || row.status}{row.status === 'pending' && row.reason && <small className="quiet"> · {row.reason}</small>}</span>
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
      {error && <div className="empty">Log unavailable: {error}{/NetworkError|Failed to fetch/i.test(error) && <small className="quiet"> — the object store the ship signed a link into is not reachable from this browser; see the storage pip at the top of the tab.</small>}</div>}
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

function CandidatePage({ id, live, policy, onBack, onMutate }) {
  const [data, setData] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [log, setLog] = useState('')
  const load = useCallback(async () => {
    try { setData(await ci.candidate(id)); setError('') } catch (cause) { setError(cause.message) }
  }, [id])
  useEffect(() => { load() }, [load])
  // the feed's row for this candidate refreshes the page as it changes
  useEffect(() => {
    if (!live) return
    setData((current) => current ? applyCandidateFactToPage(current, { kind: 'candidate', id: live.id, patch: live }) : current)
  }, [live])
  async function act(label, body) {
    setBusy(label); setError('')
    try { await ci.action(body); await load(); await onMutate?.() } catch (cause) { setError(cause.message) } finally { setBusy('') }
  }
  if (error && !data) return <div className="empty">{error} <button className="text-button" onClick={onBack}>Back</button></div>
  if (!data) return <div className="empty">Loading candidate…</div>
  const c = data.candidate
  const rows = attemptRows(data.attempts || [], c.plan || [])
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
        {c.verdictReason && <div><dt>{c.status === 'pending' ? 'Waiting' : 'Verdict'}</dt><dd>{c.verdictReason}</dd></div>}
      </dl>
      {error && <small className="field-error">{error}</small>}
      <div className="form-actions">
        {c.trust === 'untrusted' && c.status !== 'skipped' && <button className="button primary" disabled={busy !== ''} title={approveHint(policy)} onClick={() => act('approve', ciActions.approve(c.id))}>{busy === 'approve' ? 'Approving…' : 'Approve and run trusted'}</button>}
        {c.status !== 'pending' && <button className="button" disabled={busy !== ''} onClick={() => act('rerun', ciActions.rerun(c.id))}>{busy === 'rerun' ? 'Staging…' : 'Re-run'}</button>}
      </div>
      <div className="ci-list">
        <div className="table-head ci-attempt-row"><span>Job</span><span>Daemon</span><span>Status</span><span>Started</span><span>Finished</span><span>Log</span></div>
        {rows.map((row) => (
          <div className="ci-attempt-row" key={row.id} title={row.reason}>
            <span><strong>{row.job}</strong>{row.workflow && <small className="quiet"> {row.workflow}</small>}{row.runsOn.length > 0 && <small className="quiet ci-runs-on" title="runs-on"> · {row.runsOn.join(', ')}</small>}</span>
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
  const [guide, setGuide] = useState(false)
  const [policy, setPolicy] = useState(null)
  const feed = useRepositoryFeed(repo.name)
  const probe = useStorageProbe()
  const pip = feedPip(feed.status)
  useEffect(() => { setOpen('') }, [repo.name])
  useEffect(() => { let live = true; ci.policy(repo.name).then((p) => { if (live) setPolicy(p) }).catch(() => {}); return () => { live = false } }, [repo.name])
  const firstRun = noRunnerMessage(policy?.ciProtected || [], feed.runners)
  const liveCandidate = open ? (feed.raw || []).find((c) => c.id === open) || null : null
  return (
    <section className="panel ci-tab">
      <div className="section-title">
        <div><h2>CI</h2><p>Candidates staged for the CI-protected branches of this repository, newest first. A candidate lands its branch when every required job passes.</p></div>
        <div className="ci-tab-tools">
          <StoragePip probe={probe} />
          <span className={`ci-feed ci-feed-${pip}`} title={pip === 'live' ? 'Subscribed: the ship pushes every change.' : pip === 'polling' ? 'The channel is down: the list re-reads every 10 s; Refresh reads now.' : 'Connecting to the ship.'}><span className={`ci-pip ${pip === 'live' ? 'good' : pip === 'polling' ? 'warn' : ''}`} /> {pip}</span>
          {!open && <button className="text-button" onClick={() => { feed.load(''); probe.run() }}>Refresh</button>}
        </div>
      </div>
      {(probe.state === 'unreachable' || probe.state === 'cors') && <div className="empty ci-first-run ci-storage-warning">{probeMessage(probe.state, probe.host)}</div>}
      {firstRun && <div className="empty ci-first-run">{firstRun} <button type="button" className="text-button" onClick={() => setGuide(true)}>Setup guide</button></div>}
      {open ? <CandidatePage id={open} live={liveCandidate} policy={policy?.untrusted} onBack={() => { setOpen(''); feed.load('') }} onMutate={onMutate} /> : <CandidateList feed={feed} onOpen={setOpen} />}
      {guide && <SetupGuide onClose={() => setGuide(false)} />}
    </section>
  )
}

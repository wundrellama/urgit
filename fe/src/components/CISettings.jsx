import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { validateCredentialValue } from '../ci'

export default function CISettings({ repo }) {
  const [policy, setPolicy] = useState(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [scope, setScope] = useState('job')
  const [envs, setEnvs] = useState('')
  const password = useRef(null)
  useEffect(() => {
    let active = true
    setPolicy(null)
    setError('')
    api.ciPolicy(repo.name).then((value) => active && setPolicy(value)).catch((cause) => active && setError(cause.message))
    return () => { active = false }
  }, [repo.name])
  async function act(body) {
    setBusy(true)
    setError('')
    try {
      await api.ciAction({ repo: repo.name, ...body })
      setPolicy(await api.ciPolicy(repo.name))
      return true
    } catch (cause) { setError(cause.message); return false } finally { setBusy(false) }
  }
  async function add(event) {
    event.preventDefault()
    const value = password.current.value
    password.current.value = ''
    try { validateCredentialValue(value) } catch (cause) { setError(cause.message); return }
    if (await act({ action: 'set-credential', name, value, scope, envs: scope === 'env' ? [...new Set(envs.split(',').map((env) => env.trim()).filter(Boolean))] : [] })) {
      setName(''); setEnvs('')
    }
  }
  return <div className="ci-settings">
    {error && <p className="field-error" role="alert">{error}</p>}
    {!policy ? !error && <p className="quiet">Loading CI settings…</p> : <>
      <p>CI-required branches stage updates for checks before landing.</p>
      <div className="branch-policy-list">{(repo.refs || []).filter((entry) => entry.name.startsWith('refs/heads/')).map((entry) => <label className="check-row compact" key={entry.name}>
        <input type="checkbox" aria-label={`CI required: ${entry.name.replace('refs/heads/', '')}`} checked={policy.protectedRefs.includes(entry.name)} disabled={busy} onChange={(event) => act({ action: 'set-ci-protected', ref: entry.name, protected: event.target.checked })} />
        <span><strong>{entry.name.replace('refs/heads/', '')}</strong><small>CI required</small></span>
      </label>)}</div>
      <fieldset className="ci-policy"><legend>Untrusted revisions</legend>
        <label className="check-row"><input type="radio" name="ci-untrusted" checked={policy.untrusted === 'approval'} disabled={busy} onChange={() => act({ action: 'set-untrusted-policy', policy: 'approval' })} /><span>Require approval before running</span></label>
        <label className="check-row"><input type="radio" name="ci-untrusted" checked={policy.untrusted === 'restricted'} disabled={busy} onChange={() => act({ action: 'set-untrusted-policy', policy: 'restricted' })} /><span>Run restricted checks automatically<small>No credentials; approval is still required to land.</small></span></label>
      </fieldset>
      <h3>Credentials</h3>
      <p>Trusted jobs receive only credentials in their scope.</p>
      <div className="ci-credentials">{policy.credentials.map((credential) => <div key={credential.name}><span><strong>{credential.name}</strong><small>{credential.scope === 'job' ? 'All trusted jobs' : `Environments: ${credential.envs.join(', ') || 'none'}`}</small></span><button className="text-button danger-text" disabled={busy} onClick={() => act({ action: 'delete-credential', name: credential.name })} aria-label={`Delete credential ${credential.name}`}>Delete</button></div>)}</div>
      {!policy.credentials.length && <p className="quiet">No credentials stored.</p>}
      <form className="ci-credential-form" onSubmit={add} autoComplete="off">
        <label><span>Name</span><input aria-label="Credential name" value={name} onChange={(event) => setName(event.target.value)} pattern="[A-Za-z_][A-Za-z0-9_]*" required /></label>
        <label><span>Value</span><input aria-label="Credential value" type="password" ref={password} autoComplete="new-password" required onPaste={(event) => {
          try { validateCredentialValue(event.clipboardData.getData('text')) } catch (cause) { event.preventDefault(); setError(cause.message) }
        }} /><small>Single line only. Encode multiline material as base64.</small></label>
        <label><span>Scope</span><select aria-label="Credential scope" value={scope} onChange={(event) => setScope(event.target.value)}><option value="job">All trusted jobs</option><option value="env">Named environments</option></select></label>
        {scope === 'env' && <label><span>Environments</span><input aria-label="Credential environments" value={envs} onChange={(event) => setEnvs(event.target.value)} placeholder="production, staging" required /></label>}
        <button className="button" disabled={busy} type="submit">Add credential</button>
      </form>
    </>}
  </div>
}

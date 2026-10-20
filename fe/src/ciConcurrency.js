import { provenanceActions } from './ciProvenance.js'

export const canCancelRun = (candidate = {}) => candidate.canCancel === true && ['pending', 'running'].includes(candidate.status)

export function workflowRows(candidate = {}) {
  return (candidate.workflows || []).map((w) => {
    const group = w.group == null ? '' : String(w.group)
    const related = w.relatedName || 'another run'
    let detail = String(w.reason || '')
    if (w.state === 'waiting') detail = `Waiting for ${related} in group ${group}.`
    if (w.state === 'cancelled' && w.relatedRun) detail = w.reason?.includes('cancel-in-progress')
      ? `Cancelled: ${related} joined group ${group} with cancel-in-progress.`
      : `Cancelled: a newer run, ${related}, is waiting in group ${group}.`
    return { ...w, name: w.name || w.workflow, group, detail,
      canCancel: canCancelRun(candidate) && ['running', 'waiting', 'unknown'].includes(w.state) }
  })
}

// Shared production renderer: callbacks post to the authority-checked action.
// Text is always rendered as text, including group names from untrusted YAML.
export function renderConcurrency(h, candidate, { busy = false, onCancel = () => {} } = {}) {
  const rows = workflowRows(candidate)
  return h('section', { className: 'ci-concurrency', 'aria-label': 'Workflow concurrency' },
    h('div', { className: 'ci-concurrency-header' },
      h('strong', null, candidate.runName || 'Workflow run'),
      canCancelRun(candidate) && h('button', { type: 'button', className: 'button danger', disabled: busy,
        onClick: () => onCancel(provenanceActions.cancelWorkflow(candidate.id)) }, 'Cancel run')),
    candidate.concurrencyNote && h('p', { className: 'quiet' }, candidate.concurrencyNote),
    rows.map((w) => h('article', { className: 'ci-concurrency-workflow', key: w.workflow },
      h('div', { className: 'ci-concurrency-header' }, h('strong', null, w.name),
        h('span', { className: 'status', 'data-state': w.state }, w.state),
        w.canCancel && h('button', { type: 'button', className: 'button', disabled: busy,
          onClick: () => onCancel(provenanceActions.cancelWorkflow(candidate.id, w.workflow)) }, `Cancel ${w.name}`)),
      h('p', null, w.detail),
      w.group !== '' && h('p', null, 'Group: ', h('code', null, w.group)),
      h('small', { className: 'quiet' }, w.workflow,
        w.relatedRun ? ` · Related run ID: ${w.relatedRun}` : ''))),
    !candidate.canCancel && candidate.status === 'pending' && h('small', { className: 'quiet' }, 'Only the repository owner or a holder of ci-policy can cancel this run.'))
}

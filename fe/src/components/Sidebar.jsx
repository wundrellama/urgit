import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import { countParts, describeGrant, describeHold, describeReach, mergeDiscoveries } from '../groupDiscovery'
import { describeHost, filterGroups } from '../groupPolicy'
import { GitIcon, PlusIcon } from './Icons'

const idleCatalog = { loading: false, entries: [], counts: null, heldSince: null, pending: 0, settled: 0, members: 0, capped: false, error: '' }

export default function Sidebar({ repositories, peers, ourShip, selected, remoteSelected, onSelect, onSelectRemote, onCreate, onPeersChanged }) {
  const [query, setQuery] = useState('')
  const [addingPeer, setAddingPeer] = useState(false)
  const [peerName, setPeerName] = useState('')
  const [sectionsOpen, setSectionsOpen] = useState({ repositories: true, peers: true, groups: true })
  const [expanded, setExpanded] = useState({})
  const [catalogs, setCatalogs] = useState({})
  const [peerError, setPeerError] = useState('')
  // every group this ship is in, from Groups; null until it answers, and
  // the error stands in for the list when it cannot
  const [groups, setGroups] = useState(null)
  const [groupsError, setGroupsError] = useState('')
  // per group flag, like catalogs per peer: whether it is open, what its
  // members answered, and which fan-out is the current one
  const [groupsOpen, setGroupsOpen] = useState({})
  const [groupCatalogs, setGroupCatalogs] = useState({})
  // what the Groups filter box holds; it only narrows the rows shown, and
  // an open group hidden by it keeps its catalog for when it shows again
  const [groupQuery, setGroupQuery] = useState('')
  const generations = useRef({})
  useEffect(() => {
    let stale = false
    api.listGroups(ourShip)
      .then((list) => { if (!stale) { setGroups(list); setGroupsError('') } })
      .catch((cause) => { if (!stale) { setGroups([]); setGroupsError(cause.message) } })
    return () => { stale = true }
  }, [ourShip])
  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return needle ? repositories.filter((repo) => repo.name.toLowerCase().includes(needle)) : repositories
  }, [query, repositories])
  const visibleGroups = useMemo(() => filterGroups(groups, groupQuery), [groups, groupQuery])

  async function addPeer(event) {
    event.preventDefault(); setPeerError('')
    try { await api.addPeer(peerName.trim()); setPeerName(''); setAddingPeer(false); await onPeersChanged() } catch (cause) { setPeerError(cause.message) }
  }

  async function togglePeer(ship) {
    if (expanded[ship]) { setExpanded((value) => ({ ...value, [ship]: false })); return }
    setExpanded((value) => ({ ...value, [ship]: true }))
    setCatalogs((value) => ({ ...value, [ship]: { loading: true, repositories: [] } }))
    try {
      const started = await api.peerDiscover(ship)
      for (let attempt = 0; attempt < 60; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 500))
        const status = await api.peerDiscoveries()
        const found = status.discoveries?.find((item) => item.request === started.request)
        if (found && !found.active) {
          await api.peerDeleteDiscovery(started.request).catch(() => {})
          if (!found.ok) throw new Error(found.message)
          setCatalogs((value) => ({ ...value, [ship]: { loading: false, repositories: found.repositories || [] } }))
          return
        }
      }
      throw new Error('peer did not answer in time')
    } catch (cause) { setCatalogs((value) => ({ ...value, [ship]: { loading: false, error: cause.message, repositories: [] } })) }
  }

  // one catalog request to every member of the group, merged as the answers
  // come in; a refresh of the same group supersedes the loop still polling,
  // and other groups keep their own
  async function discoverGroup(flag) {
    const generation = (generations.current[flag] || 0) + 1
    generations.current[flag] = generation
    const current = () => generations.current[flag] === generation
    const show = (catalog) => setGroupCatalogs((value) => ({ ...value, [flag]: catalog }))
    show({ ...idleCatalog, loading: true })
    try {
      const started = await api.peerDiscoverGroup(flag)
      if (!current()) return
      const requests = started.requests || []
      const reach = { members: started.members || 0, capped: Boolean(started.capped) }
      let merged = mergeDiscoveries([], requests)
      for (let attempt = 0; requests.length && attempt < 70; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 500))
        const status = await api.peerDiscoveries()
        if (!current()) return
        merged = mergeDiscoveries(status.discoveries, requests)
        show({ ...idleCatalog, ...reach, ...merged, loading: merged.pending > 0 })
        if (!merged.pending) break
      }
      await Promise.all(requests.map((request) => api.peerDeleteDiscovery(request).catch(() => {})))
      if (!current()) return
      show({ ...idleCatalog, ...reach, ...merged, loading: false })
    } catch (cause) {
      if (!current()) return
      setGroupCatalogs((value) => ({ ...value, [flag]: { ...idleCatalog, ...value[flag], loading: false, error: cause.message } }))
    }
  }

  function toggleGroup(flag) {
    if (groupsOpen[flag]) { setGroupsOpen((value) => ({ ...value, [flag]: false })); return }
    setGroupsOpen((value) => ({ ...value, [flag]: true }))
    discoverGroup(flag)
  }

  function refreshGroup(flag, event) {
    event.stopPropagation()
    setGroupsOpen((value) => ({ ...value, [flag]: true }))
    discoverGroup(flag)
  }

  async function removePeer(ship, event) {
    event.stopPropagation()
    await api.removePeer(ship); await onPeersChanged()
  }

  function toggleSection(section) {
    setSectionsOpen((value) => ({ ...value, [section]: !value[section] }))
  }

  return (
    <aside className="sidebar">
      <div className="brand"><GitIcon size={20} /><span>urgit</span></div>
      <div className="sidebar-heading">
        <button className="sidebar-section-toggle" onClick={() => toggleSection('repositories')} aria-expanded={sectionsOpen.repositories}><span className="sidebar-section-chevron">{sectionsOpen.repositories ? '⌄' : '›'}</span><span>Repositories</span></button>
        <button className="icon-button" onClick={onCreate} title="New repository"><PlusIcon /></button>
      </div>
      {sectionsOpen.repositories && <>{repositories.length > 5 && <input className="repo-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Find a repository…" aria-label="Find a repository" />}
      <nav className="repo-list">
        {visible.map((repo) => (
          <button
            key={repo.name}
            className={repo.name === selected ? 'repo-link active' : 'repo-link'}
            onClick={() => onSelect(repo.name)}
          >
            <span className="repo-mark" />
            <span className="truncate">{repo.name}</span>
            {!repo.publicRead && <span className="lock" title="Private">private</span>}
          </button>
        ))}
        {!repositories.length && <p className="quiet sidebar-empty">No repositories yet.</p>}
        {repositories.length > 0 && !visible.length && <p className="quiet sidebar-empty">No matching repositories.</p>}
      </nav></>}
      <div className="sidebar-heading peer-heading"><button className="sidebar-section-toggle" onClick={() => toggleSection('peers')} aria-expanded={sectionsOpen.peers}><span className="sidebar-section-chevron">{sectionsOpen.peers ? '⌄' : '›'}</span><span>Peers</span></button><button className="icon-button" onClick={() => { setSectionsOpen((value) => ({ ...value, peers: true })); setAddingPeer((value) => !value) }} title="Add peer"><PlusIcon /></button></div>
      {sectionsOpen.peers && <>{addingPeer && <form className="peer-add" onSubmit={addPeer}><input autoFocus value={peerName} onChange={(event) => setPeerName(event.target.value)} placeholder="~sampel-palnet" /><button className="button" disabled={!peerName.trim()}>Add</button></form>}
      {peerError && <small className="field-error sidebar-peer-error">{peerError}</small>}
      <nav className="peer-tree">
        {peers.map((ship) => <div className="peer-node" key={ship}>
          <div className="peer-link-row"><button className="repo-link peer-link" onClick={() => togglePeer(ship)}><span className="peer-chevron">{expanded[ship] ? '⌄' : '›'}</span><code>{ship}</code></button><button className="peer-remove" onClick={(event) => removePeer(ship, event)} title="Remove peer">×</button></div>
          {expanded[ship] && <div className="peer-children">
            {catalogs[ship]?.loading && <small>Contacting peer…</small>}
            {catalogs[ship]?.error && <small className="field-error">{catalogs[ship].error}</small>}
            {catalogs[ship] && !catalogs[ship].loading && !catalogs[ship].repositories.length && !catalogs[ship].error && <small>No accessible repositories.</small>}
            {(catalogs[ship]?.repositories || []).map((repo) => <button key={repo.name} className={remoteSelected?.ship === ship && remoteSelected?.name === repo.name ? 'repo-link remote-repo-link active' : 'repo-link remote-repo-link'} onClick={() => onSelectRemote(ship, repo.name)}><span className="repo-mark" /><span className="truncate">{repo.name}</span>{repo.writable && <span className="write-badge">write</span>}</button>)}
          </div>}
        </div>)}
        {!peers.length && <small className="quiet peer-empty">Add a ship to browse its repositories.</small>}
      </nav></>}
      <div className="sidebar-heading peer-heading"><button className="sidebar-section-toggle" onClick={() => toggleSection('groups')} aria-expanded={sectionsOpen.groups}><span className="sidebar-section-chevron">{sectionsOpen.groups ? '⌄' : '›'}</span><span>Groups</span></button></div>
      {sectionsOpen.groups && <><input className="repo-search group-filter" value={groupQuery} onChange={(event) => setGroupQuery(event.target.value)} placeholder="Filter by group or host…" aria-label="Filter groups by group or host" />
      <nav className="peer-tree">
        {visibleGroups.map((group) => <div className="peer-node" key={group.flag}>
          <div className="peer-link-row"><button className="repo-link peer-link group-link" onClick={() => toggleGroup(group.flag)} title={group.flag}><span className="peer-chevron">{groupsOpen[group.flag] ? '⌄' : '›'}</span><span className="group-label"><span className="group-title truncate">{group.title}</span><span className="host-tag truncate">{describeHost(group)}</span></span></button><button className="peer-remove group-refresh" onClick={(event) => refreshGroup(group.flag, event)} disabled={groupCatalogs[group.flag]?.loading} title="Ask the group again">↻</button></div>
          {groupsOpen[group.flag] && <div className="peer-children">
            {groupCatalogs[group.flag]?.loading && <small>Asking… {groupCatalogs[group.flag].settled} of {groupCatalogs[group.flag].settled + groupCatalogs[group.flag].pending}</small>}
            {groupCatalogs[group.flag]?.error && <small className="field-error">{groupCatalogs[group.flag].error}</small>}
            {groupCatalogs[group.flag] && !groupCatalogs[group.flag].loading && !groupCatalogs[group.flag].error && !groupCatalogs[group.flag].entries.length && <small>No repositories shared with this group.</small>}
            {(groupCatalogs[group.flag]?.entries || []).map((entry) => <button key={entry.key} className={remoteSelected?.ship === entry.ship && remoteSelected?.name === entry.name ? 'repo-link remote-repo-link active' : 'repo-link remote-repo-link'} onClick={() => onSelectRemote(entry.ship, entry.name)}><span className="repo-mark" /><span className="truncate"><code>{entry.ship}</code> / {entry.name}</span>{describeGrant(entry.via, group.flag, groups) && <span className="via-badge" title={entry.via || 'readable without this group'}>{describeGrant(entry.via, group.flag, groups)}</span>}{entry.writable && <span className="write-badge">write</span>}</button>)}
            {groupCatalogs[group.flag] && !groupCatalogs[group.flag].loading && !groupCatalogs[group.flag].error && countParts(groupCatalogs[group.flag].counts).length > 0 && <small className="group-footer">{countParts(groupCatalogs[group.flag].counts).map((part, index) => <Fragment key={part.key}>{index > 0 && ' · '}{part.key === 'pending' ? <span title={describeHold(groupCatalogs[group.flag].heldSince)}>{part.text}</span> : part.text}</Fragment>)}{groupCatalogs[group.flag].capped && ` · ${describeReach(groupCatalogs[group.flag].members, true)}`}</small>}
          </div>}
        </div>)}
        {groups === null && !groupsError && <small className="quiet peer-empty">Loading groups…</small>}
        {groupsError && <small className="field-error sidebar-peer-error">{groupsError}</small>}
        {groups && !groups.length && !groupsError && <small className="quiet peer-empty">Join a group in Tlon to see repositories shared with it.</small>}
        {groups && groups.length > 0 && !visibleGroups.length && <small className="quiet peer-empty">No matching groups.</small>}
      </nav></>}
    </aside>
  )
}

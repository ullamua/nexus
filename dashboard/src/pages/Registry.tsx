import React, { useEffect, useState } from 'react'

interface Connector {
  name: string
  version: string
  description: string
  protocol: string
  actions: string[]
}

export default function Registry() {
  const [connectors, setConnectors] = useState<Connector[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Connector | null>(null)

  useEffect(() => {
    fetch('/registry')
      .then(r => r.json())
      .then(data => {
        setConnectors(data.connectors || [])
        setLoading(false)
      })
      .catch(e => {
        setError(e.message)
        setLoading(false)
      })
  }, [])

  if (loading) return <div className="state-msg">Loading connectors...</div>
  if (error) return <div className="state-msg error">Error: {error}</div>

  return (
    <div className="page">
      <h1 className="page-title">Connector Registry</h1>
      <p className="page-sub">{connectors.length} connector{connectors.length !== 1 ? 's' : ''} registered</p>

      <div className="connector-grid">
        {connectors.map(c => (
          <div key={c.name} className={`connector-card ${selected?.name === c.name ? 'active' : ''}`} onClick={() => setSelected(c)}>
            <div className="card-header">
              <span className="card-name">{c.name}</span>
              <span className="badge">{c.protocol}</span>
              <span className="version">v{c.version}</span>
            </div>
            <p className="card-desc">{c.description}</p>
            <div className="action-list">
              {c.actions.map(a => <span key={a} className="action-tag">{a}</span>)}
            </div>
          </div>
        ))}
      </div>

      {selected && (
        <div className="detail-panel">
          <div className="detail-header">
            <h2>{selected.name}</h2>
            <button className="close-btn" onClick={() => setSelected(null)}>×</button>
          </div>
          <div className="detail-body">
            <div className="detail-row"><span>Protocol</span><code>{selected.protocol}</code></div>
            <div className="detail-row"><span>Version</span><code>{selected.version}</code></div>
            <div className="detail-row"><span>Actions</span><span>{selected.actions.join(', ')}</span></div>
          </div>
        </div>
      )}
    </div>
  )
}

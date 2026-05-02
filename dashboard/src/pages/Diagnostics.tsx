import React, { useEffect, useState } from 'react'

interface Trace {
  request_id: string
  started_at: string
  total_ms: number
  connector?: string
  action?: string
  error?: string
}

interface Health {
  ok: boolean
  connectors: Record<string, string>
}

export default function Diagnostics() {
  const [health, setHealth] = useState<Health | null>(null)
  const [traces, setTraces] = useState<Trace[]>([])
  const [adminKey, setAdminKey] = useState('')
  const [traceError, setTraceError] = useState('')

  useEffect(() => {
    fetch('/health')
      .then(r => r.json())
      .then(setHealth)
      .catch(() => {})
  }, [])

  const fetchTraces = () => {
    setTraceError('')
    fetch('/diagnostics/traces', {
      headers: adminKey ? { 'X-Nexus-Key': adminKey } : {},
    })
      .then(r => r.json())
      .then(data => {
        if (data.ok) setTraces(data.traces || [])
        else setTraceError(data.error?.message || 'Failed to fetch traces')
      })
      .catch(e => setTraceError(e.message))
  }

  const statusColor = (s: string) =>
    s === 'healthy' ? 'var(--green)' : s === 'degraded' ? 'var(--yellow)' : 'var(--red)'

  return (
    <div className="page">
      <h1 className="page-title">Diagnostics</h1>

      {health && (
        <section className="diag-section">
          <h2 className="section-title">Connector Health</h2>
          <div className="health-grid">
            {Object.entries(health.connectors).map(([name, status]) => (
              <div key={name} className="health-card">
                <span className="health-dot" style={{ background: statusColor(status) }} />
                <span className="health-name">{name}</span>
                <span className="health-status">{status}</span>
              </div>
            ))}
            {Object.keys(health.connectors).length === 0 && (
              <p className="state-msg">No connectors registered.</p>
            )}
          </div>
        </section>
      )}

      <section className="diag-section">
        <h2 className="section-title">Request Traces</h2>
        <div className="trace-controls">
          <input
            className="key-input"
            placeholder="Admin key (NEXUS_ADMIN_KEY)"
            value={adminKey}
            onChange={e => setAdminKey(e.target.value)}
          />
          <button className="send-btn" onClick={fetchTraces}>Load Traces</button>
        </div>
        {traceError && <p className="state-msg error">{traceError}</p>}
        {traces.length > 0 && (
          <table className="trace-table">
            <thead>
              <tr>
                <th>Request ID</th>
                <th>Connector</th>
                <th>Action</th>
                <th>Total (ms)</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {traces.map(t => (
                <tr key={t.request_id}>
                  <td><code>{t.request_id}</code></td>
                  <td>{t.connector || '—'}</td>
                  <td>{t.action || '—'}</td>
                  <td>{t.total_ms}</td>
                  <td style={{ color: t.error ? 'var(--red)' : 'var(--green)' }}>
                    {t.error ? 'error' : 'ok'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  )
}

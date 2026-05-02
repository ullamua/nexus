import React, { useState } from 'react'

const DEFAULT_BODY = JSON.stringify(
  { connector: 'github', action: 'get_user', params: { username: 'torvalds' } },
  null,
  2
)

export default function Playground() {
  const [body, setBody] = useState(DEFAULT_BODY)
  const [nexusKey, setNexusKey] = useState('')
  const [response, setResponse] = useState('')
  const [latency, setLatency] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)

  const send = async () => {
    setLoading(true)
    setResponse('')
    setLatency(null)
    const start = Date.now()
    try {
      const res = await fetch('/call', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(nexusKey ? { 'X-Nexus-Key': nexusKey } : {}),
        },
        body,
      })
      const data = await res.json()
      setResponse(JSON.stringify(data, null, 2))
      setLatency(Date.now() - start)
    } catch (e: any) {
      setResponse(`Error: ${e.message}`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="page">
      <h1 className="page-title">Request Playground</h1>

      <div className="playground">
        <div className="pane">
          <div className="pane-label">Request</div>
          <input
            className="key-input"
            placeholder="X-Nexus-Key (optional)"
            value={nexusKey}
            onChange={e => setNexusKey(e.target.value)}
          />
          <textarea
            className="code-area"
            value={body}
            onChange={e => setBody(e.target.value)}
            rows={18}
            spellCheck={false}
          />
          <button className="send-btn" onClick={send} disabled={loading}>
            {loading ? 'Sending...' : 'Send Request'}
          </button>
        </div>

        <div className="pane">
          <div className="pane-label">
            Response {latency !== null && <span className="latency">{latency}ms</span>}
          </div>
          <textarea
            className="code-area response"
            value={response}
            readOnly
            rows={20}
            placeholder="Response will appear here..."
          />
        </div>
      </div>
    </div>
  )
}

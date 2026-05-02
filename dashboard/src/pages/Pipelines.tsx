import React, { useState } from 'react'

const DEFAULT_PIPELINE = JSON.stringify(
  {
    pipeline: [
      { id: 'step1', connector: 'animekai', action: 'encrypt_id', params: { text: '{{input.content_id}}' } },
      { id: 'step2', connector: 'animekai', action: 'episode_list', params: { enc: '{{step1.data.encrypted_token}}' }, depends_on: ['step1'] },
    ],
    input: { content_id: 'dIG98qei6A' },
  },
  null,
  2
)

export default function Pipelines() {
  const [body, setBody] = useState(DEFAULT_PIPELINE)
  const [nexusKey, setNexusKey] = useState('')
  const [response, setResponse] = useState('')
  const [loading, setLoading] = useState(false)

  const run = async () => {
    setLoading(true)
    setResponse('')
    try {
      const res = await fetch('/pipeline', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(nexusKey ? { 'X-Nexus-Key': nexusKey } : {}),
        },
        body,
      })
      const data = await res.json()
      setResponse(JSON.stringify(data, null, 2))
    } catch (e: any) {
      setResponse(`Error: ${e.message}`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="page">
      <h1 className="page-title">Pipeline Runner</h1>
      <p className="page-sub">Define a multi-step DAG pipeline. Steps with no shared dependencies run in parallel.</p>

      <div className="playground">
        <div className="pane">
          <div className="pane-label">Pipeline Definition</div>
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
            rows={20}
            spellCheck={false}
          />
          <button className="send-btn" onClick={run} disabled={loading}>
            {loading ? 'Running...' : 'Run Pipeline'}
          </button>
        </div>

        <div className="pane">
          <div className="pane-label">Pipeline Result</div>
          <textarea
            className="code-area response"
            value={response}
            readOnly
            rows={22}
            placeholder="Pipeline result will appear here..."
          />
        </div>
      </div>
    </div>
  )
}

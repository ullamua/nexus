import React, { useState } from 'react'
import Registry from './pages/Registry'
import Playground from './pages/Playground'
import Pipelines from './pages/Pipelines'
import Diagnostics from './pages/Diagnostics'

type Page = 'registry' | 'playground' | 'pipelines' | 'diagnostics'

const NAV: { id: Page; label: string }[] = [
  { id: 'registry', label: 'Registry' },
  { id: 'playground', label: 'Playground' },
  { id: 'pipelines', label: 'Pipelines' },
  { id: 'diagnostics', label: 'Diagnostics' },
]

export default function App() {
  const [page, setPage] = useState<Page>('registry')

  return (
    <div className="app">
      <header className="header">
        <div className="logo">
          <span className="logo-mark">⬡</span>
          <span className="logo-name">Nexus</span>
        </div>
        <nav className="nav">
          {NAV.map(n => (
            <button
              key={n.id}
              className={`nav-btn ${page === n.id ? 'active' : ''}`}
              onClick={() => setPage(n.id)}
            >
              {n.label}
            </button>
          ))}
        </nav>
      </header>

      <main className="main">
        {page === 'registry' && <Registry />}
        {page === 'playground' && <Playground />}
        {page === 'pipelines' && <Pipelines />}
        {page === 'diagnostics' && <Diagnostics />}
      </main>
    </div>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { fetchConfig, fetchHealth, fetchStats, type Config, type Health, type Stats } from '../lib/api'
import { ShieldMark } from '../components/icons'

interface Props {
  onBack: () => void
}

const ENDPOINTS: { method: string; path: string; description: string; access: string }[] = [
  { method: 'GET', path: '/health', description: 'Worker status', access: 'public' },
  { method: 'GET', path: '/config', description: 'Model settings', access: 'signed in' },
  { method: 'GET', path: '/stats', description: 'Usage counters', access: 'signed in' },
  { method: 'GET', path: '/history', description: 'Recent checks', access: 'signed in' },
  { method: 'DELETE', path: '/history', description: 'Clear check history', access: 'moderate' },
  { method: 'POST', path: '/moderate', description: 'Check one message', access: 'moderate' },
  { method: 'POST', path: '/moderate/batch', description: 'Check many messages', access: 'moderate' },
  { method: 'GET', path: '/reviews', description: 'Review queue', access: 'review' },
  { method: 'POST', path: '/reviews', description: 'Decide a review', access: 'review' },
  { method: 'DELETE', path: '/reviews', description: 'Reopen a review', access: 'review' },
  { method: 'GET', path: '/db', description: 'Tables and row counts', access: 'db' },
  { method: 'GET', path: '/db/table', description: 'Rows from one table', access: 'db' },
  { method: 'POST', path: '/db/query', description: 'Read-only SQL query', access: 'db' },
  { method: 'GET', path: '/files', description: 'Server filesystem browser', access: 'files' },
  { method: 'POST', path: '/terminal', description: 'Run a command', access: 'terminal' },
  { method: 'POST', path: '/terminal/cancel', description: 'Cancel a command', access: 'terminal' },
  { method: 'GET', path: '/setup', description: 'Account and service settings', access: 'setup' },
  { method: 'PUT', path: '/setup', description: 'Update setup', access: 'setup' },
  { method: 'GET', path: '/users', description: 'Console accounts', access: 'users' },
  { method: 'POST', path: '/users', description: 'Create an account', access: 'users' },
  { method: 'PUT', path: '/users', description: 'Update an account', access: 'users' },
  { method: 'DELETE', path: '/users', description: 'Remove an account', access: 'users' },
]

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function ServiceInfo({ onBack }: Props) {
  const [health, setHealth] = useState<Health | null>(null)
  const [config, setConfig] = useState<Config | null>(null)
  const [stats, setStats] = useState<Stats | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    const [healthResult, configResult, statsResult] = await Promise.allSettled([
      fetchHealth(),
      fetchConfig(),
      fetchStats(),
    ])
    if (healthResult.status === 'fulfilled') setHealth(healthResult.value)
    if (configResult.status === 'fulfilled') setConfig(configResult.value)
    if (statsResult.status === 'fulfilled') setStats(statsResult.value)
    if ([healthResult, configResult, statsResult].some((result) => result.status === 'rejected')) {
      setError('Some details could not be loaded.')
    }
    setLoading(false)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  return (
    <div className="setup-screen">
      <div className="setup-wrap">
        <header className="setup-header">
          <div className="auth-brand">
            <ShieldMark />
            <div>
              <strong>Soglia</strong>
              <span>moderation desk</span>
            </div>
          </div>
          <div className="setup-header-actions">
            <span className="mono setup-build">build 6</span>
            <button className="ghost-button setup-back" onClick={onBack}>
              <span aria-hidden="true">←</span> Back to console
            </button>
          </div>
        </header>

        <div className="setup-intro">
          <h1>Service info</h1>
          <p>Live status, model details and the HTTP endpoints exposed by the API.</p>
        </div>

        {error && <p className="inline-error" role="alert">{error}</p>}

        {loading ? (
          <div className="setup-loading mono">Loading service info…</div>
        ) : (
          <div className="service-info">
            <section className="setup-card">
              <div className="setup-card-head">
                <h2>Service</h2>
                <span>live</span>
              </div>
              <dl className="setup-status-grid">
                <div><dt>Status</dt><dd>{health?.status === 'ok' ? 'online' : 'unavailable'}</dd></div>
                <div><dt>Service</dt><dd className="mono service-value">profanity-filter-api</dd></div>
                <div><dt>Model path</dt><dd className="mono service-value">{health?.model ?? '—'}</dd></div>
                <div><dt>Checks recorded</dt><dd>{stats?.total ?? '—'}</dd></div>
              </dl>
            </section>

            <section className="setup-card">
              <div className="setup-card-head">
                <h2>Model</h2>
                <span>from /config</span>
              </div>
              <dl className="setup-status-grid service-model-grid">
                <div><dt>Architecture</dt><dd>{config?.model?.architecture ?? '—'}</dd></div>
                <div><dt>Task</dt><dd>{config?.model?.task ?? '—'}</dd></div>
                <div><dt>Layers</dt><dd>{config?.model?.layers ?? '—'}</dd></div>
                <div><dt>Heads</dt><dd>{config?.model?.heads ?? '—'}</dd></div>
                <div><dt>Hidden size</dt><dd>{config?.model?.hidden_size ?? '—'}</dd></div>
                <div><dt>Vocabulary</dt><dd>{config?.model?.vocab_size ?? '—'}</dd></div>
                <div><dt>Transformers</dt><dd>{config?.model?.transformers_version ?? '—'}</dd></div>
                <div><dt>Blocklist</dt><dd>{config?.blocklist_size ?? '—'} terms</dd></div>
              </dl>
            </section>

            <section className="setup-card">
              <div className="setup-card-head">
                <h2>Thresholds</h2>
                <span>from /config</span>
              </div>
              <dl className="setup-status-grid">
                <div><dt>Block at</dt><dd>{config?.block_threshold ?? '—'}</dd></div>
                <div><dt>Review at</dt><dd>{config?.review_threshold ?? '—'}</dd></div>
                <div><dt>Temperature</dt><dd>{config?.temperature ?? '—'}</dd></div>
                <div><dt>Max tokens</dt><dd>{config?.max_tokens ?? '—'}</dd></div>
              </dl>
            </section>

            {config?.files && config.files.length > 0 && (
              <section className="setup-card">
                <div className="setup-card-head">
                  <h2>Model files</h2>
                  <span>{config.files.length} files</span>
                </div>
                <ul className="service-files">
                  {config.files.map((file) => (
                    <li key={file.name}>
                      <span className="mono">{file.name}</span>
                      <span className="mono">{formatBytes(file.bytes)}</span>
                    </li>
                  ))}
                </ul>
              </section>
            )}

            <section className="setup-card">
              <div className="setup-card-head">
                <h2>API endpoints</h2>
                <span>access by permission</span>
              </div>
              <ul className="service-endpoints">
                {ENDPOINTS.map((endpoint) => (
                  <li key={`${endpoint.method} ${endpoint.path}`}>
                    <span className={`service-method method-${endpoint.method.toLowerCase()}`}>{endpoint.method}</span>
                    <code className="mono service-path">{endpoint.path}</code>
                    <span className="service-description">{endpoint.description}</span>
                    <span className="service-access">{endpoint.access}</span>
                  </li>
                ))}
              </ul>
              <p className="setup-note">
                Endpoints marked <code>signed in</code> need a session; the rest require the named permission.
                Raw JSON is still available at <code className="mono">/</code>.
              </p>
            </section>
          </div>
        )}
      </div>
    </div>
  )
}

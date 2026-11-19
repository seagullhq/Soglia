import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from 'react'
import {
  cancelTerminal,
  checkMessage,
  decideReview,
  checkMessages,
  clearHistory,
  fetchConfig,
  fetchDBTables,
  fetchFiles,
  fetchHealth,
  fetchHistory,
  fetchReviews,
  fetchStats,
  getMe,
  runTerminal,
  login,
  logout,
  reopenReview,
  runDBQuery,
  setUnauthorizedHandler,
  type Action,
  type Config,
  type DBResult,
  type DBTable,
  type FilePreview,
  type FilesResponse,
  type Health,
  type HistoryRecord,
  type ReviewItem,
  type SessionUser,
  type Stats,
  type Verdict,
} from '../lib/api'
import { LayersIcon, RefreshIcon, ShieldMark } from '../components/icons'
import { ServiceInfo } from './ServiceInfo'
import { Setup } from './Setup'

const EXAMPLES = [
  'Ciao! Ci vediamo domani alle 10 su Discord?',
  'Che bella scazzottata nel film ieri sera',
  'You are such a f**king idiot',
  'v a f f a n c u l o',
  'stfu you absolute m0r0n',
]

const MAX_BATCH = 100
const HISTORY_LIMIT = 12
const BUILD = '6'

type HistoryEntry = HistoryRecord

function basename(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts[parts.length - 1] ?? path
}

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fixed(value: unknown, digits: number): string {
  return typeof value === 'number' ? value.toFixed(digits) : '—'
}

function count(value: unknown): string {
  return typeof value === 'number' ? value.toLocaleString('en-US') : '—'
}

function spec(value: unknown): string {
  if (typeof value === 'number') return value.toLocaleString('en-US')
  return typeof value === 'string' && value ? value : '—'
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong.'
}

type ConsoleTab = 'moderation' | 'reviews' | 'files' | 'db'

function tabPermission(tab: ConsoleTab): string | null {
  if (tab === 'reviews') return 'review'
  if (tab === 'files') return 'files'
  if (tab === 'db') return 'db'
  return null
}

function tabAllowed(tab: ConsoleTab, permissions: string[]): boolean {
  const needed = tabPermission(tab)
  return needed === null || permissions.includes(needed)
}

function writeTabToUrl(tab: ConsoleTab): void {
  const url = new URL(window.location.href)
  url.searchParams.delete('view')
  if (tab === 'moderation') url.searchParams.delete('tab')
  else url.searchParams.set('tab', tab)
  window.history.replaceState(null, '', url)
}

function ActionBadge({ action }: { action: Action }) {
  return <span className={`action-badge action-${action}`}>{action}</span>
}

function ProbabilityRail({ prob, config }: { prob: number; config: Config | null }) {
  const review = config?.review_threshold ?? 0.6
  const block = config?.block_threshold ?? 0.65
  const pct = Math.max(0, Math.min(100, prob * 100))

  return (
    <div className="rail">
      <div className="rail-track">
        <div className="rail-fill" style={{ width: `${pct}%` }} />
        <span className="rail-tick tick-review" style={{ left: `${review * 100}%` }} />
        <span className="rail-tick tick-block" style={{ left: `${block * 100}%` }} />
        <span className="rail-needle" style={{ left: `${pct}%` }} />
      </div>
      <div className="rail-scale mono">
        <span>0.00</span>
        <span>1.00</span>
      </div>
      <div className="rail-meta">
        <span className="threshold threshold-review">review ≥ {review.toFixed(2)}</span>
        <span className="threshold threshold-block">block ≥ {block.toFixed(2)}</span>
        <strong className="rail-value mono">{prob.toFixed(4)}</strong>
      </div>
    </div>
  )
}

function VerdictView({
  verdict,
  text,
  config,
}: {
  verdict: Verdict | null
  text: string
  config: Config | null
}) {
  if (!verdict) {
    return (
      <div className="verdict-empty">
        <div className="empty-mark" aria-hidden="true" />
        <p>No verdict yet. Send a message to see the decision.</p>
        <span>The verdict shows where the message landed against the review and block thresholds.</span>
      </div>
    )
  }

  return (
    <div className={`verdict verdict-${verdict.action}`} aria-live="polite">
      <div className="verdict-head">
        <span className="verdict-word">{verdict.action}</span>
        <div className="verdict-meta">
          <span className="tier-chip">{verdict.tier}</span>
          <span className="mono time-chip">{verdict.ms.toFixed(2)} ms</span>
        </div>
      </div>
      <ProbabilityRail prob={verdict.prob} config={config} />
      <div className="verdict-foot">
        <span className="checked-text">{text ? `Checked: "${truncate(text, 88)}"` : 'Latest check'}</span>
        {verdict.matched && (
          <span className="matched">
            {verdict.tier.startsWith('Tier 1') ? 'matched' : 'likely word'}{' '}
            <code>{verdict.matched}</code>
          </span>
        )}
      </div>
    </div>
  )
}

function LoginScreen({ onLogin }: { onLogin: (username: string, password: string) => Promise<void> }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    setBusy(true)
    try {
      await onLogin(username, password)
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-screen">
      <div className="auth-card">
        <div className="auth-brand">
          <ShieldMark />
          <div>
            <strong>Soglia</strong>
            <span>moderation desk</span>
          </div>
        </div>
        <h1>Sign in</h1>
        <p className="auth-intro">The moderation console is protected. Sign in with the account configured on the API.</p>
        <form onSubmit={(event) => void submit(event)}>
          <label htmlFor="auth-user">Username</label>
          <input
            id="auth-user"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
          <label htmlFor="auth-password">Password</label>
          <input
            id="auth-password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="current-password"
            required
          />
          {error && <p className="inline-error" role="alert">{error}</p>}
          <button className="primary-button auth-submit" type="submit" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
        <p className="auth-foot mono">build {BUILD} · sessions expire after 8 hours</p>
      </div>
    </div>
  )
}

function FilesPanel({ initialPath, modelDir }: { initialPath: string; modelDir?: string }) {
  const [data, setData] = useState<FilesResponse | null>(null)
  const [input, setInput] = useState('')
  const [file, setFile] = useState<FilePreview | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const modelPath = modelDir?.startsWith('/app') ? modelDir : ''

  const load = useCallback(async (path: string) => {
    setLoading(true)
    setError('')
    try {
      const result = await fetchFiles(path)
      setData(result)
      setInput(result.path)
      setFile(!result.is_dir && result.file ? result.file : null)
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(initialPath)
  }, [initialPath, load])

  function open(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void load(input.trim())
  }

  return (
    <section className="files-panel">
      <div className="section-head">
        <h2>Files <span className="hint">browse /app on the machine running the API</span></h2>
      </div>

      <p className="files-notice">
        Browsing is limited to <code className="mono">/app</code>. Private keys, <code className="mono">.env</code>{' '}
        files, credential stores and binaries are never returned; secrets inside{' '}
        <code className="mono">settings.json</code> are masked.
      </p>

      {error && <p className="inline-error" role="alert">{error}</p>}

      <div className="files-toolbar">
        <button className="ghost-button" onClick={() => void load(data?.parent ?? '')} disabled={!data?.parent}>
          ↑ Up
        </button>
        <form className="files-go" onSubmit={open}>
          <input
            className="mono"
            aria-label="Path"
            placeholder="Path under /app"
            value={input}
            onChange={(event) => setInput(event.target.value)}
          />
          <button className="primary-button" type="submit" disabled={loading}>Open</button>
        </form>
      </div>

      <div className="files-quick">
        <button className="link-button" onClick={() => void load('')}>/app</button>
        {modelPath && <button className="link-button" onClick={() => void load(modelPath)}>Model</button>}
        <span className="files-current mono">{data?.path ?? '—'}</span>
      </div>

      {loading && !data ? (
        <div className="setup-loading mono">Loading files…</div>
      ) : data ? (
        <div className="files-layout">
          <div className="files-card">
            <div className="files-card-head">
              <h3>{basename(data.path) || data.path}</h3>
              <span>{data.is_dir ? `${data.entries?.length ?? 0} items` : 'file'}</span>
            </div>
            {data.is_dir ? (
              <ul className="files-rows">
                {(data.entries ?? []).map((entry) => (
                  <li key={entry.path}>
                    <button
                      className={`files-row${entry.is_dir ? ' is-dir' : ''}${entry.locked ? ' is-locked' : ''}`}
                      onClick={() => void load(entry.path)}
                      title={entry.path}
                    >
                      <span className="files-icon" aria-hidden="true">{entry.is_dir ? '▸' : entry.locked ? '•' : '·'}</span>
                      <span className="files-name">{entry.name}</span>
                      <span className="files-size mono">{entry.is_dir ? 'dir' : formatBytes(entry.size)}</span>
                      {entry.reason && <span className="files-reason">{entry.reason}</span>}
                    </button>
                  </li>
                ))}
                {(data.entries?.length ?? 0) === 0 && <li className="files-empty">Empty directory</li>}
              </ul>
            ) : (
              <div className="files-file-meta">
                <p className="mono">{data.file?.path}</p>
                <p>{formatBytes(data.file?.size ?? 0)}</p>
              </div>
            )}
          </div>

          {file && (
            <div className="files-card files-preview">
              <div className="files-card-head">
                <h3>{file.name}</h3>
                <span>{formatBytes(file.size)}</span>
              </div>
              {file.locked ? (
                <p className="files-locked">{file.reason ?? 'This file cannot be previewed.'}</p>
              ) : file.binary ? (
                <p className="files-locked">{file.reason ?? 'Binary file.'}</p>
              ) : (
                <>
                  {file.truncated && <p className="files-note">Preview truncated to the first 256 KB.</p>}
                  <pre className="files-code mono">{file.content}</pre>
                </>
              )}
            </div>
          )}
        </div>
      ) : (
        <div className="setup-loading mono">Loading files…</div>
      )}
    </section>
  )
}

type TerminalEntry = { kind: 'command' | 'output' | 'error'; text: string }
type TerminalHistorySetter = Dispatch<SetStateAction<string[]>>

function newCommandId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function TerminalModal({
  onClose,
  history,
  setHistory,
}: {
  onClose: () => void
  history: string[]
  setHistory: TerminalHistorySetter
}) {
  const [entries, setEntries] = useState<TerminalEntry[]>([])
  const [input, setInput] = useState('')
  const [cwd, setCwd] = useState('')
  const [busy, setBusy] = useState(false)
  const [historyIndex, setHistoryIndex] = useState(-1)
  const [copied, setCopied] = useState(false)
  const outputRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const activeIdRef = useRef('')
  const stickToBottomRef = useRef(true)

  function clearOutput() {
    setEntries([])
    stickToBottomRef.current = true
  }

  async function stop() {
    const id = activeIdRef.current
    if (!id) return
    try {
      await cancelTerminal(id)
    } catch (cause) {
      setEntries((previous) => [...previous, { kind: 'error', text: messageOf(cause) }])
    }
  }

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      } else if (event.ctrlKey && (event.key === 'c' || event.key === 'C')) {
        // Ctrl+C interrupts a running command; otherwise clear the input line.
        // When nothing is running and the input is not focused, leave the
        // browser's own copy shortcut alone so output stays selectable.
        if (busy) {
          event.preventDefault()
          void stop()
        } else if (document.activeElement === inputRef.current) {
          event.preventDefault()
          setInput('')
        }
      } else if (event.ctrlKey && (event.key === 'l' || event.key === 'L')) {
        event.preventDefault()
        clearOutput()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, busy])

  useEffect(() => {
    if (outputRef.current && stickToBottomRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [entries])

  function onScroll() {
    const element = outputRef.current
    if (!element) return
    stickToBottomRef.current = element.scrollHeight - element.scrollTop - element.clientHeight < 40
  }

  async function copyOutput() {
    const text = entries.map((entry) => entry.text).join('\n')
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch (cause) {
      setEntries((previous) => [...previous, { kind: 'error', text: messageOf(cause) }])
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const command = input.trim()
    if (!command || busy) return
    setInput('')
    setHistory((previous) => [...previous.slice(-99), command])
    setHistoryIndex(-1)
    // `clear` needs a terminal emulator; handle it in the modal instead of
    // sending it to the shell (where TERM is unset and `cls` does not exist).
    if (command === 'clear' || command === 'cls') {
      clearOutput()
      return
    }
    stickToBottomRef.current = true
    setEntries((previous) => [
      ...previous,
      { kind: 'command', text: `${cwd ? basename(cwd) : '~'} $ ${command}` },
    ])
    const id = newCommandId()
    activeIdRef.current = id
    setBusy(true)
    try {
      const result = await runTerminal(id, command, cwd)
      setCwd(result.cwd)
      setEntries((previous) => [
        ...previous,
        ...(result.output ? [{ kind: 'output' as const, text: result.output }] : []),
        ...(result.code !== 0 ? [{ kind: 'error' as const, text: `exit ${result.code}` }] : []),
      ])
    } catch (cause) {
      setEntries((previous) => [...previous, { kind: 'error', text: messageOf(cause) }])
    } finally {
      activeIdRef.current = ''
      setBusy(false)
    }
  }

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div className="terminal-modal" role="dialog" aria-modal="true" aria-label="Terminal">
        <div className="terminal-head">
          <div>
            <strong>Terminal</strong>
            <span className="mono">{cwd || 'API working directory'}</span>
          </div>
          <div className="terminal-actions">
            <button
              className="terminal-tool"
              type="button"
              onClick={() => void copyOutput()}
              disabled={entries.length === 0}
              title="Copy all output"
              aria-label="Copy all output"
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
            <button className="modal-close" onClick={onClose} aria-label="Close terminal">×</button>
          </div>
        </div>

        <p className="terminal-note">
          Commands run on the machine hosting the API. Signed-in access only.
          ↑ / ↓ history · Ctrl+C stop · Ctrl+L clear.
        </p>

        <div className="terminal-screen" ref={outputRef} onScroll={onScroll}>
          {entries.length === 0 ? (
            <div className="terminal-line terminal-muted">Type a command to get started, or clear / cls to reset.</div>
          ) : (
            entries.map((entry, index) => (
              <div key={index} className={`terminal-line terminal-${entry.kind}`}>{entry.text}</div>
            ))
          )}
        </div>

        <form className="terminal-prompt-row" onSubmit={(event) => void submit(event)}>
          <span className="terminal-prompt mono">{cwd ? basename(cwd) : '~'} $</span>
          <input
            ref={inputRef}
            autoFocus
            className="mono"
            aria-label="Command"
            placeholder="ls -la"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'ArrowUp') {
                event.preventDefault()
                if (history.length === 0) return
                const next = historyIndex === -1 ? history.length - 1 : Math.max(0, historyIndex - 1)
                setHistoryIndex(next)
                setInput(history[next])
              } else if (event.key === 'ArrowDown') {
                event.preventDefault()
                if (historyIndex === -1) return
                const next = historyIndex + 1
                if (next >= history.length) {
                  setHistoryIndex(-1)
                  setInput('')
                } else {
                  setHistoryIndex(next)
                  setInput(history[next])
                }
              }
            }}
            disabled={busy}
          />
          {busy ? (
            <button className="terminal-stop" type="button" onClick={() => void stop()}>Stop</button>
          ) : (
            <button className="primary-button" type="submit" disabled={!input.trim()}>Run</button>
          )}
        </form>
      </div>
    </div>
  )
}

function ReviewPanel({
  items,
  pending,
  config,
  busyId,
  error,
  onDecide,
  onReopen,
  onOpen,
}: {
  items: ReviewItem[]
  pending: number
  config: Config | null
  busyId: string
  error: string
  onDecide: (item: ReviewItem, decision: 'allow' | 'block', note: string) => Promise<void>
  onReopen: (item: ReviewItem) => Promise<void>
  onOpen: (item: ReviewItem) => void
}) {
  const [filter, setFilter] = useState<'all' | 'pending' | 'decided'>('all')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [note, setNote] = useState('')

  const decided = items.filter((item) => Boolean(item.decision)).length
  const visible = items.filter((item) =>
    filter === 'all' ? true : filter === 'pending' ? !item.decision : Boolean(item.decision),
  )
  const selected = visible.find((item) => item.id === selectedId) ?? visible[0] ?? null
  const busy = busyId !== ''

  useEffect(() => {
    setNote(selected?.note ?? '')
  }, [selected?.id, selected?.note])

  return (
    <section className="review-workspace">
      <div className="section-head">
        <h2>
          Review queue
          <span className="hint">flagged by the model, decided by a moderator</span>
        </h2>
      </div>

      <div className="review-columns">
        <div className="review-list-pane">
          <div className="review-filters" role="tablist" aria-label="Review filter">
            <button
              role="tab"
              aria-selected={filter === 'all'}
              className={filter === 'all' ? 'is-active' : ''}
              onClick={() => setFilter('all')}
            >
              All <span className="mono">{items.length}</span>
            </button>
            <button
              role="tab"
              aria-selected={filter === 'pending'}
              className={filter === 'pending' ? 'is-active' : ''}
              onClick={() => setFilter('pending')}
            >
              Pending <span className="mono">{pending}</span>
            </button>
            <button
              role="tab"
              aria-selected={filter === 'decided'}
              className={filter === 'decided' ? 'is-active' : ''}
              onClick={() => setFilter('decided')}
            >
              Decided <span className="mono">{decided}</span>
            </button>
          </div>

          {visible.length === 0 ? (
            <p className="review-empty">
              {filter === 'pending'
                ? 'Nothing awaiting review.'
                : filter === 'decided'
                  ? 'No decisions recorded yet.'
                  : 'Nothing in the review queue yet.'}
            </p>
          ) : (
            <ul className="review-items">
              {visible.map((item) => (
                <li key={item.id}>
                  <button
                    className={`review-item${selected?.id === item.id ? ' is-selected' : ''}`}
                    onClick={() => setSelectedId(item.id)}
                    aria-current={selected?.id === item.id ? 'true' : undefined}
                  >
                    <span className="review-item-top">
                      <span className="mono at">{new Date(item.at).toLocaleTimeString()}</span>
                      <span className="mono review-item-score">{item.prob.toFixed(3)}</span>
                    </span>
                    <span className="review-item-text">{truncate(item.text, 70)}</span>
                    <span className="review-item-foot">
                      <span className="mono">{item.tier}</span>
                      {item.matched && <span className="mono review-matched">{item.matched}</span>}
                      {item.decision && (
                        <span className={`action-badge action-${item.decision}`}>{item.decision}</span>
                      )}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="review-detail-pane">
          {selected === null ? (
            <p className="review-detail-empty">Select a message to review it.</p>
          ) : (
            <>
              <div className="review-detail-head">
                <span className="mono review-detail-id">#{selected.id}</span>
                <span className="mono at">{new Date(selected.at).toLocaleString()}</span>
                <button className="link-button" onClick={() => onOpen(selected)}>
                  Open in checker →
                </button>
              </div>

              <blockquote className="review-message">{selected.text}</blockquote>

              <div className="review-signals">
                <ProbabilityRail prob={selected.prob} config={config} />
                <dl className="review-facts">
                  <div>
                    <dt>Matched</dt>
                    <dd className="mono">{selected.matched || '—'}</dd>
                  </div>
                  <div>
                    <dt>Tier</dt>
                    <dd className="mono">{selected.tier}</dd>
                  </div>
                </dl>
              </div>

              {error && <p className="inline-error" role="alert">{error}</p>}

              {selected.decision ? (
                <div className="review-resolved">
                  <span className={`action-badge action-${selected.decision}`}>
                    {selected.decision === 'allow' ? 'Allowed' : 'Blocked'}
                  </span>
                  <p>
                    Decided by <strong>{selected.reviewer || 'a moderator'}</strong>
                    {selected.decided_at ? ` · ${new Date(selected.decided_at).toLocaleString()}` : ''}
                  </p>
                  {selected.note && <blockquote className="review-resolved-note">{selected.note}</blockquote>}
                  <label className="field-label" htmlFor="review-note">
                    Update note <span className="hint">optional</span>
                  </label>
                  <textarea
                    id="review-note"
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    placeholder="Adjust the note before changing the decision…"
                    rows={2}
                  />
                  <div className="review-change-actions">
                    <button
                      className={selected.decision === 'allow' ? 'review-block' : 'review-allow'}
                      disabled={busy}
                      onClick={() =>
                        void onDecide(selected, selected.decision === 'allow' ? 'block' : 'allow', note)
                      }
                    >
                      Change to {selected.decision === 'allow' ? 'Block' : 'Allow'}
                    </button>
                    <button
                      className="review-reopen"
                      disabled={busy}
                      onClick={() => void onReopen(selected)}
                    >
                      Undo decision
                    </button>
                  </div>
                </div>
              ) : (
                <div className="review-decide">
                  <label className="field-label" htmlFor="review-note">
                    Moderator note <span className="hint">optional</span>
                  </label>
                  <textarea
                    id="review-note"
                    value={note}
                    onChange={(event) => setNote(event.target.value)}
                    placeholder="Add context for this decision…"
                    rows={2}
                  />
                  <div className="review-decide-actions">
                    <button
                      className="review-allow"
                      disabled={busy}
                      onClick={() => void onDecide(selected, 'allow', note)}
                    >
                      Allow message
                    </button>
                    <button
                      className="review-block"
                      disabled={busy}
                      onClick={() => void onDecide(selected, 'block', note)}
                    >
                      Block message
                    </button>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </section>
  )
}

function dbCellText(value: unknown): string {
  if (value === null || value === undefined) return 'NULL'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function DBPanel({ onChanged }: { onChanged: () => void }) {
  const [tables, setTables] = useState<DBTable[]>([])
  const [selected, setSelected] = useState('')
  const [sql, setSql] = useState('')
  const [result, setResult] = useState<DBResult | null>(null)
  const [deleted, setDeleted] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const refreshTables = useCallback(async () => {
    try {
      const response = await fetchDBTables()
      setTables(response.tables)
    } catch (cause) {
      setError(messageOf(cause))
    }
  }, [])

  const run = useCallback(async (query: string) => {
    if (/\bDELETE\b/i.test(query)) {
      const confirmed = window.confirm(
        `Run this DELETE? Deleted rows cannot be recovered.\n\n${query.trim()}`,
      )
      if (!confirmed) return
    }
    setBusy(true)
    setError('')
    try {
      const response = await runDBQuery(query)
      if ('deleted' in response) {
        setResult(null)
        setDeleted(response.deleted)
        await refreshTables()
        onChanged()
      } else {
        setDeleted(null)
        setResult(response)
      }
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }, [onChanged, refreshTables])

  useEffect(() => {
    let cancelled = false
    fetchDBTables()
      .then((response) => {
        if (cancelled) return
        setTables(response.tables)
        const first = response.tables.find((table) => table.name === 'checks') ?? response.tables[0]
        if (first) {
          const query = `SELECT * FROM ${first.name} ORDER BY 1 DESC LIMIT 200`
          setSelected(first.name)
          setSql(query)
          void run(query)
        }
      })
      .catch((cause) => {
        if (!cancelled) setError(messageOf(cause))
      })
    return () => { cancelled = true }
  }, [run])

  function openTable(table: DBTable) {
    const query = `SELECT * FROM ${table.name} ORDER BY 1 DESC LIMIT 200`
    setSelected(table.name)
    setSql(query)
    void run(query)
  }

  return (
    <section className="db-workspace">
      <div className="section-head">
        <h2>
          Database
          <span className="hint">table browser and SQL queries · admin DELETE enabled</span>
        </h2>
      </div>

      <div className="db-columns">
        <div className="db-tables-pane">
          <div className="db-pane-title">Tables</div>
          {tables.length === 0 ? (
            <p className="review-empty">No tables found.</p>
          ) : (
            <ul className="db-tables">
              {tables.map((table) => (
                <li key={table.name}>
                  <button
                    className={`db-table${selected === table.name ? ' is-selected' : ''}`}
                    onClick={() => openTable(table)}
                  >
                    <span className="mono db-table-name">{table.name}</span>
                    <span className="mono db-table-count">{table.rows.toLocaleString('en-US')}</span>
                    <span className="db-table-columns">{table.columns.join(', ')}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          <p className="db-note">
            Credential columns are masked. SELECT / WITH queries and admin DELETE statements are
            accepted; all other writes are rejected.
          </p>
        </div>

        <div className="db-main-pane">
          <div className="db-query">
            <label className="field-label" htmlFor="db-sql">
              SQL query <span className="hint">SELECT / WITH / DELETE · Ctrl + Enter to run</span>
            </label>
            <textarea
              id="db-sql"
              className="db-sql-input"
              value={sql}
              onChange={(event) => setSql(event.target.value)}
              onKeyDown={(event) => {
                if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
                  event.preventDefault()
                  void run(sql)
                }
              }}
              placeholder="SELECT * FROM checks ORDER BY id DESC LIMIT 50"
              rows={3}
              spellCheck={false}
            />
            <div className="db-query-actions">
              <button className="primary-button" onClick={() => void run(sql)} disabled={busy}>
                {busy ? 'Running…' : 'Run query'}
              </button>
              <span className="db-examples">
                Try:
                <button onClick={() => void run('SELECT action, count(*) AS n FROM checks GROUP BY action ORDER BY n DESC')}>action counts</button>
                <button onClick={() => void run('SELECT * FROM reviews ORDER BY decided_at DESC LIMIT 50')}>review decisions</button>
              </span>
            </div>
            {error && <p className="inline-error" role="alert">{error}</p>}
          </div>

          <div className="db-results">
            <div className="db-results-head">
              <span>Results</span>
              {deleted !== null ? (
                <span className="hint mono">
                  {deleted} row{deleted === 1 ? '' : 's'} deleted
                </span>
              ) : result && (
                <span className="hint mono">
                  {result.rows.length} row{result.rows.length === 1 ? '' : 's'}
                  {result.truncated ? ` · first ${result.rows.length} only` : ''}
                </span>
              )}
            </div>
            {deleted !== null ? (
              <p className="db-results-empty db-delete-note">
                DELETE completed. {deleted} row{deleted === 1 ? '' : 's'} removed; table counts and
                console data have been refreshed.
              </p>
            ) : !result ? (
              <p className="db-results-empty">Run a query or choose a table to see its rows.</p>
            ) : result.columns.length === 0 || result.rows.length === 0 ? (
              <p className="db-results-empty">The query returned no rows.</p>
            ) : (
              <div className="db-table-scroll">
                <table className="db-grid">
                  <thead>
                    <tr>{result.columns.map((column) => <th key={column}>{column}</th>)}</tr>
                  </thead>
                  <tbody>
                    {result.rows.map((row, index) => (
                      <tr key={index}>
                        {result.columns.map((column) => (
                          <td key={column} className="mono">{dbCellText(row[column])}</td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      </div>
    </section>
  )
}

export default function App() {
  const [filesNav, setFilesNav] = useState({ path: '', seq: 0 })
  const [terminalOpen, setTerminalOpen] = useState(false)
  const [terminalHistory, setTerminalHistory] = useState<string[]>(() => {
    try {
      const saved: unknown = JSON.parse(localStorage.getItem('soglia.terminal.history') || '[]')
      return Array.isArray(saved)
        ? saved.filter((item): item is string => typeof item === 'string').slice(-100)
        : []
    } catch {
      return []
    }
  })
  const [view, setView] = useState<'dashboard' | 'setup' | 'service'>(() => {
    const requested = new URLSearchParams(window.location.search).get('view')
    return requested === 'setup' || requested === 'service' ? requested : 'dashboard'
  })
  const [tab, setTab] = useState<ConsoleTab>(() => {
    const params = new URLSearchParams(window.location.search)
    const requested = params.get('tab')
    if (requested === 'files' || params.get('view') === 'files') return 'files'
    if (requested === 'reviews') return 'reviews'
    return requested === 'db' ? 'db' : 'moderation'
  })
  const [user, setUser] = useState<string | null>(null)
  const [role, setRole] = useState('')
  const [permissions, setPermissions] = useState<string[]>([])
  const [authReady, setAuthReady] = useState(false)
  const [health, setHealth] = useState<Health | null>(null)
  const [config, setConfig] = useState<Config | null>(null)
  const [stats, setStats] = useState<Stats | null>(null)
  const [offline, setOffline] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  const [text, setText] = useState('')
  const [verdict, setVerdict] = useState<Verdict | null>(null)
  const [checkedText, setCheckedText] = useState('')
  const [busy, setBusy] = useState(false)
  const [formError, setFormError] = useState('')

  const [batchText, setBatchText] = useState('')
  const [batchTexts, setBatchTexts] = useState<string[]>([])
  const [batchResults, setBatchResults] = useState<Verdict[] | null>(null)
  const [batchBusy, setBatchBusy] = useState(false)
  const [batchError, setBatchError] = useState('')

  const [history, setHistory] = useState<HistoryEntry[]>([])
  const [reviews, setReviews] = useState<ReviewItem[]>([])
  const [pendingReviews, setPendingReviews] = useState(0)
  const [reviewBusyId, setReviewBusyId] = useState('')
  const [reviewError, setReviewError] = useState('')

  useEffect(() => {
    try {
      localStorage.setItem('soglia.terminal.history', JSON.stringify(terminalHistory.slice(-100)))
    } catch {
      // Storage can be unavailable (private mode, quota); history still works in memory.
    }
  }, [terminalHistory])

  const refreshStatus = useCallback(async () => {
    setRefreshing(true)
    const [healthResult, statsResult] = await Promise.allSettled([fetchHealth(), fetchStats()])
    if (healthResult.status === 'fulfilled') {
      setHealth(healthResult.value)
      setOffline(false)
    } else {
      setOffline(true)
    }
    if (statsResult.status === 'fulfilled') setStats(statsResult.value)
    setRefreshing(false)
  }, [])

  const refreshReviews = useCallback(async () => {
    try {
      const result = await fetchReviews()
      setReviews(result.items)
      setPendingReviews(result.pending)
    } catch {
      // Keep the last known queue. A transient API hiccup must not erase the
      // moderator's view of pending work.
    }
  }, [])

  const applySession = useCallback((session: SessionUser) => {
    setUser(session.user)
    setRole(session.role)
    setPermissions(session.permissions)
  }, [])

  const clearSession = useCallback(() => {
    setUser(null)
    setRole('')
    setPermissions([])
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => clearSession())
    getMe()
      .then(applySession)
      .catch(clearSession)
      .finally(() => setAuthReady(true))
    return () => setUnauthorizedHandler(null)
  }, [applySession, clearSession])

  // After an account change, re-resolve the session: a username-only change
  // keeps the user signed in, while a password change invalidates the cookie.
  const reloadSession = useCallback(async () => {
    try {
      applySession(await getMe())
    } catch {
      clearSession()
    }
  }, [applySession, clearSession])

  useEffect(() => {
    if (!user) return
    void refreshStatus()
    fetchConfig()
      .then(setConfig)
      .catch(() => setOffline(true))
    fetchHistory()
      .then(setHistory)
      .catch(() => setHistory([]))
    if (permissions.includes('review')) void refreshReviews()
    const timer = window.setInterval(() => {
      void refreshStatus()
      if (permissions.includes('review')) void refreshReviews()
    }, 8000)
    return () => window.clearInterval(timer)
  }, [user, permissions, refreshStatus, refreshReviews])

  useEffect(() => {
    if (!user) return
    if (!tabAllowed(tab, permissions)) {
      setTab('moderation')
      writeTabToUrl('moderation')
    }
  }, [user, permissions, tab])

  const handleLogin = useCallback(
    async (username: string, password: string) => {
      applySession(await login(username, password))
    },
    [applySession],
  )

  const changeTab = useCallback(
    (next: ConsoleTab) => {
      if (!tabAllowed(next, permissions)) return
      setTab(next)
      writeTabToUrl(next)
    },
    [permissions],
  )

  const openFiles = useCallback(
    (path: string) => {
      if (!permissions.includes('files')) return
      setFilesNav((previous) => ({ path, seq: previous.seq + 1 }))
      changeTab('files')
    },
    [changeTab, permissions],
  )

  const clearAllHistory = useCallback(async () => {
    try {
      await clearHistory()
      setHistory([])
      await Promise.all([refreshReviews(), refreshStatus()])
    } catch (error) {
      setFormError(messageOf(error))
    }
  }, [refreshReviews, refreshStatus])

  const submitReview = useCallback(
    async (item: ReviewItem, decision: 'allow' | 'block', note: string) => {
      setReviewBusyId(item.id)
      setReviewError('')
      try {
        await decideReview(item.id, decision, note)
        await Promise.all([refreshReviews(), refreshStatus()])
      } catch (error) {
        setReviewError(messageOf(error))
      } finally {
        setReviewBusyId('')
      }
    },
    [refreshReviews, refreshStatus],
  )

  const reopenReviewItem = useCallback(
    async (item: ReviewItem) => {
      setReviewBusyId(item.id)
      setReviewError('')
      try {
        await reopenReview(item.id)
        await Promise.all([refreshReviews(), refreshStatus()])
      } catch (error) {
        setReviewError(messageOf(error))
      } finally {
        setReviewBusyId('')
      }
    },
    [refreshReviews, refreshStatus],
  )

  const openReview = useCallback((item: ReviewItem) => {
    setText(item.text)
    setCheckedText(item.text)
    setVerdict(item)
    setFormError('')
    changeTab('moderation')
  }, [changeTab])

  const refreshAfterDBChange = useCallback(() => {
    void refreshStatus()
    void refreshReviews()
    fetchHistory()
      .then(setHistory)
      .catch(() => setHistory([]))
  }, [refreshStatus, refreshReviews])

  const handleLogout = useCallback(async () => {
    try {
      await logout()
    } finally {
      setUser(null)
      setConfig(null)
      setStats(null)
      setVerdict(null)
      setBatchResults(null)
      setHistory([])
      setReviews([])
      setPendingReviews(0)
      setReviewError('')
    }
  }, [])

  const pushHistory = useCallback((result: Verdict, source: string) => {
    const entry: HistoryEntry = {
      ...result,
      id: `${Date.now()}-${Math.random()}`,
      text: source,
      at: Date.now(),
    }
    setHistory((previous) => [entry, ...previous].slice(0, HISTORY_LIMIT))
  }, [])

  const runCheck = useCallback(async () => {
    if (!text.trim()) {
      setFormError('Write a message to check.')
      return
    }
    setFormError('')
    setBusy(true)
    try {
      const result = await checkMessage(text)
      setVerdict(result)
      setCheckedText(text)
      pushHistory(result, text)
      void refreshStatus()
      void refreshReviews()
    } catch (error) {
      setFormError(messageOf(error))
    } finally {
      setBusy(false)
    }
  }, [text, pushHistory, refreshStatus, refreshReviews])

  const runBatch = useCallback(async () => {
    const lines = batchText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
    if (lines.length === 0) {
      setBatchError('Add at least one message, one per line.')
      return
    }
    if (lines.length > MAX_BATCH) {
      setBatchError(`A batch can hold up to ${MAX_BATCH} messages.`)
      return
    }
    setBatchError('')
    setBatchBusy(true)
    try {
      const results = await checkMessages(lines)
      setBatchTexts(lines)
      setBatchResults(results)
      results.forEach((result, index) => pushHistory(result, lines[index]))
      void refreshStatus()
      void refreshReviews()
    } catch (error) {
      setBatchError(messageOf(error))
    } finally {
      setBatchBusy(false)
    }
  }, [batchText, pushHistory, refreshStatus, refreshReviews])

  if (!authReady) {
    return (
      <div className="auth-screen">
        <div className="auth-loading mono">Checking session…</div>
      </div>
    )
  }

  if (!user) return <LoginScreen onLogin={handleLogin} />

  if (view === 'setup' && permissions.includes('setup')) {
    return (
      <Setup
        onBack={() => setView('dashboard')}
        onAccountChanged={() => reloadSession()}
      />
    )
  }

  if (view === 'service') {
    return <ServiceInfo onBack={() => setView('dashboard')} />
  }

  const serviceOnline = health?.status === 'ok' && !offline

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <ShieldMark />
          <div>
            <strong>Soglia</strong>
            <span>moderation desk</span>
            <span className="build-tag mono">build {BUILD}</span>
          </div>
        </div>

        <section className="side-section">
          <h2>Service</h2>
          <div className="status-line">
            <span className={`status-dot ${serviceOnline ? 'online' : 'offline'}`} />
            <span>{serviceOnline ? 'API reachable' : 'API unreachable'}</span>
          </div>
          <div className="side-row">
            <span>Model</span>
            <span className="mono">{config?.model_dir ? basename(config.model_dir) : '—'}</span>
          </div>
          <div className="side-row">
            <span>Worker</span>
            <span>{serviceOnline ? 'running' : 'stopped'}</span>
          </div>
          <button className="link-button" onClick={() => void refreshStatus()} disabled={refreshing}>
            <RefreshIcon /> {refreshing ? 'Checking…' : 'Refresh status'}
          </button>
        </section>

        <section className="side-section">
          <h2>Model</h2>
          <dl className="rule-list">
            <div>
              <dt>Architecture</dt>
              <dd className="mono">{spec(config?.model?.architecture)}</dd>
            </div>
            <div>
              <dt>Task</dt>
              <dd>{spec(config?.model?.task)}</dd>
            </div>
            <div>
              <dt>Layers / heads</dt>
              <dd className="mono">
                {typeof config?.model?.layers === 'number' && typeof config?.model?.heads === 'number'
                  ? `${config.model.layers} / ${config.model.heads}`
                  : '—'}
              </dd>
            </div>
            <div>
              <dt>Hidden size</dt>
              <dd className="mono">{spec(config?.model?.hidden_size)}</dd>
            </div>
            <div>
              <dt>Vocabulary</dt>
              <dd className="mono">{spec(config?.model?.vocab_size)}</dd>
            </div>
            <div>
              <dt>Input limit</dt>
              <dd className="mono">{typeof config?.max_tokens === 'number' ? `${config.max_tokens} tokens` : '—'}</dd>
            </div>
            <div>
              <dt>Built with</dt>
              <dd className="mono">
                {config?.model?.transformers_version ? `transformers ${config.model.transformers_version}` : '—'}
              </dd>
            </div>
          </dl>
        </section>

        <section className="side-section">
          <h2>Decision path</h2>
          <ol className="flow">
            <li>
              <span className="flow-index mono">01</span>
              <span><strong>Normalize</strong><small>homoglyphs, leetspeak, spacing</small></span>
            </li>
            <li>
              <span className="flow-index mono">02</span>
              <span><strong>Blocklist</strong><small>{count(config?.blocklist_size)} exact terms</small></span>
            </li>
            <li>
              <span className="flow-index mono">03</span>
              <span><strong>Transformer</strong><small>temperature {fixed(config?.temperature, 4)}</small></span>
            </li>
          </ol>
          <div className="range-list">
            <div>
              <span><i className="range-dot dot-allow" />allow</span>
              <span className="mono">below {fixed(config?.review_threshold, 2)}</span>
            </div>
            <div>
              <span><i className="range-dot dot-review" />review</span>
              <span className="mono">{fixed(config?.review_threshold, 2)} to {fixed(config?.block_threshold, 2)}</span>
            </div>
            <div>
              <span><i className="range-dot dot-block" />block</span>
              <span className="mono">from {fixed(config?.block_threshold, 2)}</span>
            </div>
          </div>
        </section>

        <section className="side-section">
          <h2>Model files</h2>
          <ul className="file-list">
            {(Array.isArray(config?.files) ? config.files : []).map((file) => (
              <li key={file.name} className="file-item">
                <button
                  className="file-link"
                  title={`Open ${file.name}`}
                  onClick={() => {
                    const base = config?.model_dir?.replace(/[\\/]+$/, '') ?? ''
                    openFiles(base ? `${base}/${file.name}` : file.name)
                  }}
                >
                  <span className="file-name mono">{file.name}</span>
                  <span className="file-size mono">{formatBytes(file.bytes)}</span>
                </button>
              </li>
            ))}
            {!config && <li className="file-placeholder">—</li>}
          </ul>
        </section>

        <section className="side-section side-api">
          <h2>API</h2>
          <div className="side-api-links">
            <button className="side-link" onClick={() => setView('service')}>
              <span>Service info</span><span aria-hidden="true">→</span>
            </button>
            {permissions.includes('setup') && (
              <button className="side-link" onClick={() => setView('setup')}>
                <span>Open setup</span><span aria-hidden="true">→</span>
              </button>
            )}
          </div>
          <p className="side-note side-api-note">Go HTTP service · Python inference worker</p>
        </section>
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <h1>Moderation desk</h1>
            <p>Check messages against the trained profanity filter.</p>
            <div className="header-model">
              <span className="mono model-chip">{spec(config?.model?.architecture)}</span>
              <span className="mono model-chip">{config?.model_dir ? basename(config.model_dir) : '—'}</span>
              <span className="mono model-chip">{spec(config?.model?.layers)} layers</span>
              <span className="mono model-chip">INT8 ONNX</span>
            </div>
          </div>
          <div className="topbar-actions">
            <div className={`pill ${serviceOnline ? 'pill-online' : 'pill-offline'}`}>
              {serviceOnline ? 'live' : 'offline'}
            </div>
            <span className="session-user mono" title={`Signed in as ${user} (${role})`}>{user} · {role}</span>
            {permissions.includes('terminal') && (
              <button className="terminal-button" onClick={() => setTerminalOpen(true)}>Terminal</button>
            )}
            {permissions.includes('setup') && (
              <button className="signout-button" onClick={() => setView('setup')}>Setup</button>
            )}
            <button className="signout-button" onClick={() => void handleLogout()}>Sign out</button>
          </div>
        </header>

        {offline && (
          <div className="banner" role="alert">
            <span>Cannot reach the API. Start it from the <code>api/</code> folder with <code>go run .</code>.</span>
            <button onClick={() => void refreshStatus()}>Retry</button>
          </div>
        )}

        <div className="stat-strip">
          <div>
            <span className="stat-value mono">{stats?.total ?? '—'}</span>
            <span className="stat-label">Checks processed</span>
          </div>
          <div>
            <span className="stat-value mono">{stats?.block ?? '—'}</span>
            <span className="stat-label">Blocked</span>
          </div>
          <div title={stats ? `${stats.review} messages flagged for review in total` : undefined}>
            <span className={`stat-value mono${pendingReviews > 0 ? ' stat-attention' : ''}`}>
              {stats?.pending_reviews ?? '—'}
            </span>
            <span className="stat-label">Awaiting review</span>
          </div>
          <div>
            <span className="stat-value mono">{stats ? `${stats.avg_ms.toFixed(2)} ms` : '—'}</span>
            <span className="stat-label">Average time</span>
          </div>
        </div>

        <div className="console-tabs" role="tablist" aria-label="Console views">
          <button
            role="tab"
            aria-selected={tab === 'moderation'}
            className={`console-tab${tab === 'moderation' ? ' is-active' : ''}`}
            onClick={() => changeTab('moderation')}
          >
            Moderation
          </button>
          {permissions.includes('review') && (
            <button
              role="tab"
              aria-selected={tab === 'reviews'}
              className={`console-tab${tab === 'reviews' ? ' is-active' : ''}`}
              onClick={() => changeTab('reviews')}
            >
              Review
              {pendingReviews > 0 && <span className="tab-count mono">{pendingReviews}</span>}
            </button>
          )}
          {permissions.includes('files') && (
            <button
              role="tab"
              aria-selected={tab === 'files'}
              className={`console-tab${tab === 'files' ? ' is-active' : ''}`}
              onClick={() => openFiles('')}
            >
              Files
            </button>
          )}
          {permissions.includes('db') && (
            <button
              role="tab"
              aria-selected={tab === 'db'}
              className={`console-tab${tab === 'db' ? ' is-active' : ''}`}
              onClick={() => changeTab('db')}
            >
              Database
            </button>
          )}
        </div>

        <div className="console-panel" role="tabpanel">
          {tab === 'moderation' && (
            <>
            {permissions.includes('moderate') && (
            <section className="check-section">
              <div className="section-head">
                <h2>Check a message</h2>
                <span className="hint">Ctrl + Enter to run</span>
              </div>

              <div className="composer">
                <label htmlFor="message">Message</label>
                <textarea
                  id="message"
                  value={text}
                  onChange={(event) => setText(event.target.value)}
                  onKeyDown={(event) => {
                    if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
                      event.preventDefault()
                      void runCheck()
                    }
                  }}
                  placeholder="Type a message in any supported language…"
                  rows={3}
                />
                <div className="composer-foot">
                  <div className="examples">
                    <span>Try</span>
                    {EXAMPLES.map((example) => (
                      <button key={example} onClick={() => setText(example)} title={example}>
                        {truncate(example, 26)}
                      </button>
                    ))}
                  </div>
                  <div className="composer-actions">
                    <button className="ghost-button" onClick={() => { setText(''); setFormError('') }}>Clear</button>
                    <button className="primary-button" onClick={() => void runCheck()} disabled={busy}>
                      {busy ? 'Checking…' : 'Run check'}
                    </button>
                  </div>
                </div>
              </div>

              {formError && <p className="inline-error" role="alert">{formError}</p>}

              <VerdictView verdict={verdict} text={checkedText} config={config} />
            </section>
            )}

            {permissions.includes('moderate') && (
            <section className="batch-section">
              <div className="section-head">
                <h2><LayersIcon /> Batch check</h2>
                <span className="hint">one message per line, up to {MAX_BATCH}</span>
              </div>
              <div className="batch-grid">
                <div>
                  <label className="field-label" htmlFor="batch">Messages</label>
                  <textarea
                    id="batch"
                    className="batch-input"
                    value={batchText}
                    onChange={(event) => setBatchText(event.target.value)}
                    placeholder={'First message\nSecond message\nThird message'}
                    rows={5}
                  />
                  <div className="batch-actions">
                    <span className="line-count mono">
                      {batchText.split(/\r?\n/).filter((line) => line.trim()).length} messages
                    </span>
                    <button className="primary-button" onClick={() => void runBatch()} disabled={batchBusy}>
                      {batchBusy ? 'Checking…' : 'Check batch'}
                    </button>
                  </div>
                  {batchError && <p className="inline-error" role="alert">{batchError}</p>}
                </div>
                <div>
                  <span className="field-label">Results</span>
                  {batchResults === null ? (
                    <div className="batch-empty">Results from the last batch appear here.</div>
                  ) : batchResults.length === 0 ? (
                    <div className="batch-empty">No messages were checked.</div>
                  ) : (
                    <ol className="batch-results">
                      {batchResults.map((result, index) => (
                        <li key={`${index}-${batchTexts[index]}`}>
                          <span className="mono index">{String(index + 1).padStart(2, '0')}</span>
                          <ActionBadge action={result.action} />
                          <span className="batch-text">{truncate(batchTexts[index] ?? '', 52)}</span>
                          <span className="mono batch-prob">{result.prob.toFixed(3)}</span>
                        </li>
                      ))}
                    </ol>
                  )}
                </div>
              </div>
            </section>
            )}

            <section className="history-section">
              <div className="section-head">
                <h2>Recent checks <span className="hint">saved in the database</span></h2>
                {permissions.includes('moderate') && history.length > 0 && (
                  <button className="link-button" onClick={() => void clearAllHistory()}>Clear history</button>
                )}
              </div>
              {history.length === 0 ? (
                <p className="history-empty">Nothing checked yet. Your last {HISTORY_LIMIT} checks will collect here.</p>
              ) : (
                <ul className="history-list">
                  {history.map((entry) => (
                    <li key={entry.id}>
                      <button onClick={() => { setText(entry.text); setCheckedText(entry.text); setVerdict(entry) }}>
                        <span className="mono at">{new Date(entry.at).toLocaleTimeString()}</span>
                        <ActionBadge action={entry.action} />
                        <span className="history-text">{truncate(entry.text, 70)}</span>
                        <span className="mono history-tier">{entry.tier}</span>
                        <span className="mono history-prob">{entry.prob.toFixed(3)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            </>
          )}

          {tab === 'reviews' && (
            <ReviewPanel
              items={reviews}
              pending={pendingReviews}
              config={config}
              busyId={reviewBusyId}
              error={reviewError}
              onDecide={(item, decision, note) => submitReview(item, decision, note)}
              onReopen={(item) => reopenReviewItem(item)}
              onOpen={openReview}
            />
          )}

          {tab === 'files' && (
            <FilesPanel
              key={filesNav.seq}
              initialPath={filesNav.path}
              modelDir={config?.model_dir}
            />
          )}

          {tab === 'db' && <DBPanel onChanged={refreshAfterDBChange} />}
        </div>
      </main>

      {terminalOpen && (
        <TerminalModal
          onClose={() => setTerminalOpen(false)}
          history={terminalHistory}
          setHistory={setTerminalHistory}
        />
      )}
    </div>
  )
}

import { useCallback, useEffect, useState, type FormEvent } from 'react'
import {
  createUser,
  deleteUser,
  fetchSetup,
  fetchUsers,
  resetSetup,
  updateSetup,
  updateUser,
  type ConsoleUser,
  type SetupInfo,
} from '../lib/api'
import { ShieldMark } from '../components/icons'

interface Props {
  onBack: () => void
  onAccountChanged: () => void | Promise<void>
}

interface FormState {
  username: string
  password: string
  confirm: string
  database_url: string
  model_dir: string
  python: string
  worker: string
  secure_cookie: boolean
  addr: string
  tls_cert: string
  tls_key: string
  terminal_enabled: boolean
  temperature: number
  block_threshold: number
  review_threshold: number
}

function formFrom(info: SetupInfo): FormState {
  return {
    username: info.account.username,
    password: '',
    confirm: '',
    database_url: info.settings.database_url,
    model_dir: info.settings.model_dir,
    python: info.settings.python,
    worker: info.settings.worker,
    secure_cookie: info.settings.secure_cookie,
    addr: info.settings.addr,
    tls_cert: info.settings.tls_cert,
    tls_key: info.settings.tls_key,
    terminal_enabled: info.settings.terminal_enabled,
    temperature: info.settings.temperature,
    block_threshold: info.settings.block_threshold,
    review_threshold: info.settings.review_threshold,
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong.'
}

interface UsersPanelProps {
  onChanged: () => void | Promise<void>
}

function UsersPanel({ onChanged }: UsersPanelProps) {
  const [users, setUsers] = useState<ConsoleUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState('')
  const [busy, setBusy] = useState(false)

  const [newUsername, setNewUsername] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState('viewer')

  const [resetFor, setResetFor] = useState<string | null>(null)
  const [resetPassword, setResetPassword] = useState('')

  const load = useCallback(() => fetchUsers().then((data) => setUsers(data.users)), [])
  const enabledAdmins = users.filter((user) => user.role === 'admin' && user.enabled).length

  useEffect(() => {
    load()
      .catch((cause) => setError(messageOf(cause)))
      .finally(() => setLoading(false))
  }, [load])

  async function run(action: () => Promise<unknown>, success: string): Promise<boolean> {
    setError('')
    setSaved('')
    setBusy(true)
    try {
      await action()
      await load()
      await onChanged()
      setSaved(success)
      return true
    } catch (cause) {
      setError(messageOf(cause))
      return false
    } finally {
      setBusy(false)
    }
  }

  async function addUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const username = newUsername.trim()
    if (await run(() => createUser(username, newPassword, newRole), `Account ${username} created.`)) {
      setNewUsername('')
      setNewPassword('')
      setNewRole('viewer')
    }
  }

  function startReset(username: string) {
    setError('')
    setSaved('')
    setResetPassword('')
    setResetFor(username)
  }

  async function saveReset(username: string) {
    const ok = await run(
      () => updateUser({ username, password: resetPassword }),
      `Password reset for ${username}.`,
    )
    if (ok) {
      setResetFor(null)
      setResetPassword('')
    }
  }

  async function changeRole(username: string, role: string) {
    await run(() => updateUser({ username, role }), `Role updated for ${username}.`)
  }

  async function toggleEnabled(user: ConsoleUser) {
    await run(
      () => updateUser({ username: user.username, enabled: !user.enabled }),
      `${user.username} ${user.enabled ? 'disabled' : 'enabled'}.`,
    )
  }

  async function remove(username: string) {
    if (!window.confirm(`Remove account ${username}?`)) return
    await run(() => deleteUser(username), `Account ${username} removed.`)
  }

  return (
    <section className="setup-card users-card">
      <div className="setup-card-head">
        <div className="setup-card-head-main">
          <span className="setup-index">06</span>
          <h2>Users &amp; permissions</h2>
        </div>
        <span className="setup-tag setup-tag--admin">admins only</span>
      </div>

      {error && <p className="inline-error" role="alert">{error}</p>}
      {saved && <p className="setup-saved" role="status">{saved}</p>}

      {loading ? (
        <div className="setup-loading mono">Loading accounts…</div>
      ) : (
        <div className="users-list">
          {users.map((user) => {
            const isMain = user.id === 1
            const soleAdmin = user.role === 'admin' && user.enabled && enabledAdmins === 1
            return (
            <div className={`user-row${user.enabled ? '' : ' is-disabled'}`} key={user.username}>
              <div className="user-identity">
                <div className="user-name-row">
                  <strong className="mono">{user.username}</strong>
                  {isMain && <em className="main-badge">main admin</em>}
                </div>
                <small>{user.enabled ? 'enabled' : 'disabled'}</small>
              </div>
              <label className="user-role">
                <span>Role</span>
                <select
                  value={user.role}
                  disabled={busy || soleAdmin}
                  title={soleAdmin ? 'The only enabled admin cannot be demoted.' : undefined}
                  onChange={(event) => void changeRole(user.username, event.target.value)}
                >
                  <option value="admin">Admin</option>
                  <option value="moderator">Moderator</option>
                  <option value="viewer">Viewer</option>
                </select>
              </label>
              <div className="user-actions">
                <button
                  type="button"
                  className="link-button"
                  disabled={busy || isMain}
                  title={isMain ? 'Set SOGLIA_PASSWORD and restart to change the main admin password.' : undefined}
                  onClick={() => startReset(user.username)}
                >
                  Reset password
                </button>
                <button
                  type="button"
                  className="link-button"
                  disabled={busy || soleAdmin}
                  title={soleAdmin ? 'The only enabled admin cannot be disabled.' : undefined}
                  onClick={() => void toggleEnabled(user)}
                >
                  {user.enabled ? 'Disable' : 'Enable'}
                </button>
                <button
                  type="button"
                  className="link-button danger-link"
                  disabled={busy || soleAdmin}
                  title={soleAdmin ? 'The only enabled admin cannot be removed.' : undefined}
                  onClick={() => void remove(user.username)}
                >
                  Remove
                </button>
              </div>
              {resetFor === user.username && (
                <div className="user-reset">
                  <input
                    type="password"
                    className="mono"
                    placeholder="New password (min 8 characters)"
                    value={resetPassword}
                    autoFocus
                    onChange={(event) => setResetPassword(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') {
                        event.preventDefault()
                        void saveReset(user.username)
                      }
                      if (event.key === 'Escape') setResetFor(null)
                    }}
                  />
                  <button type="button" className="primary-button" disabled={busy} onClick={() => void saveReset(user.username)}>
                    Save
                  </button>
                  <button type="button" className="ghost-button" onClick={() => setResetFor(null)}>
                    Cancel
                  </button>
                </div>
              )}
            </div>
            )
          })}
        </div>
      )}

      <form className="user-add" onSubmit={(event) => void addUser(event)}>
        <input
          className="mono"
          placeholder="Username"
          value={newUsername}
          onChange={(event) => setNewUsername(event.target.value)}
          required
        />
        <input
          type="password"
          className="mono"
          placeholder="Password (min 8 characters)"
          value={newPassword}
          onChange={(event) => setNewPassword(event.target.value)}
          autoComplete="new-password"
          required
        />
        <select value={newRole} onChange={(event) => setNewRole(event.target.value)}>
          <option value="admin">Admin</option>
          <option value="moderator">Moderator</option>
          <option value="viewer">Viewer</option>
        </select>
        <button type="submit" className="primary-button" disabled={busy}>Add user</button>
      </form>

      <p className="setup-note">
        Admin: full access. Moderator: checks and review decisions. Viewer: read-only stats and history.
        The only enabled admin cannot be demoted, disabled or removed. The main admin password is managed
        by <code>SOGLIA_PASSWORD</code> and cannot be reset here — change it in <code>.env</code> and restart.
      </p>
    </section>
  )
}

export function Setup({ onBack, onAccountChanged }: Props) {
  const [info, setInfo] = useState<SetupInfo | null>(null)
  const [form, setForm] = useState<FormState | null>(null)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    fetchSetup()
      .then((data) => {
        setInfo(data)
        setForm(formFrom(data))
      })
      .catch((cause) => setError(messageOf(cause)))
  }, [])

  function field<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((previous) => (previous ? { ...previous, [key]: value } : previous))
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!form) return
    setError('')
    setSaved('')
    if (form.password !== form.confirm) {
      setError('Passwords do not match.')
      return
    }

    setBusy(true)
    try {
      const result = await updateSetup({
        username: form.username,
        password: form.password,
        database_url: form.database_url,
        model_dir: form.model_dir,
        python: form.python,
        worker: form.worker,
        secure_cookie: form.secure_cookie,
        addr: form.addr,
        tls_cert: form.tls_cert,
        tls_key: form.tls_key,
        terminal_enabled: form.terminal_enabled,
        temperature: form.temperature,
        block_threshold: form.block_threshold,
        review_threshold: form.review_threshold,
      })
      if (result.account_changed) {
        await onAccountChanged()
        return
      }
      const fresh = await fetchSetup()
      setInfo(fresh)
      setForm(formFrom(fresh))
      setSaved(
        result.settings_changed
          ? 'Service settings saved. Restart the API for them to take effect.'
          : 'No changes to save.',
      )
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }

  async function restoreDefaults() {
    if (!window.confirm('Restore all service settings to their defaults? Accounts and users are not affected.')) return
    setError('')
    setSaved('')
    setBusy(true)
    try {
      await resetSetup()
      const fresh = await fetchSetup()
      setInfo(fresh)
      setForm(formFrom(fresh))
      setSaved('Service settings restored to defaults. Restart the API for them to take effect.')
    } catch (cause) {
      setError(messageOf(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="setup-screen setup-page">
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
          <p className="setup-eyebrow">Administration / Configuration</p>
          <h1>Setup</h1>
          <p>Change the console account, manage users, and edit the service configuration in one place.</p>
        </div>

        {error && <p className="inline-error" role="alert">{error}</p>}
        {saved && <p className="setup-saved" role="status">{saved}</p>}

        {!form || !info ? (
          <div className="setup-loading mono">Loading setup…</div>
        ) : (
          <>
            <form id="setup-form" className="setup-form" onSubmit={(event) => void submit(event)}>
              <section className="setup-card">
                <div className="setup-card-head">
                  <div className="setup-card-head-main">
                    <span className="setup-index">01</span>
                    <h2>Console account</h2>
                  </div>
                  <span className="setup-tag setup-tag--now">applies immediately</span>
                </div>
                <div className="setup-grid">
                  <label>
                    <span>Username</span>
                    <input value={form.username} onChange={(e) => field('username', e.target.value)} required />
                  </label>
                  <label>
                    <span>New password</span>
                    <input type="password" value={form.password} onChange={(e) => field('password', e.target.value)} autoComplete="new-password" />
                  </label>
                  <label>
                    <span>Confirm password</span>
                    <input type="password" value={form.confirm} onChange={(e) => field('confirm', e.target.value)} autoComplete="new-password" />
                  </label>
                </div>
                <p className="setup-note">Leave both password fields blank to keep the current password. Changing the account signs you out if the password changes.</p>
              </section>

              <section className="setup-card">
                <div className="setup-card-head">
                  <div className="setup-card-head-main">
                    <span className="setup-index">02</span>
                    <h2>Database</h2>
                  </div>
                  <span className="setup-tag setup-tag--restart">applies after restart</span>
                </div>
                <div className="setup-grid">
                  <label className="setup-wide">
                    <span>PostgreSQL URL</span>
                    <input className="mono" value={form.database_url} onChange={(e) => field('database_url', e.target.value)} />
                  </label>
                </div>
                <p className="setup-note">
                  The password is hidden. Leave <code>********</code> unchanged to keep it. The URL is stored in{' '}
                  <code className="mono">{info.status.settings_file}</code>.
                </p>
              </section>

              <section className="setup-card">
                <div className="setup-card-head">
                  <div className="setup-card-head-main">
                    <span className="setup-index">03</span>
                    <h2>Service</h2>
                  </div>
                  <span className="setup-tag setup-tag--restart">applies after restart</span>
                </div>
                <div className="setup-grid">
                  <label>
                    <span>Listen address</span>
                    <input className="mono" value={form.addr} onChange={(e) => field('addr', e.target.value)} required />
                  </label>
                  <label>
                    <span>Python executable</span>
                    <input className="mono" value={form.python} onChange={(e) => field('python', e.target.value)} />
                  </label>
                  <label>
                    <span>Worker path</span>
                    <input className="mono" value={form.worker} onChange={(e) => field('worker', e.target.value)} />
                  </label>
                  <label className="setup-wide">
                    <span>Model directory</span>
                    <input className="mono" value={form.model_dir} onChange={(e) => field('model_dir', e.target.value)} />
                  </label>
                  <label>
                    <span>TLS certificate</span>
                    <input className="mono" value={form.tls_cert} onChange={(e) => field('tls_cert', e.target.value)} />
                  </label>
                  <label>
                    <span>TLS private key</span>
                    <input className="mono" value={form.tls_key} onChange={(e) => field('tls_key', e.target.value)} />
                  </label>
                  <label className="setup-check">
                    <input type="checkbox" checked={form.secure_cookie} onChange={(e) => field('secure_cookie', e.target.checked)} />
                    <span>Secure cookie (only when serving over HTTPS)</span>
                  </label>
                  <label className="setup-check">
                    <input type="checkbox" checked={form.terminal_enabled} onChange={(e) => field('terminal_enabled', e.target.checked)} />
                    <span>Diagnostic terminal (read-only, admins only)</span>
                  </label>
                </div>
                <p className="setup-note">
                  Set both TLS paths to serve HTTPS, or leave both blank for HTTP. The address and TLS
                  settings, like the terminal switch, take effect after the API is restarted.
                </p>
              </section>

              <section className="setup-card">
                <div className="setup-card-head">
                  <div className="setup-card-head-main">
                    <span className="setup-index">04</span>
                    <h2>Model thresholds</h2>
                  </div>
                  <span className="setup-tag setup-tag--restart">applies after restart</span>
                </div>
                <div className="setup-grid">
                  <label>
                    <span>Temperature</span>
                    <input
                      type="number"
                      className="mono"
                      min="0.01"
                      max="100"
                      step="0.01"
                      value={form.temperature}
                      onChange={(e) => field('temperature', e.target.value === '' ? 0 : Number(e.target.value))}
                    />
                  </label>
                  <label>
                    <span>Review threshold</span>
                    <input
                      type="number"
                      className="mono"
                      min="0"
                      max="1"
                      step="0.01"
                      value={form.review_threshold}
                      onChange={(e) => field('review_threshold', e.target.value === '' ? 0 : Number(e.target.value))}
                    />
                  </label>
                  <label>
                    <span>Block threshold</span>
                    <input
                      type="number"
                      className="mono"
                      min="0"
                      max="1"
                      step="0.01"
                      value={form.block_threshold}
                      onChange={(e) => field('block_threshold', e.target.value === '' ? 0 : Number(e.target.value))}
                    />
                  </label>
                </div>
                <p className="setup-note">
                  These override the values in <code>calibration.json</code>. Thresholds are
                  probabilities between 0 and 1; temperature must be greater than 0.
                </p>
              </section>

              <section className="setup-card setup-status">
                <div className="setup-card-head">
                  <div className="setup-card-head-main">
                    <span className="setup-index">05</span>
                    <h2>Status</h2>
                  </div>
                </div>
                <dl className="setup-status-grid">
                  <div><dt>Database</dt><dd><span className="status-pill status-pill--ok">{info.status.database}</span></dd></div>
                  <div>
                    <dt>HTTPS</dt>
                    <dd><span className={`status-pill ${info.status.https ? 'status-pill--ok' : 'status-pill--muted'}`}>{info.status.https ? 'on' : 'off'}</span></dd>
                  </div>
                  <div>
                    <dt>Model directory</dt>
                    <dd><span className={`status-pill ${info.status.model_dir_exists ? 'status-pill--ok' : 'status-pill--bad'}`}>{info.status.model_dir_exists ? 'found' : 'missing'}</span></dd>
                  </div>
                  <div>
                    <dt>Worker</dt>
                    <dd><span className={`status-pill ${info.status.worker_exists ? 'status-pill--ok' : 'status-pill--bad'}`}>{info.status.worker_exists ? 'found' : 'missing'}</span></dd>
                  </div>
                  <div>
                    <dt>Restart needed</dt>
                    <dd><span className={`status-pill ${info.status.restart_required ? 'status-pill--warn' : 'status-pill--ok'}`}>{info.status.restart_required ? 'yes' : 'no'}</span></dd>
                  </div>
                </dl>
              </section>

            </form>

            <UsersPanel onChanged={onAccountChanged} />

            <div className="setup-actions">
              <button type="button" className="ghost-button setup-reset" disabled={busy} onClick={() => void restoreDefaults()}>
                Restore defaults
              </button>
              <button type="button" className="ghost-button" onClick={onBack}>Cancel</button>
              <button type="submit" form="setup-form" className="primary-button" disabled={busy}>
                {busy ? 'Saving…' : 'Save setup'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

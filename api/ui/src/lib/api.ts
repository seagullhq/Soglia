export type Action = 'allow' | 'review' | 'block'

export interface Verdict {
  action: Action
  tier: string
  prob: number
  ms: number
  matched?: string
}

export interface Health {
  status: string
  model: string
}

export interface ModelDetails {
  architecture: string
  task: string
  layers: number
  heads: number
  hidden_size: number
  vocab_size: number
  transformers_version: string
}

export interface ModelFile {
  name: string
  bytes: number
}

export interface Config {
  terminal_enabled: boolean
  model_dir: string
  temperature: number
  block_threshold: number
  review_threshold: number
  blocklist_size: number
  max_tokens: number
  model: ModelDetails
  files: ModelFile[]
}

export interface Stats {
  total: number
  allow: number
  review: number
  pending_reviews: number
  block: number
  tier1: number
  tier2: number
  fastpath: number
  avg_ms: number
}

let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  onUnauthorized = handler
}

function checkStatus(response: Response): void {
  if (response.status === 401) onUnauthorized?.()
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
  checkStatus(response)
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}))
    throw new Error((payload as { error?: string }).error || `${path} returned ${response.status}`)
  }
  return (await response.json()) as T
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  })
  checkStatus(response)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error((payload as { error?: string }).error || `${path} returned ${response.status}`)
  }
  return payload as T
}

async function deleteJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, {
    method: 'DELETE',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  })
  checkStatus(response)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error((payload as { error?: string }).error || `${path} returned ${response.status}`)
  }
  return payload as T
}

async function putJSON<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  })
  checkStatus(response)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error((payload as { error?: string }).error || `${path} returned ${response.status}`)
  }
  return payload as T
}

export interface SessionUser {
  user: string
  role: string
  permissions: string[]
}

export interface ConsoleUser {
  id: number
  username: string
  role: string
  enabled: boolean
  created_at: number
}

export const fetchUsers = () => getJSON<{ users: ConsoleUser[] }>('/users')
export const createUser = (username: string, password: string, role: string) =>
  postJSON<{ ok: boolean }>('/users', { username, password, role })
export const updateUser = (payload: {
  username: string
  role?: string
  enabled?: boolean
  password?: string
}) => putJSON<{ ok: boolean }>('/users', payload)
export const deleteUser = (username: string) =>
  deleteJSON<{ ok: boolean }>(`/users?username=${encodeURIComponent(username)}`)

// Auth calls deliberately bypass checkStatus: a failed login is expected to
// return 401 and must not be treated as an expired session.
async function authPost<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error((payload as { error?: string }).error || `${path} returned ${response.status}`)
  }
  return payload as T
}

export const getMe = () => getJSON<SessionUser>('/auth/me')
export const login = (username: string, password: string) =>
  authPost<SessionUser>('/auth/login', { username, password })
export const logout = () => authPost<{ ok: boolean }>('/auth/logout', {})

export interface HistoryRecord extends Verdict {
  id: string
  text: string
  at: number
}

export const fetchHealth = () => getJSON<Health>('/health')
export const fetchConfig = () => getJSON<Config>('/config')
export const fetchStats = () => getJSON<Stats>('/stats')
export const fetchHistory = () => getJSON<HistoryRecord[]>('/history')
export const clearHistory = () => deleteJSON<{ ok: boolean }>('/history')

export interface ReviewItem extends HistoryRecord {
  decision?: string
  note?: string
  reviewer?: string
  decided_at?: number
}

export interface ReviewsResponse {
  items: ReviewItem[]
  pending: number
}

export const fetchReviews = () => getJSON<ReviewsResponse>('/reviews')
export const decideReview = (id: string, decision: 'allow' | 'block', note: string) =>
  postJSON<{ ok: boolean }>('/reviews', { id: Number(id), decision, note })
export const reopenReview = (id: string) =>
  deleteJSON<{ ok: boolean }>(`/reviews?id=${encodeURIComponent(id)}`)

export interface DBTable {
  name: string
  rows: number
  columns: string[]
}

export interface DBResult {
  columns: string[]
  rows: Record<string, unknown>[]
  truncated: boolean
}

export interface DBDeleteResult {
  deleted: number
}

export type DBQueryResponse = DBResult | DBDeleteResult

export const fetchDBTables = () => getJSON<{ tables: DBTable[] }>('/db')
export const runDBQuery = (sql: string) => postJSON<DBQueryResponse>('/db/query', { sql })

export interface SetupInfo {
  account: { username: string; role: string }
  settings: {
    database_url: string
    model_dir: string
    python: string
    worker: string
    secure_cookie: boolean
  }
  status: {
    settings_file: string
    database: string
    model_dir_exists: boolean
    worker_exists: boolean
    restart_required: boolean
  }
}

export interface SetupUpdate {
  username?: string
  password?: string
  database_url?: string
  model_dir?: string
  python?: string
  worker?: string
  secure_cookie?: boolean
}

export interface SetupResult {
  ok: boolean
  settings_changed: boolean
  account_changed: boolean
}

export const fetchSetup = () => getJSON<SetupInfo>('/setup')
export const updateSetup = (update: SetupUpdate) => putJSON<SetupResult>('/setup', update)

export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  mod_time: string
  locked: boolean
  reason?: string
}

export interface FilePreview {
  name: string
  path: string
  size: number
  content?: string
  truncated: boolean
  locked: boolean
  reason?: string
  binary?: boolean
}

export interface FilesResponse {
  path: string
  parent: string
  is_dir: boolean
  entries?: FileEntry[]
  file?: FilePreview
}

export const fetchFiles = (path: string) =>
  getJSON<FilesResponse>(`/files?path=${encodeURIComponent(path)}`)

export interface TerminalResult {
  output: string
  cwd: string
  code: number
}

export const runTerminal = (id: string, command: string, cwd: string) =>
  postJSON<TerminalResult>('/terminal', { id, command, cwd })

export const cancelTerminal = (id: string) =>
  postJSON<{ cancelled: boolean }>('/terminal/cancel', { id })

export const checkMessage = (text: string) => postJSON<Verdict>('/moderate', { text })
export const checkMessages = (texts: string[]) =>
  postJSON<{ results: Verdict[] }>('/moderate/batch', { texts }).then((data) => data.results)

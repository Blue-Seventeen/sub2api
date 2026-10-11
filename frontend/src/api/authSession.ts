import type { AuthResponse } from '@/types'

export type AuthSessionID = string | null
const SESSION_KEY = 'auth_session_id'
const GENERATION_KEY = 'auth_session_generation'
const responseSessions = new WeakMap<object, AuthSessionID>()
const establishedResponses = new WeakMap<object, string>()
// Remember issued completion identities for the document's lifetime: a replaced callback token must
// not be interpreted as a fresh OAuth login.
const completionSessions = new Map<string, string>()
function newSessionID(): string {
  // getRandomValues also works on HTTP deployments; randomUUID requires a secure context.
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/** Local concurrency identity only; never an authorization credential. */
export function getAuthSessionID(): AuthSessionID {
  let id = localStorage.getItem(SESSION_KEY)
  // Upgrade a persisted login without changing its tokens or profile.
  if (!id && localStorage.getItem('auth_token')) {
    id = newSessionID()
    localStorage.setItem(SESSION_KEY, id)
  }
  if (id) return id
  // Shared across documents and retained after logout, so an anonymous operation cannot
  // become current again after another tab logs in and then removes its session ID.
  let generation = localStorage.getItem(GENERATION_KEY)
  if (!generation) {
    generation = newSessionID()
    localStorage.setItem(GENERATION_KEY, generation)
  }
  return `anonymous:${generation}`
}

export function authSessionChanged(): Error & { code: string; status: number } {
  return Object.assign(new Error('Session changed during token refresh or authentication.'), {
    code: 'AUTH_SESSION_CHANGED', status: 401
  })
}

export function assertAuthSession(session: AuthSessionID): void {
  if (getAuthSessionID() !== session) throw authSessionChanged()
}

export function rememberResponseSession(data: unknown, session: AuthSessionID): void {
  if (data && typeof data === 'object') responseSessions.set(data, session)
}

export function clearAuthSession(): void {
  localStorage.setItem(GENERATION_KEY, newSessionID())
  for (const key of ['auth_token', 'refresh_token', 'auth_user', 'token_expires_at', SESSION_KEY]) {
    localStorage.removeItem(key)
  }
}

interface SessionTokens { access_token: string; refresh_token?: string; expires_in?: number }

export function establishAuthSession(tokens: SessionTokens, expected: AuthSessionID): string {
  assertAuthSession(expected)
  clearAuthSession()
  const id = newSessionID()
  localStorage.setItem(SESSION_KEY, id)
  localStorage.setItem('auth_token', tokens.access_token)
  if (tokens.refresh_token) localStorage.setItem('refresh_token', tokens.refresh_token)
  if (tokens.expires_in) localStorage.setItem('token_expires_at', String(Date.now() + tokens.expires_in * 1000))
  return id
}

/** API persistence and Store adoption share the same commit identity. */
export function commitAuthResponse(response: AuthResponse, expected: AuthSessionID, persistForAdoption = false): string {
  const committed = establishedResponses.get(response)
  if (committed) {
    assertAuthSession(committed)
    return committed
  }
  if (responseSessions.has(response)) assertAuthSession(responseSessions.get(response)!)
  const id = establishAuthSession(response, expected)
  localStorage.setItem('auth_user', JSON.stringify(response.user))
  if (persistForAdoption) {
    establishedResponses.set(response, id)
    completionSessions.set(response.access_token, id)
  }
  return id
}

export function persistOAuthSession(tokens: SessionTokens): void {
  const committed = establishedResponses.get(tokens)
  if (committed) {
    assertAuthSession(committed)
    return
  }
  if (responseSessions.has(tokens)) assertAuthSession(responseSessions.get(tokens)!)
  const id = establishAuthSession(tokens, getAuthSessionID())
  establishedResponses.set(tokens, id)
  completionSessions.set(tokens.access_token, id)
}

export function adoptOAuthSession(token: string): string {
  const session = completionSessions.get(token)
  if (session) {
    assertAuthSession(session)
    return session
  }
  return establishAuthSession({ access_token: token }, getAuthSessionID())
}

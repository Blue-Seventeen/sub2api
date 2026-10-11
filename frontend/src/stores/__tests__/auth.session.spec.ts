import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import { apiClient } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { authAPI, exchangePendingOAuthCompletion, persistOAuthTokenContext } from '@/api/auth'
import { refreshAuthTokens } from '@/api/tokenRefresh'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const userFields = {
  balance: 100, concurrency: 5, status: 'active', allowed_groups: null,
  balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [],
  created_at: '2026-10-12T00:00:00Z', updated_at: '2026-10-12T00:00:00Z'
}
const admin = { ...userFields, id: 7, username: 'admin', email: 'admin@example.com', role: 'admin', run_mode: 'simple' }
const member = { ...userFields, id: 8, username: 'member', email: 'member@example.com', role: 'user', run_mode: 'standard' }
function tokens(name: string) {
  return { access_token: `${name}-access`, refresh_token: `${name}-refresh`, expires_in: 3600, token_type: 'Bearer' }
}
function response(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} }
}

describe('real auth Store/API session isolation', () => {
  let profile: ReturnType<typeof deferred<ReturnType<typeof response>>>
  let profileConfig: InternalAxiosRequestConfig
  let logoutResponse: ReturnType<typeof deferred<ReturnType<typeof response>>>
  let logoutConfig: InternalAxiosRequestConfig
  let store: ReturnType<typeof useAuthStore>
  let requests: InternalAxiosRequestConfig[]
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setActivePinia(createPinia())
    store = useAuthStore()
    requests = []
    profile = deferred()
    logoutResponse = deferred()
    profileConfig = undefined as unknown as InternalAxiosRequestConfig
    logoutConfig = undefined as unknown as InternalAxiosRequestConfig
    apiClient.defaults.adapter = async (config) => {
      requests.push(config)
      if (config.url === '/auth/me') { profileConfig = config; return profile.promise }
      if (config.url === '/auth/logout') { logoutConfig = config; return logoutResponse.promise }
      const body = JSON.parse(config.data || '{}')
      const account = body.email === admin.email ? admin : member
      return response(config, { ...tokens(account.username), user: account })
    }
    Object.defineProperty(navigator, 'locks', { configurable: true, value: undefined })
  })
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
  async function signIn(account = admin) {
    await store.login({ email: account.email, password: 'password' })
  }

  it.each(['success', '401'])('rejects late old admin profile %s after new user login', async (kind) => {
    vi.useFakeTimers()
    await signIn()
    const pending = store.refreshUser().catch((error) => error)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    await signIn(member)
    if (kind === 'success') profile.resolve(response(profileConfig, admin))
    else profile.reject({ config: profileConfig, response: { status: 401, data: {} } })
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.user?.id).toBe(8)
    expect(store.isAdmin).toBe(false)
    expect(store.runMode).toBe('standard')
    expect(localStorage.getItem('auth_token')).toBe('member-access')
    expect(requests.filter((r) => r.url === '/auth/me')).toHaveLength(1)
  })

  it('clears logout immediately and late revocation cannot clear a new login', async () => {
    vi.useFakeTimers()
    await signIn()
    const pending = store.logout().catch((error) => error)
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_token')).toBeNull()
    await vi.waitFor(() => expect(logoutConfig).toBeDefined())
    expect(JSON.parse(logoutConfig.data).refresh_token).toBe('admin-refresh')
    await signIn(member)
    logoutResponse.resolve(response(logoutConfig, {}))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.user?.id).toBe(8)
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it('rejects old profile after same account signs in again', async () => {
    vi.useFakeTimers()
    await signIn()
    const firstID = localStorage.getItem('auth_session_id')
    const pending = store.refreshUser().catch((error) => error)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    await signIn()
    profile.resolve(response(profileConfig, { ...admin, username: 'old-profile' }))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(localStorage.getItem('auth_session_id')).not.toBe(firstID)
    expect(store.user?.username).toBe('admin')
  })

  it('OAuth profile completion after logout rejects without restarting timers', async () => {
    vi.useFakeTimers()
    persistOAuthTokenContext(tokens('oauth'))
    const pending = store.setToken('oauth-access').catch((error) => error)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    const logout = store.logout()
    await vi.waitFor(() => expect(logoutConfig).toBeDefined())
    logoutResponse.resolve(response(logoutConfig, {}))
    await logout
    profile.resolve(response(profileConfig, admin))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.user).toBeNull()
    expect(localStorage.getItem('auth_session_id')).toBeNull()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('new session refresh does not share old session in-flight result', async () => {
    vi.useFakeTimers()
    await signIn()
    const oldNetwork = deferred<unknown>()
    const newNetwork = deferred<unknown>()
    vi.spyOn(axios, 'post').mockImplementationOnce(() => oldNetwork.promise as ReturnType<typeof axios.post>)
      .mockImplementationOnce(() => newNetwork.promise as ReturnType<typeof axios.post>)
    const old = refreshAuthTokens().catch((error) => error)
    await signIn(member)
    const sessionID = localStorage.getItem('auth_session_id')
    const current = refreshAuthTokens()
    expect(axios.post).toHaveBeenCalledTimes(2)
    newNetwork.resolve({ data: { code: 0, data: tokens('rotated-member') } })
    await expect(current).resolves.toMatchObject({ access_token: 'rotated-member-access' })
    oldNetwork.resolve({ data: { code: 0, data: tokens('rotated-admin') } })
    expect(await old).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(localStorage.getItem('auth_token')).toBe('rotated-member-access')
    expect(localStorage.getItem('auth_session_id')).toBe(sessionID)
  })

  it('API storage commit already invalidates an old Store profile before Store adoption', async () => {
    vi.useFakeTimers()
    await signIn()
    const pending = store.refreshUser().catch((error) => error)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    await authAPI.login({ email: member.email, password: 'password' })
    profile.resolve(response(profileConfig, admin))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(JSON.parse(localStorage.getItem('auth_user')!).id).toBe(8)
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it('late OAuth exchange tokens cannot replace a newer login at the persist boundary', async () => {
    vi.useFakeTimers()
    apiClient.defaults.adapter = async (config) => response(config, tokens('oauth'))
    const completion = await exchangePendingOAuthCompletion()
    apiClient.defaults.adapter = async (config) => response(config, { ...tokens('member'), user: member })
    await signIn(member)
    expect(() => persistOAuthTokenContext(completion)).toThrow(expect.objectContaining({ code: 'AUTH_SESSION_CHANGED' }))
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it('logout invalidates even a pending login that began anonymously', async () => {
    vi.useFakeTimers()
    const loginNetwork = deferred<ReturnType<typeof response>>()
    let config!: InternalAxiosRequestConfig
    apiClient.defaults.adapter = async (request) => { config = request; return loginNetwork.promise }
    const pending = store.login({ email: member.email, password: 'password' }).catch((error) => error)
    await vi.waitFor(() => expect(config).toBeDefined())
    await store.logout()
    loginNetwork.resolve(response(config, { ...tokens('member'), user: member }))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_token')).toBeNull()
  })

  it.each(['registration', 'OAuth'])('rejects %s continuation after its promotion await without damaging new login timers', async (entry) => {
    vi.useFakeTimers()
    const bind = deferred<ReturnType<typeof response>>()
    let bindConfig!: InternalAxiosRequestConfig
    const adapter = apiClient.defaults.adapter as (config: InternalAxiosRequestConfig) => Promise<ReturnType<typeof response>>
    apiClient.defaults.adapter = async (config) => {
      if (config.url === '/promotion/me/bind-referrer') { bindConfig = config; return bind.promise }
      return adapter(config)
    }
    sessionStorage.setItem('promotion_ref_code', 'REFERRER')
    let pending: Promise<unknown>
    if (entry === 'registration') pending = store.register({ email: admin.email, password: 'password' }).catch((error) => error)
    else {
      persistOAuthTokenContext(tokens('oauth'))
      pending = store.setToken('oauth-access').catch((error) => error)
      await vi.waitFor(() => expect(profileConfig).toBeDefined())
      profile.resolve(response(profileConfig, admin))
    }
    await vi.waitFor(() => expect(bindConfig).toBeDefined())
    await signIn(member)
    await vi.advanceTimersByTimeAsync(0)
    const timerCount = vi.getTimerCount()
    bind.resolve(response(bindConfig, {}))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.user?.id).toBe(8)
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(timerCount)
  })

  it('late proactive refresh cannot overwrite new login or restart its timer', async () => {
    vi.useFakeTimers()
    apiClient.defaults.adapter = async (config) => response(config, { ...tokens('admin'), expires_in: 121, user: admin })
    await signIn()
    const refresh = deferred<unknown>()
    vi.spyOn(axios, 'post').mockImplementationOnce(() => refresh.promise as ReturnType<typeof axios.post>)
    await vi.advanceTimersByTimeAsync(1000)
    expect(axios.post).toHaveBeenCalledTimes(1)
    apiClient.defaults.adapter = async (config) => response(config, { ...tokens('member'), user: member })
    await signIn(member)
    await vi.advanceTimersByTimeAsync(0)
    const timerCount = vi.getTimerCount()
    refresh.resolve({ data: { code: 0, data: tokens('stale') } })
    await vi.advanceTimersByTimeAsync(0)
    expect(store.token).toBe('member-access')
    expect(localStorage.getItem('auth_token')).toBe('member-access')
    expect(vi.getTimerCount()).toBe(timerCount)
  })

  it('migrates an existing login without replacing its token and keeps identity after profile updates', async () => {
    vi.useFakeTimers()
    localStorage.setItem('auth_token', 'legacy-access')
    localStorage.setItem('auth_user', JSON.stringify(member))
    store.checkAuth()
    expect(store.token).toBe('legacy-access')
    const sessionID = localStorage.getItem('auth_session_id')
    expect(sessionID).toBeTruthy()
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    profile.resolve(response(profileConfig, { ...member, username: 'updated' }))
    await vi.advanceTimersByTimeAsync(0)
    expect(store.user?.username).toBe('updated')
    expect(localStorage.getItem('auth_session_id')).toBe(sessionID)
  })

  it('a current profile 401 still clears both storage and Store state', async () => {
    vi.useFakeTimers()
    window.history.replaceState({}, '', '/login')
    await signIn()
    localStorage.removeItem('refresh_token')
    const pending = store.refreshUser().catch((error) => error)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    profile.reject({ config: profileConfig, response: { status: 401, data: { code: 'INVALID_TOKEN' } } })
    expect(await pending).toMatchObject({ status: 401, code: 'INVALID_TOKEN' })
    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_session_id')).toBeNull()
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('a late 401 from an already retried old request cannot clear the replacement session', async () => {
    vi.useFakeTimers()
    await signIn()
    const retry = deferred<ReturnType<typeof response>>()
    let retryConfig!: InternalAxiosRequestConfig
    let attempts = 0
    const adapter = apiClient.defaults.adapter as (config: InternalAxiosRequestConfig) => Promise<ReturnType<typeof response>>
    apiClient.defaults.adapter = async (config) => {
      if (config.url !== '/auth/me') return adapter(config)
      attempts++
      if (attempts === 1) throw { config, response: { status: 401, data: {} } }
      retryConfig = config
      return retry.promise
    }
    vi.spyOn(axios, 'post').mockResolvedValueOnce({ data: { code: 0, data: tokens('rotated-admin') } })
    const pending = store.refreshUser().catch((error) => error)
    await vi.waitFor(() => expect(retryConfig).toBeDefined())
    await signIn(member)
    retry.reject({ config: retryConfig, response: { status: 401, data: { code: 'REVOKED' } } })
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(store.user?.id).toBe(8)
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it.each(['registration', '2FA', 'Passkey'])('establishes a fresh session through %s with real API calls', async (entry) => {
    vi.useFakeTimers()
    await signIn()
    const prior = localStorage.getItem('auth_session_id')
    if (entry === 'registration') await store.register({ email: member.email, password: 'password' })
    if (entry === '2FA') await store.login2FA('temporary-token', '123456')
    if (entry === 'Passkey') {
      class Credential {
        id = 'credential'; rawId = new ArrayBuffer(1); type = 'public-key'; authenticatorAttachment = 'platform'
        response = { authenticatorData: new ArrayBuffer(1), clientDataJSON: new ArrayBuffer(1), signature: new ArrayBuffer(1), userHandle: null }
        getClientExtensionResults() { return {} }
      }
      vi.stubGlobal('PublicKeyCredential', Credential)
      Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get: async () => new Credential() } })
      apiClient.defaults.adapter = async (config) => response(config, config.url?.endsWith('/begin')
        ? { session_token: 'ceremony', options: { publicKey: { challenge: 'AA' } } }
        : { ...tokens('member'), user: member })
      await store.loginWithPasskey()
    }
    expect(store.user?.id).toBe(8)
    expect(store.runMode).toBe('standard')
    expect(localStorage.getItem('auth_session_id')).toBeTruthy()
    expect(localStorage.getItem('auth_session_id')).not.toBe(prior)
    expect(localStorage.getItem('refresh_token')).toBe('member-refresh')
  })

  it('old Passkey credential completion is rejected before sending finish as a new user', async () => {
    vi.useFakeTimers()
    await signIn()
    class Credential {
      id = 'credential'; rawId = new ArrayBuffer(1); type = 'public-key'; authenticatorAttachment = 'platform'
      response = { authenticatorData: new ArrayBuffer(1), clientDataJSON: new ArrayBuffer(1), signature: new ArrayBuffer(1), userHandle: null }
      getClientExtensionResults() { return {} }
    }
    vi.stubGlobal('PublicKeyCredential', Credential)
    const credential = deferred<Credential>()
    Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get: () => credential.promise } })
    const adapter = apiClient.defaults.adapter as (config: InternalAxiosRequestConfig) => Promise<ReturnType<typeof response>>
    apiClient.defaults.adapter = async (config) => {
      if (config.url === '/auth/passkey/login/begin') {
        requests.push(config)
        return response(config, { session_token: 'old-ceremony', options: { publicKey: { challenge: 'AA' } } })
      }
      return adapter(config)
    }
    const pending = store.loginWithPasskey().catch((error) => error)
    // Network boundary proves begin completed before replacing the authenticated account.
    await vi.waitFor(() => expect(requests.some((r) => r.url === '/auth/passkey/login/begin')).toBe(true))
    await signIn(member)
    credential.resolve(new Credential())
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(requests.some((r) => r.url === '/auth/passkey/login/finish')).toBe(false)
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it('OAuth 2FA API login followed by setToken adopts its already committed session and refresh context', async () => {
    vi.useFakeTimers()
    const completion = await authAPI.login2FA({ temp_token: 'oauth-temp', totp_code: '123456' })
    const id = localStorage.getItem('auth_session_id')
    const pending = store.setToken(completion.access_token)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    profile.resolve(response(profileConfig, member))
    await pending
    expect(localStorage.getItem('auth_session_id')).toBe(id)
    expect(localStorage.getItem('refresh_token')).toBe('member-refresh')
    expect(vi.getTimerCount()).toBeGreaterThanOrEqual(2)
  })

  it('an earlier API 2FA completion cannot be adopted after a replacement login', async () => {
    vi.useFakeTimers()
    apiClient.defaults.adapter = async (config) => response(config, { ...tokens('two-factor'), user: admin })
    const completion = await authAPI.login2FA({ temp_token: 'temp', totp_code: '123456' })
    apiClient.defaults.adapter = async (config) => response(config, { ...tokens('member'), user: member })
    await signIn(member)
    await expect(store.setToken(completion.access_token)).rejects.toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(localStorage.getItem('auth_token')).toBe('member-access')
  })

  it('establishes a session on HTTP deployments where secure-context randomUUID is unavailable', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('crypto', { getRandomValues: crypto.getRandomValues.bind(crypto) })
    await signIn(member)
    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('auth_session_id')).toBeTruthy()
  })

  it('WeChat 2FA login2FA then persistOAuthTokenContext then setToken preserves the committed session', async () => {
    vi.useFakeTimers()
    const completion = await authAPI.login2FA({ temp_token: 'wechat-temp', totp_code: '123456' })
    const id = localStorage.getItem('auth_session_id')
    const expiry = localStorage.getItem('token_expires_at')
    persistOAuthTokenContext(completion)
    expect(localStorage.getItem('auth_session_id')).toBe(id)
    expect(localStorage.getItem('refresh_token')).toBe('member-refresh')
    expect(localStorage.getItem('token_expires_at')).toBe(expiry)
    const pending = store.setToken(completion.access_token)
    await vi.waitFor(() => expect(profileConfig).toBeDefined())
    profile.resolve(response(profileConfig, member))
    await pending
    expect(store.user?.id).toBe(8)
    expect(localStorage.getItem('auth_session_id')).toBe(id)
  })

  it('WeChat 2FA completion persist still rejects after another session replaces it', async () => {
    vi.useFakeTimers()
    const completion = await authAPI.login2FA({ temp_token: 'wechat-temp', totp_code: '123456' })
    await signIn(admin)
    const id = localStorage.getItem('auth_session_id')
    expect(() => persistOAuthTokenContext(completion)).toThrow(expect.objectContaining({ code: 'AUTH_SESSION_CHANGED' }))
    expect(localStorage.getItem('auth_session_id')).toBe(id)
    expect(localStorage.getItem('auth_token')).toBe('admin-access')
  })

  it.each(['login then logout', 'anonymous logout'])('shared storage tabs reject deferred anonymous login after peer %s', async (peerAction) => {
    vi.useFakeTimers()
    // Independent module registries model two documents using one origin's localStorage.
    vi.resetModules()
    const tabA = await import('@/api/auth')
    const clientA = (await import('@/api/client')).apiClient
    const pendingNetwork = deferred<ReturnType<typeof response>>()
    let configA!: InternalAxiosRequestConfig
    clientA.defaults.adapter = async (config) => { configA = config; return pendingNetwork.promise }
    const pending = tabA.login({ email: member.email, password: 'password' }).catch((error) => error)
    await vi.waitFor(() => expect(configA).toBeDefined())

    vi.resetModules()
    const tabB = await import('@/api/auth')
    const clientB = (await import('@/api/client')).apiClient
    clientB.defaults.adapter = async (config) => response(config, { ...tokens('peer'), user: admin })
    if (peerAction === 'login then logout') await tabB.login({ email: admin.email, password: 'password' })
    await tabB.logout()
    expect(localStorage.getItem('auth_session_id')).toBeNull()
    expect(localStorage.getItem('auth_token')).toBeNull()
    pendingNetwork.resolve(response(configA, { ...tokens('late-a'), user: member }))
    expect(await pending).toMatchObject({ code: 'AUTH_SESSION_CHANGED' })
    expect(localStorage.getItem('auth_session_id')).toBeNull()
    expect(localStorage.getItem('auth_token')).toBeNull()
  })
})

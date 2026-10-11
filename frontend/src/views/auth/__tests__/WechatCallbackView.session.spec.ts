import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { InternalAxiosRequestConfig } from 'axios'
import WechatCallbackView from '@/views/auth/WechatCallbackView.vue'
import { apiClient } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { i18n } from '@/i18n'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const fields = {
  balance: 100, concurrency: 5, status: 'active', allowed_groups: null,
  balance_notify_enabled: false, balance_notify_threshold: null, balance_notify_extra_emails: [],
  created_at: '2026-10-12T00:00:00Z', updated_at: '2026-10-12T00:00:00Z'
}
const admin = { ...fields, id: 7, username: 'old-admin', email: 'admin@example.com', role: 'admin', run_mode: 'simple' }
const member = { ...fields, id: 8, username: 'new-user', email: 'member@example.com', role: 'user', run_mode: 'standard' }
function response(config: InternalAxiosRequestConfig, data: unknown) {
  return { config, data: { code: 0, data }, status: 200, statusText: 'OK', headers: {} }
}

describe('WeChat mounted callback with real Store/API session boundaries', () => {
  let wrapper: VueWrapper | undefined
  let requests: InternalAxiosRequestConfig[]
  let settings: ReturnType<typeof deferred<ReturnType<typeof response>>>
  let settingsConfig: InternalAxiosRequestConfig
  let errors: unknown[]
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    delete window.__APP_CONFIG__
    setActivePinia(createPinia())
    // The runtime-only i18n build accepts precompiled message functions as locale data.
    i18n.global.locale.value = 'en'
    i18n.global.missingWarn = false
    i18n.global.fallbackWarn = false
    i18n.global.setLocaleMessage('en', {
      auth: {
        wechatProviderName: () => 'WeChat', loginSuccess: () => 'Login successful',
        oidc: { callbackTitle: () => 'WeChat sign in', callbackProcessing: () => 'Signing in', callbackHint: () => 'Login interrupted' }
      },
      profile: { authBindings: { bindSuccess: () => 'Account bound' } }
    })
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] })
    requests = []
    errors = []
    settings = deferred()
    settingsConfig = undefined as unknown as InternalAxiosRequestConfig
    window.history.replaceState({}, '', '/auth/wechat/callback#access_token=legacy-admin-access&refresh_token=legacy-admin-refresh&expires_in=3600&redirect=%2Fdashboard')
    apiClient.defaults.adapter = async (config) => {
      requests.push(config)
      if (config.url === '/settings/public') { settingsConfig = config; return settings.promise }
      if (config.url === '/auth/login') return response(config, {
        access_token: 'member-access', refresh_token: 'member-refresh', expires_in: 3600,
        token_type: 'Bearer', user: member
      })
      if (config.url === '/auth/me') return response(config, config.headers.Authorization === 'Bearer legacy-admin-access' ? admin : member)
      if (config.url === '/auth/logout') return response(config, {})
      throw new Error(`Unexpected HTTP request: ${config.method} ${config.url}`)
    }
  })
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    delete window.__APP_CONFIG__
  })
  async function mountCallback() {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/auth/wechat/callback', component: WechatCallbackView },
        { path: '/dashboard', component: { template: '<div>Dashboard</div>' } }
      ]
    })
    await router.push('/auth/wechat/callback')
    wrapper = mount(WechatCallbackView, {
      global: { plugins: [i18n, router], config: { errorHandler: (error) => { errors.push(error) } } }
    })
    await flushPromises()
    expect(settingsConfig).toBeDefined()
    return router
  }

  for (const replacement of ['new login', 'logout']) {
    it.each(['success', 'failure'])(`stale fragment cannot commit after ${replacement} while settings %s is deferred`, async (outcome) => {
      await mountCallback()
      const store = useAuthStore()
      if (replacement === 'new login') await store.login({ email: member.email, password: 'password' })
      else await store.logout()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(0)
      const expectedID = localStorage.getItem('auth_session_id')
      const expectedToken = localStorage.getItem('auth_token')
      const expectedUser = localStorage.getItem('auth_user')
      const expectedRefresh = localStorage.getItem('refresh_token')
      const expectedExpiry = localStorage.getItem('token_expires_at')
      const expectedGeneration = localStorage.getItem('auth_session_generation')
      const expectedTimers = vi.getTimerCount()
      const requestCount = requests.length

      if (outcome === 'success') settings.resolve(response(settingsConfig, { wechat_oauth_open_enabled: true, wechat_oauth_mp_enabled: false }))
      else settings.reject({ config: settingsConfig, response: { status: 503, data: { message: 'Settings temporarily unavailable' } } })
      await flushPromises()

      expect(localStorage.getItem('auth_token')).toBe(expectedToken)
      expect(localStorage.getItem('auth_user')).toBe(expectedUser)
      expect(localStorage.getItem('refresh_token')).toBe(expectedRefresh)
      expect(localStorage.getItem('token_expires_at')).toBe(expectedExpiry)
      expect(localStorage.getItem('auth_session_generation')).toBe(expectedGeneration)
      expect(localStorage.getItem('auth_session_id')).toBe(expectedID)
      expect(store.token).toBe(expectedToken)
      expect(store.user?.id ?? null).toBe(replacement === 'new login' ? 8 : null)
      expect(store.isAdmin).toBe(false)
      expect(store.runMode).toBe('standard')
      expect(requests).toHaveLength(requestCount)
      expect(wrapper?.text()).toContain('Login interrupted')
      expect(wrapper?.text()).not.toContain('Signing in')
      expect(errors).toEqual([])
      expect(useAppStore().toasts).toContainEqual(expect.objectContaining({
        type: 'error', message: 'Session changed during token refresh or authentication.'
      }))
      // Allow the existing error-toast dismissal timer to finish; auth timers must survive unchanged.
      await vi.advanceTimersByTimeAsync(5000)
      expect(vi.getTimerCount()).toBe(expectedTimers)
    })
  }

  it('normal legacy fragment still establishes its profile and refresh timers after settings resolve', async () => {
    const router = await mountCallback()
    settings.resolve(response(settingsConfig, { wechat_oauth_open_enabled: true, wechat_oauth_mp_enabled: false }))
    await flushPromises()
    const store = useAuthStore()
    expect(errors).toEqual([])
    expect(store.user?.id).toBe(7)
    expect(store.isAdmin).toBe(true)
    expect(store.runMode).toBe('simple')
    expect(localStorage.getItem('auth_token')).toBe('legacy-admin-access')
    expect(localStorage.getItem('refresh_token')).toBe('legacy-admin-refresh')
    expect(requests.filter((request) => request.url === '/auth/me')).toHaveLength(1)
    expect(vi.getTimerCount()).toBeGreaterThanOrEqual(2)
    expect(router.currentRoute.value.path).toBe('/dashboard')
  })
})

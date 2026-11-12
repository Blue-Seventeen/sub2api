import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { adminAPI } from '@/api/admin'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const { showError, showWarning } = vi.hoisted(() => ({
  showError: vi.fn(),
  showWarning: vi.fn()
}))
const persistedAccounts = new Map<number, Record<string, unknown>>()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showWarning,
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      bulkUpdate: vi.fn(),
      getById: vi.fn(),
      checkMixedChannelRisk: vi.fn()
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function mountModal(extraProps: Record<string, unknown> = {}) {
  return mount(BulkEditAccountModal, {
    props: {
      show: true,
      accountIds: [1, 2],
      selectedPlatforms: ['antigravity'],
      selectedTypes: ['apikey'],
      proxies: [],
      groups: [],
      ...extraProps
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: `
            <select
              v-bind="$attrs"
              :value="modelValue"
              @change="$emit('update:modelValue', $event.target.value)"
            >
              <option v-for="option in options" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          `
        },
        ProxySelector: {
          props: ['modelValue', 'showAutoBestOption', 'autoBestValue', 'proxies'],
          emits: ['update:modelValue'],
          template: `
            <select
              data-testid="bulk-edit-proxy-selector"
              :value="modelValue ?? ''"
              @change="$emit('update:modelValue', $event.target.value === '' ? null : Number($event.target.value))"
            >
              <option value="">none</option>
              <option v-if="showAutoBestOption" :value="autoBestValue">auto-best</option>
              <option v-for="proxy in proxies" :key="proxy.id" :value="proxy.id">{{ proxy.name }}</option>
            </select>
          `
        },
        GroupSelector: true,
        Icon: true
      }
    }
  })
}

describe('BulkEditAccountModal', () => {
  beforeEach(() => {
    persistedAccounts.clear()
    vi.mocked(adminAPI.accounts.bulkUpdate).mockReset()
    vi.mocked(adminAPI.accounts.getById).mockReset()
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockReset()
    showError.mockReset()
    showWarning.mockReset()

    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: 1, ...persistedAccounts.get(id) } as any))
    vi.mocked(adminAPI.accounts.bulkUpdate).mockImplementation(async (idsOrPayload: any, changes?: any) => {
      const ids = Array.isArray(idsOrPayload) ? idsOrPayload : [1, 2]
      const payload = changes ?? idsOrPayload
      for (const id of ids) {
        const current = persistedAccounts.get(id) ?? {}
        const extra = { ...(current.extra as object ?? {}), ...(payload.extra ?? {}) }
        if ('upstream_billing_probe_enabled' in payload) extra.upstream_billing_probe_enabled = payload.upstream_billing_probe_enabled
        persistedAccounts.set(id, { ...current, ...payload, extra, credentials: { ...(current.credentials as object ?? {}), ...(payload.credentials ?? {}) } })
      }
      return { success: ids.length, failed: 0, success_ids: ids, results: [] } as any
    })
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockResolvedValue({
      has_risk: false
    } as any)
  })

  it('批量修改倍率时提示自动同步账号需要先关闭同步', async () => {
    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="bulk-rate-sync-warning"]').exists()).toBe(false)
    await wrapper.get('#bulk-edit-rate-multiplier-enabled').setValue(true)

    expect(wrapper.get('[data-testid="bulk-rate-sync-warning"]').text()).toContain(
      'admin.accounts.bulkEdit.rateSyncWarning'
    )
  })

  it('后端拒绝修改同步账号倍率时展示专用错误', async () => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockRejectedValueOnce({
      status: 409,
      reason: 'UPSTREAM_BILLING_RATE_SYNC_BULK_CONFLICT',
      metadata: { count: '2' },
      message: 'conflict'
    })
    const wrapper = mountModal()

    await wrapper.get('#bulk-edit-rate-multiplier-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.rateSyncConflict')
  })

  it('antigravity 白名单包含 Gemini 图片模型且过滤掉普通 GPT 模型', async () => {
    const wrapper = mountModal()
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    expect(selector.exists()).toBe(true)

    await selector.find('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('gemini-3.1-flash-image')
    expect(wrapper.text()).toContain('gemini-2.5-flash-image')
    expect(wrapper.text()).not.toContain('gpt-5.3-codex')
  })

  it('includes image mappings in Antigravity presets and filters OpenAI presets', async () => {
    const wrapper = mountModal()

    const mappingTab = wrapper.findAll('button').find((btn) => btn.text().includes('admin.accounts.modelMapping'))
    expect(mappingTab).toBeTruthy()
    await mappingTab!.trigger('click')

    const text = wrapper.text()
    expect(text).toMatch(/3\.1-Flash-Image(?: passthrough|透传)/)
    expect(text).toContain('3-Pro-Image')
    expect(text).not.toContain('GPT-5.3 Codex Spark')
  })

  it('submits null model_mapping when model restriction is enabled with an empty whitelist', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['anthropic'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {}
      }
    })
  })

  it('全部目标为 Grok OAuth 时，官方主机 base_url 作为手动端点切换正常提交', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://api.x.ai/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://api.x.ai/v1'
      }
    })
  })

  it('所选全为 grok 时展示快捷端点，点击后填入并自动勾选 base_url', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    const presets = wrapper.findAll('[data-testid="grok-base-url-preset"]')
    expect(presets.length).toBe(5)

    // 第三个预设为区域 API (us-east-1.api.x.ai/v1)
    await presets[2].trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://us-east-1.api.x.ai/v1'
      }
    })
  })

  it('所选含非 grok 平台时不展示快捷端点', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok', 'anthropic'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.findAll('[data-testid="grok-base-url-preset"]').length).toBe(0)
  })

  it('全部目标为 Grok OAuth 时，第三方 base_url 正常提交', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://relay.example.com/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://relay.example.com/v1'
      }
    })
  })

  it('混合类型选择（含 apikey）时官方主机 base_url 不拦截', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['grok'],
      selectedTypes: ['apikey', 'oauth']
    })

    await wrapper.get('#bulk-edit-base-url-enabled').setValue(true)
    await wrapper.get('#bulk-edit-base-url').setValue('https://api.x.ai/v1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        base_url: 'https://api.x.ai/v1'
      }
    })
  })

  it('enables OpenAI account passthrough in bulk edit', async () => {

    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
  })

  it('OpenAI OAuth 批量编辑可开启 namespace 摊平兼容开关', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-flatten-namespaces-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-flatten-namespaces-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_responses_flatten_namespaces: true
      }
    })
  })

  it('namespace 摊平开关不对 setup-token 等非 OAuth 选择展示', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth', 'setup-token']
    })

    expect(wrapper.find('#bulk-edit-openai-flatten-namespaces-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 OAuth 专属 WS mode 字段（含 http_bridge）', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('http_bridge')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_oauth_responses_websockets_v2_mode: 'http_bridge',
        openai_oauth_responses_websockets_v2_enabled: true
      }
    })
  })

  it('hides websocket mode entry for OpenAI API key bulk edit', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-openai-ws-mode-enabled').exists()).toBe(false)
  })

  it('submits codex_cli_only for OpenAI OAuth bulk edit', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true
      }
    })
  })

  it('submits codex_cli_only_allow_app_server when parent switch is enabled', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 子开关从属于 codex_cli_only：必须同时批量开启父开关才写入
    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true,
        codex_cli_only_allow_app_server: true
      }
    })
  })

  it('does not submit codex_cli_only_allow_app_server without the parent switch', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    // 仅开启子开关、不批量设置父开�?codex_cli_only：不应写入孤立字段，也不应调用接�?    await wrapper.get('#bulk-edit-openai-codex-app-server-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-app-server-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).not.toHaveBeenCalled()
  })

  it('submits API-key websocket mode for OpenAI API key bulk edit', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-apikey-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-apikey-ws-mode-select"]').setValue('ctx_pool')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_apikey_responses_websockets_v2_mode: 'ctx_pool',
        openai_apikey_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI API Key 批量编辑可统一开启上游倍率自动探测', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: true
    })
  })

  it('非 OpenAI 平台的 API Key 批量编辑同样可开启上游倍率自动探测', async () => {
    // 探测已放宽到全部 API-key 平台，混合平台选择只要求类型全为 apikey。
    const wrapper = mountModal({
      selectedPlatforms: ['grok', 'anthropic'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: true
    })
  })

  it('OpenAI API Key 批量编辑可统一关闭上游倍率自动探测', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-upstream-billing-auto-probe-select"]').setValue('disabled')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      upstream_billing_probe_enabled: false
    })
  })

  it('非 OpenAI API Key 目标不显示上游倍率自动探测批量开关', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    expect(wrapper.find('#bulk-edit-upstream-billing-auto-probe-enabled').exists()).toBe(false)
  })

  it('筛选结果批量编辑可统一开启上游倍率自动探测', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'filtered',
        filters: { platform: 'openai', type: 'apikey', status: 'active' },
        previewCount: 20,
        selectedPlatforms: ['openai'],
        selectedTypes: ['apikey']
      }
    })

    await wrapper.get('#bulk-edit-upstream-billing-auto-probe-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: { platform: 'openai', type: 'apikey', status: 'active' },
      upstream_billing_probe_enabled: true
    })
  })

  it('筛选 OpenAI 账号批量编辑应提交 Compact 模式和专属模型映射', async () => {
    const wrapper = mountModal({
      accountIds: [],
      selectedPlatforms: [],
      selectedTypes: [],
      target: {
        mode: 'filtered',
        filters: { platform: 'openai' },
        previewCount: 12,
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth', 'apikey']
      }
    })

    await wrapper.get('#bulk-edit-openai-compact-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-mode-select"]').setValue('force_on')
    await wrapper.get('#bulk-edit-openai-compact-model-mapping-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-compact-model-mapping-add"]').trigger('click')
    const inputs = wrapper.findAll('[data-testid="bulk-edit-openai-compact-model-mapping-input"]')
    await inputs[0].setValue('gpt-5.4')
    await inputs[1].setValue('gpt-5.4-openai-compact')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: { platform: 'openai' },
      extra: {
        openai_compact_mode: 'force_on'
      },
      credentials: {
        compact_model_mapping: {
          'gpt-5.4': 'gpt-5.4-openai-compact'
        }
      }
    })
  })

  it('disables OpenAI account passthrough in bulk edit', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: false,
        openai_oauth_passthrough: false
      }
    })
  })

  it('does not submit model restrictions when OpenAI passthrough is enabled', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
    expect(wrapper.text()).toContain('admin.accounts.openai.modelRestrictionDisabledByPassthrough')
  })


  // Exercise the real submit path for every enable flag, including false/0/[]/{}
  // values. The setup state supplies values; no payload builder is mocked.
  const fields = [
    { flag: 'enableBaseUrl', values: { baseUrl: 'https://relay.example/v1' }, payload: { credentials: { base_url: 'https://relay.example/v1' } }, clear: { baseUrl: '' }, cleared: null },
    { flag: 'enableModelRestriction', values: { allowedModels: ['claude-sonnet-4-5'] }, payload: { credentials: { model_mapping: { 'claude-sonnet-4-5': 'claude-sonnet-4-5' } } }, clear: { allowedModels: [] }, cleared: { credentials: { model_mapping: {} } } },
    { flag: 'enableCustomErrorCodes', values: { selectedErrorCodes: [401, 403] }, payload: { credentials: { custom_error_codes_enabled: true, custom_error_codes: [401, 403] } }, clear: { selectedErrorCodes: [] }, cleared: { credentials: { custom_error_codes_enabled: true, custom_error_codes: [] } } },
    { flag: 'enableInterceptWarmup', values: { interceptWarmupRequests: true }, payload: { credentials: { intercept_warmup_requests: true } }, clear: { interceptWarmupRequests: false }, cleared: { credentials: { intercept_warmup_requests: false } } },
    { flag: 'enableHeaderOverride', values: { headerOverrideEnabled: true, headerOverrideRows: [{ name: 'X-Test', value: 'fixture' }] }, payload: { credentials: { header_override_enabled: true, header_overrides: { 'x-test': 'fixture' } } }, clear: { headerOverrideEnabled: false }, cleared: { credentials: { header_override_enabled: false, header_overrides: {} } }, platform: 'openai', type: 'apikey' },
    { flag: 'enableProxy', values: { proxyId: 9 }, payload: { proxy_id: 9, extra: { auto_select_proxy: false } }, clear: { proxyId: null }, cleared: { proxy_id: 0, extra: { auto_select_proxy: false } } },
    { flag: 'enableConcurrency', values: { concurrency: 8 }, payload: { concurrency: 8 }, clear: { concurrency: 1 }, cleared: { concurrency: 1 } },
    { flag: 'enableLoadFactor', values: { loadFactor: 3 }, payload: { load_factor: 3 }, clear: { loadFactor: null }, cleared: { load_factor: 0 } },
    { flag: 'enablePriority', values: { priority: 5 }, payload: { priority: 5 }, clear: { priority: 0 }, cleared: { priority: 0 } },
    { flag: 'enableRateMultiplier', values: { rateMultiplier: 0.5 }, payload: { rate_multiplier: 0.5 }, clear: { rateMultiplier: 0 }, cleared: { rate_multiplier: 0 } },
    { flag: 'enableStatus', values: { status: 'inactive' }, payload: { status: 'inactive' }, clear: { status: 'active' }, cleared: { status: 'active' } },
    { flag: 'enableGroups', values: { groupIds: [3] }, payload: { group_ids: [3] }, clear: { groupIds: [] }, cleared: { group_ids: [] } },
    { flag: 'enableOpenAIPassthrough', values: { openaiPassthroughEnabled: true }, payload: { extra: { openai_passthrough: true } }, clear: { openaiPassthroughEnabled: false }, cleared: { extra: { openai_passthrough: false, openai_oauth_passthrough: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAIFlattenNamespaces', values: { openaiFlattenNamespacesEnabled: true }, payload: { extra: { openai_responses_flatten_namespaces: true } }, clear: { openaiFlattenNamespacesEnabled: false }, cleared: { extra: { openai_responses_flatten_namespaces: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAILongContextBilling', values: { openAILongContextBillingEnabled: true }, payload: { extra: { openai_long_context_billing_enabled: true } }, clear: { openAILongContextBillingEnabled: false }, cleared: { extra: { openai_long_context_billing_enabled: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAIEndpointCapabilities', values: { openAIEndpointCapabilities: ['embeddings'] }, payload: { credentials: { openai_capabilities: ['embeddings'] }, extra: { openai_responses_mode: null } }, clear: { openAIEndpointCapabilities: ['chat_completions', 'embeddings'] }, cleared: { credentials: { openai_capabilities: null } }, platform: 'openai', type: 'apikey' },
    { flag: 'enableOpenAIResponsesMode', values: { openAIResponsesMode: 'force_responses' }, payload: { extra: { openai_responses_mode: 'force_responses' } }, clear: { openAIResponsesMode: 'auto' }, cleared: { extra: { openai_responses_mode: null } }, platform: 'openai', type: 'apikey' },
    { flag: 'enableOpenAIWSMode', values: { openaiOAuthResponsesWebSocketV2Mode: 'http_bridge' }, payload: { extra: { openai_oauth_responses_websockets_v2_mode: 'http_bridge', openai_oauth_responses_websockets_v2_enabled: true } }, clear: { openaiOAuthResponsesWebSocketV2Mode: 'off' }, cleared: { extra: { openai_oauth_responses_websockets_v2_mode: 'off', openai_oauth_responses_websockets_v2_enabled: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAIAPIKeyWSMode', values: { openaiAPIKeyResponsesWebSocketV2Mode: 'passthrough' }, payload: { extra: { openai_apikey_responses_websockets_v2_mode: 'passthrough', openai_apikey_responses_websockets_v2_enabled: true } }, clear: { openaiAPIKeyResponsesWebSocketV2Mode: 'off' }, cleared: { extra: { openai_apikey_responses_websockets_v2_mode: 'off', openai_apikey_responses_websockets_v2_enabled: false } }, platform: 'openai', type: 'apikey' },
    { flag: 'enableUpstreamBillingAutoProbe', values: { upstreamBillingAutoProbeMode: 'enabled' }, payload: { upstream_billing_probe_enabled: true }, clear: { upstreamBillingAutoProbeMode: 'disabled' }, cleared: { upstream_billing_probe_enabled: false }, platform: 'openai', type: 'apikey' },
    { flag: 'enableCodexCLIOnly', values: { codexCLIOnlyEnabled: true }, payload: { extra: { codex_cli_only: true } }, clear: { codexCLIOnlyEnabled: false }, cleared: { extra: { codex_cli_only: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableCodexCLIOnlyAppServer', values: { enableCodexCLIOnly: true, codexCLIOnlyEnabled: true, codexCLIOnlyAppServerEnabled: true }, baseline: { extra: { codex_cli_only: true } }, payload: { extra: { codex_cli_only: true, codex_cli_only_allow_app_server: true } }, clear: { codexCLIOnlyAppServerEnabled: false }, cleared: { extra: { codex_cli_only: true, codex_cli_only_allow_app_server: false } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableCodexFingerprintMode', values: { codexFingerprintMode: 'device' }, payload: { extra: { codex_fingerprint_mode: 'device' } }, clear: { codexFingerprintMode: 'off' }, cleared: { extra: { codex_fingerprint_mode: 'off' } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAICompactMode', values: { openAICompactMode: 'force_on' }, payload: { extra: { openai_compact_mode: 'force_on' } }, clear: { openAICompactMode: 'auto' }, cleared: { extra: { openai_compact_mode: 'auto' } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableOpenAICompactModelMapping', values: { openAICompactModelMappings: [{ from: 'gpt-5.4', to: 'compact-fixture' }] }, payload: { credentials: { compact_model_mapping: { 'gpt-5.4': 'compact-fixture' } } }, clear: { openAICompactModelMappings: [] }, cleared: { credentials: { compact_model_mapping: {} } }, platform: 'openai', type: 'oauth' },
    { flag: 'enableRpmLimit', values: { rpmLimitEnabled: true, bulkBaseRpm: 30, bulkRpmStrategy: 'tiered', bulkRpmStickyBuffer: 5 }, payload: { extra: { base_rpm: 30, rpm_strategy: 'tiered', rpm_sticky_buffer: 5 } }, clear: { rpmLimitEnabled: false }, cleared: { extra: { base_rpm: 0, rpm_strategy: '', rpm_sticky_buffer: 0 } }, platform: 'anthropic', type: 'oauth' }
  ]

  it.each(fields)('$flag preserves unchecked fields and submits explicit set/reset values', async (field) => {
    const wrapper = mountModal({ selectedPlatforms: [field.platform ?? 'anthropic'], selectedTypes: [field.type ?? 'apikey'] })
    const state = (wrapper.vm as any).$.setupState
    Object.assign(state, field.values)
    expect(state.buildUpdatePayload()).toEqual(field.baseline ?? null)
    state[field.flag] = true
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    const expected = JSON.parse(JSON.stringify(field.payload))
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenLastCalledWith([1, 2], expected)
    vi.mocked(adminAPI.accounts.bulkUpdate).mockClear()
    Object.assign(state, field.clear)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    if (field.cleared === null) {
      expect(adminAPI.accounts.bulkUpdate).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.noFieldsSelected')
    } else {
      expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], JSON.parse(JSON.stringify(field.cleared)))
    }
    state[field.flag] = false
    expect(state.buildUpdatePayload()).toEqual(field.baseline ?? null)
    wrapper.unmount()
  })

  it.each([9, null])('explicitly disables automatic proxy selection for fixed/none proxy %s', async (proxyId) => {
    const wrapper = mountModal({ proxies: [{ id: 9, name: 'proxy-9' }] })
    await wrapper.get('#bulk-edit-proxy-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-proxy-selector"]').setValue(proxyId === null ? '' : '9')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      proxy_id: proxyId ?? 0, extra: { auto_select_proxy: false }
    })
  })

  it.each([null, 0])('clears the previous RPM sticky buffer override when enabled with buffer %s', async (buffer) => {
    const wrapper = mountModal({ selectedPlatforms: ['anthropic'], selectedTypes: ['oauth'] })
    const state = (wrapper.vm as any).$.setupState
    state.enableRpmLimit = true
    state.rpmLimitEnabled = true
    state.bulkBaseRpm = 30
    state.bulkRpmStickyBuffer = buffer
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: { base_rpm: 30, rpm_strategy: 'tiered', rpm_sticky_buffer: 0 }
    })
  })

  it('keeps partial failures open and emits only failed IDs for safe retry', async () => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success: 1, failed: 1, failed_ids: [2], success_ids: [1],
      results: [{ account_id: 1, success: true }, { account_id: 2, success: false, error: 'conflict' }]
    })
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('updated')).toEqual([[{ partial: true, failedIds: [2] }]])
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.partialSuccess')
  })

  it('surfaces committed scheduler cache refresh warnings without replaying the write', async () => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success: 2, failed: 0, success_ids: [1, 2], scheduler_sync_warning: 'cache unavailable', results: []
    } as any)
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.bulkEdit.schedulerSyncWarning')
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('does not accept an API success when the actual account readback disagrees', async () => {
    vi.mocked(adminAPI.accounts.getById).mockResolvedValue({ id: 1, priority: 99 } as any)
    const refreshAccounts = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountModal({ refreshAccounts })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.getById).toHaveBeenCalledWith(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.verificationFailed')
    wrapper.unmount()
  })

  it('awaits parent refresh acknowledgement and retries reads/refresh without repeating committed writes', async () => {
    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: 1 } as any))
    const refreshAccounts = vi.fn().mockRejectedValueOnce(new Error('refresh unavailable')).mockResolvedValue(undefined)
    const wrapper = mountModal({ refreshAccounts })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(refreshAccounts).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.refreshFailed')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(refreshAccounts).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('waits for a deferred parent refresh before announcing success or closing', async () => {
    let finishRefresh!: () => void
    const refreshAccounts = vi.fn(() => new Promise<void>(resolve => { finishRefresh = resolve }))
    const wrapper = mountModal({ refreshAccounts })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(refreshAccounts).toHaveBeenCalledOnce()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    finishRefresh()
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('retries failed readback without rewriting successful accounts and preserves API failures', async () => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success: 2, failed: 1, success_ids: [1, 2], failed_ids: [3], results: []
    })
    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: id === 2 ? 99 : 1 } as any))
    const wrapper = mountModal({ accountIds: [1, 2, 3] })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.emitted('updated')).toEqual([[{ partial: true, failedIds: [3, 2] }]])
    vi.mocked(adminAPI.accounts.getById).mockClear().mockImplementation(async id => ({ id, priority: 1 } as any))
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledOnce()
    expect(adminAPI.accounts.getById).toHaveBeenCalledOnce()
    expect(adminAPI.accounts.getById).toHaveBeenCalledWith(2)
    expect(wrapper.emitted('updated')?.at(-1)).toEqual([{ partial: true, failedIds: [3] }])
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    { success: 2, failed: 0, success_ids: [1], results: [] },
    { success: 2, failed: 0, success_ids: [1, 1], results: [] },
    { success: 1, failed: 1, success_ids: [1], failed_ids: [1], results: [] }
  ])('never replays a committed write after an inconsistent result response %#', async response => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce(response)
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledOnce()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.responseIncomplete')
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('retries only failed result IDs after a filtered partial update and never replays the filter', async () => {
    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: 0 } as any))
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success: 1, failed: 1,
      results: [{ account_id: 1, success: true }, { account_id: 2, success: false, error: 'conflict' }]
    })
    const wrapper = mountModal({
      accountIds: [], target: { mode: 'filtered', filters: { platform: 'anthropic' }, previewCount: 2, selectedPlatforms: ['anthropic'], selectedTypes: ['apikey'] }
    })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-priority').setValue(0)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenNthCalledWith(1, { filters: { platform: 'anthropic' }, priority: 0 })
    expect(wrapper.emitted('updated')).toEqual([[{ partial: true, failedIds: [2] }]])
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.setProps({ accountIds: [2], target: { mode: 'selected', selectedPlatforms: ['anthropic'], selectedTypes: ['apikey'] } })
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenNthCalledWith(2, [2], { priority: 0 })
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('does not close or report updated when every account fails', async () => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success: 0, failed: 2, failed_ids: [1, 2], results: []
    })
    const wrapper = mountModal()
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.failed')
    wrapper.unmount()
  })

  it.each([
    { mode: 'selected', success: 0 },
    { mode: 'filtered', success: 0 },
    { mode: 'selected', success: 1 },
    { mode: 'filtered', success: 1 }
  ])('never replays unconfirmed writes in $mode mode with $success reported successes', async ({ mode, success }) => {
    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValueOnce({
      success, failed: 2 - success, success_ids: success ? [1] : [],
      failed_ids: success ? [2] : [1, 2], unconfirmed_ids: [2], results: []
    } as any)
    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: 99 } as any))
    const refreshAccounts = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountModal({
      refreshAccounts,
      accountIds: mode === 'selected' ? [1, 2] : [],
      target: mode === 'filtered'
        ? { mode: 'filtered', filters: { platform: 'anthropic' }, previewCount: 2, selectedPlatforms: ['anthropic'], selectedTypes: ['apikey'] }
        : undefined
    })
    await wrapper.get('#bulk-edit-priority-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.verificationFailed')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('updated')).toBeUndefined()

    vi.mocked(adminAPI.accounts.getById).mockImplementation(async id => ({ id, priority: 1 } as any))
    refreshAccounts.mockRejectedValueOnce(new Error('fixture refresh unavailable'))
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.refreshFailed')
    expect(wrapper.emitted('close')).toBeUndefined()

    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.bulkEdit.unconfirmedResult')
    expect(adminAPI.accounts.getById).toHaveBeenCalledWith(2)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledOnce()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('updated')).toBeUndefined()
    wrapper.unmount()
  })

  it('omits all untouched fields and resets every enable flag when reopened', async () => {
    const wrapper = mountModal({ selectedPlatforms: ['openai'], selectedTypes: ['oauth'] })
    const state = (wrapper.vm as any).$.setupState
    expect(fields).toHaveLength(26)
    expect(state.buildUpdatePayload()).toBeNull()
    for (const field of fields) state[field.flag] = true
    state.userMsgQueueMode = 'serialize'
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    for (const field of fields) expect(state[field.flag], field.flag).toBe(false)
    expect(state.userMsgQueueMode).toBeNull()
    expect(state.buildUpdatePayload()).toBeNull()
    wrapper.unmount()
  })

  it.each(['', 'throttle', 'serialize'])('submits independent user message queue mode %s and clears its legacy boolean', async (mode) => {
    const wrapper = mountModal({ selectedPlatforms: ['anthropic'], selectedTypes: ['oauth'] })
    const state = (wrapper.vm as any).$.setupState
    state.userMsgQueueMode = mode
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], { extra: { user_msg_queue_mode: mode, user_msg_queue_enabled: false } })
    wrapper.unmount()
  })

  it('describes custom error codes as pool scheduling effects, not a blanket retry promise', () => {
    expect(zh.admin.accounts.customErrorCodesWarning).toContain('池模式')
    expect(zh.admin.accounts.customErrorCodesWarning).toContain('500')
    expect(en.admin.accounts.customErrorCodesWarning).toContain('pool mode')
    expect(en.admin.accounts.customErrorCodesWarning).toContain('500')
  })

  it('submits auto_select_proxy when choosing automatic best proxy in bulk edit', async () => {
    const wrapper = mountModal({
      proxies: [{ id: 9, name: 'proxy-9' }]
    })

    await wrapper.get('#bulk-edit-proxy-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-proxy-selector"]').setValue('-1')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      proxy_id: 0,
      extra: {
        auto_select_proxy: true
      }
    })
  })

})

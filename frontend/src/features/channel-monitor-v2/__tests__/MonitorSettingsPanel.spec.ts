import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'
import type { MonitorConfig } from '@/api/channelMonitorV2'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  groups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))
vi.mock('@/api/channelMonitorV2', () => ({
  getConfig: mocks.getConfig,
  updateConfig: mocks.updateConfig,
  MONITOR_ERROR_CATEGORIES: ['authentication', 'network'],
}))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: mocks.groups } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({
  cachedPublicSettings: { channel_monitor_enabled: true },
  showError: mocks.showError,
  showSuccess: mocks.showSuccess,
}) }))
vi.mock('@/utils/featureFlags', () => ({
  getChannelMonitorMode: () => 'v2',
  isChannelMonitorV2Mode: () => true,
}))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, te: () => false }),
}))

function config(): MonitorConfig {
  return {
    version: 2,
    enabled: true,
    refresh_interval_seconds: 60,
    platforms: [
      { platform: 'openai', enabled: true, models: ['gpt-4'] },
      { platform: 'custom-provider', enabled: true, models: ['custom-model'] },
    ],
    group_ids: [42],
    ignored_error_categories: [],
    health_thresholds: {
      minimum_sample: 70, warning_error_rate: 0.1, critical_error_rate: 0.3,
      target_ttft_ms: 1500, warning_ttft_ms: 2000, critical_ttft_ms: 5000,
      warning_cache_rate: 0.7, critical_cache_rate: 0.4,
      error_weight: 0.5, ttft_weight: 0.3, cache_weight: 0.2,
    },
  }
}

async function panel(value = config()) {
  mocks.getConfig.mockResolvedValue(value)
  const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { RouterLink: true, teleport: true } } })
  await flushPromises()
  return wrapper
}

function button(wrapper: Awaited<ReturnType<typeof panel>>, key: string) {
  return wrapper.findAll('button').find((item) => item.text() === 'channelMonitorV2.settings.' + key)!
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.groups.mockResolvedValue([])
  mocks.updateConfig.mockImplementation(async (value) => JSON.parse(JSON.stringify(value)))
})

describe('MonitorSettingsPanel derived group catalog', () => {
  it('renders the server-derived model catalog without manual platform/model controls', async () => {
    const original = config()
    const wrapper = await panel(original)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('input[type="text"]')).toHaveLength(0)
    expect(button(wrapper, 'addPlatform')).toBeUndefined()
    expect(wrapper.findAll('[data-testid="monitor-derived-model"]').map(item => item.text())).toEqual(['gpt-4', 'custom-model'])
    expect(wrapper.text()).toContain('channelMonitorV2.settings.catalogSource')
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(original.platforms).toHaveLength(2)
    wrapper.unmount()
  })

  it.each([
    {
      outcome: 'a different derived catalog',
      platforms: [{ platform: 'gemini', enabled: true, models: ['gemini-returned-by-server'] }],
      models: ['gemini-returned-by-server'],
    },
    { outcome: 'an empty derived catalog', platforms: [], models: [] },
  ])('keeps the saved catalog and unsaved-scope warning until saving returns $outcome', async ({ platforms, models }) => {
    mocks.groups.mockResolvedValue([{ id: 42, name: 'A', platform: 'openai' }, { id: 84, name: 'B', platform: 'openai' }])
    const original = config()
    let completeSave!: (value: MonitorConfig) => void
    mocks.updateConfig.mockReturnValueOnce(new Promise<MonitorConfig>((resolve) => { completeSave = resolve }))
    const wrapper = await panel(original)
    const catalog = () => wrapper.findAll('[data-testid="monitor-derived-model"]').map(item => item.text())
    const unsavedScope = () => wrapper.findAll('[role="status"]').some(item => item.text() === 'channelMonitorV2.settings.catalogAfterSave')

    expect(catalog()).toEqual(['gpt-4', 'custom-model'])
    expect(unsavedScope()).toBe(false)
    await wrapper.findAll('input[type="checkbox"]')[1].setValue(true)
    expect(catalog()).toEqual(['gpt-4', 'custom-model'])
    expect(unsavedScope()).toBe(true)

    await button(wrapper, 'save').trigger('click')
    expect(mocks.updateConfig).toHaveBeenCalledWith({ ...original, group_ids: [42, 84] })
    expect(catalog()).toEqual(['gpt-4', 'custom-model'])
    expect(unsavedScope()).toBe(true)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()

    completeSave({ ...original, group_ids: [42, 84], platforms })
    await flushPromises()
    expect(catalog()).toEqual(models)
    expect(unsavedScope()).toBe(false)
    expect(wrapper.text().includes('channelMonitorV2.settings.catalogEmpty')).toBe(models.length === 0)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(mocks.showSuccess).toHaveBeenCalledOnce()
    expect(original.group_ids).toEqual([42])
    expect(original.platforms.map(platform => platform.models).flat()).toEqual(['gpt-4', 'custom-model'])
    wrapper.unmount()
  })

  it('restores the server group selection and catalog when saving fails', async () => {
    mocks.groups.mockResolvedValue([{ id: 42, name: 'A', platform: 'openai' }])
    const wrapper = await panel()
    mocks.updateConfig.mockRejectedValueOnce(new Error('save failed'))
    await wrapper.find('input[type="checkbox"]').setValue(false)
    await button(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledOnce()
    expect((wrapper.find('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.findAll('[data-testid="monitor-derived-model"]')).toHaveLength(2)
    expect(wrapper.findAll('input[type="text"]')).toHaveLength(0)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('explains an empty catalog without falling back to all platform models', async () => {
    const wrapper = await panel({ ...config(), platforms: [] })
    expect(wrapper.text()).toContain('channelMonitorV2.settings.catalogEmpty')
    expect(wrapper.findAll('[data-testid="monitor-derived-model"]')).toHaveLength(0)
    expect(button(wrapper, 'addPlatform')).toBeUndefined()
    wrapper.unmount()
  })
})

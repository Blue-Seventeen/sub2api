import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createPinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import GroupsView from '../GroupsView.vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'

const { list, create, update, showError, mode } = vi.hoisted(() => ({
  list: vi.fn(), create: vi.fn(), update: vi.fn(), showError: vi.fn(),
  mode: { simple: false },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: mode.simple }) }))
vi.mock('@/api/admin', () => ({ adminAPI: {
  groups: {
    list, create, update, getAll: vi.fn(async () => []),
    getModelAllowlistCandidates: vi.fn(async () => []),
    getModelsListCandidates: vi.fn(async () => []),
    getUsageSummary: vi.fn(async () => []), getCapacitySummary: vi.fn(async () => []),
    getLiveCapability: vi.fn(async () => ({ supported: false })),
  }, accounts: { list: vi.fn(async () => ({ items: [] })) },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))
vi.mock('@/stores/onboarding', () => ({ useOnboardingStore: () => ({
  isCurrentStep: () => false, nextStep: vi.fn(),
}) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const group = {
  id: 42, name: 'existing', platform: 'openai', rate_multiplier: 1.25,
  status: 'active', subscription_type: 'standard', rpm_limit: 0,
  daily_limit_usd: null, weekly_limit_usd: null, monthly_limit_usd: null,
  custom_limit_hours: 12, custom_limit_usd: 7,
  long_context_pricing_enabled: false,
  peak_rate_enabled: true, peak_rate_windows: [{ start: '09:00', end: '12:00', multiplier: 0 }],
  supported_model_scopes: [], model_routing: null, model_routing_enabled: false,
  fallback_group_id: null, fallback_group_id_on_invalid_request: null,
  codex_models_manifest_config: { enabled: false, account_ids: [], fallback_to_scheduler: false },
  model_pricing: [{
    platform: 'openai', models: ['gpt-6.1-sol'], billing_mode: 'token',
    input_price: 0.000002, output_price: 0.000008, cache_read_price: 0,
    cache_write_price: null, cache_write_1h_price: 0.000004,
    image_input_price: null, image_output_price: null, per_request_price: null,
    fast_multiplier: 2, flex_multiplier: 0.5,
    reasoning_effort_multipliers: { high: 1.5 }, intervals: [], time_pricing: null,
  }],
}
const layout = defineComponent({ template: '<main><slot /></main>' })
const tableLayout = defineComponent({
  template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>',
})
const table = defineComponent({
  props: ['data'], template: '<div><slot v-if="data.length" name="cell-actions" :row="data[0]" /></div>',
})
const dialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const manifest = defineComponent({
  setup(_, { expose }) { expose({ validate: () => true, resetValidation: () => undefined }) },
  template: '<div />',
})
let wrapper: VueWrapper
const mountView = () => {
  wrapper = mount(GroupsView, { global: { plugins: [createPinia()], stubs: {
    AppLayout: layout, TablePageLayout: tableLayout, DataTable: table, BaseDialog: dialog,
    Pagination: true, ConfirmDialog: true, EmptyState: true, Select: true,
    PlatformIcon: true, Icon: true, GroupCapacityBadge: true, GroupRateMultipliersModal: true,
    GroupRPMOverridesModal: true,
    CodexManifestAccountsField: manifest, ModelTagInput: true, VueDraggable: true,
  } } })
  return wrapper
}
const clickText = async (text: string) => {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  expect(button, text).toBeTruthy()
  await button!.trigger('click')
  await flushPromises()
}
const edit = async () => { mountView(); await flushPromises(); await clickText('common.edit') }

describe('GroupsView pricing UI and submission', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mode.simple = false
    localStorage.clear()
    list.mockResolvedValue({ items: [structuredClone(group)], total: 1, page: 1, page_size: 20, pages: 1 })
    create.mockResolvedValue({})
    update.mockResolvedValue({})
  })
  afterEach(() => wrapper?.unmount())

  it('renders the persisted false switch and saves an enabled group without changing custom rates', async () => {
    await edit()
    const toggle = wrapper.get<HTMLInputElement>('[data-testid="edit-group-long-context"]')
    expect(toggle.element.checked).toBe(false)
    await toggle.setValue(true)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(42, expect.objectContaining({
      long_context_pricing_enabled: true, rate_multiplier: 1.25,
      custom_limit_hours: 12, custom_limit_usd: 7,
      peak_rate_windows: [{ start: '09:00', end: '12:00', multiplier: 0 }],
    }))
  })

  it('saves a disabled switch and reloads the stored state after cancelling edits', async () => {
    list.mockResolvedValue({ items: [{ ...structuredClone(group), long_context_pricing_enabled: true }], total: 1 })
    await edit()
    await wrapper.get('[data-testid="edit-group-long-context"]').setValue(false)
    await clickText('common.cancel')
    expect(update).not.toHaveBeenCalled()
    await clickText('common.edit')
    expect(wrapper.get<HTMLInputElement>('[data-testid="edit-group-long-context"]').element.checked).toBe(true)
    await wrapper.get('[data-testid="edit-group-long-context"]').setValue(false)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0][1].long_context_pricing_enabled).toBe(false)
  })

  it('edits USD per MTok and preserves zero cache prices and existing multipliers', async () => {
    await edit()
    const card = wrapper.getComponent(PricingEntryCard)
    const inputs = card.findAll('input[type="number"]')
    expect((inputs[0].element as HTMLInputElement).value).toBe('2')
    await inputs[0].setValue('3')
    expect(wrapper.getComponent(PricingEntryCard).element).toBe(card.element)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0][1].model_pricing).toEqual([expect.objectContaining({
      models: ['gpt-6.1-sol'], input_price: 0.000003, output_price: 0.000008,
      cache_read_price: 0, cache_write_1h_price: 0.000004,
      fast_multiplier: 2, flex_multiplier: 0.5, reasoning_effort_multipliers: { high: 1.5 },
      intervals: [], time_pricing: null,
    })])
    expect(group.model_pricing[0].input_price).toBe(0.000002)
  })

  it('clears pricing explicitly when the last card is removed', async () => {
    await edit()
    await wrapper.getComponent(PricingEntryCard).find('button').trigger('click')
    expect(wrapper.findComponent(PricingEntryCard).exists()).toBe(false)
    await wrapper.get('#edit-group-form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0][1].model_pricing).toEqual([])
  })

  it('shows the official default on create and saves an explicit opt out', async () => {
    mountView(); await flushPromises(); await clickText('admin.groups.createGroup')
    const toggle = wrapper.get<HTMLInputElement>('[data-testid="create-group-long-context"]')
    expect(toggle.element.checked).toBe(true)
    await toggle.setValue(false)
    await wrapper.get('#create-group-form input[type="text"]').setValue('new group')
    await wrapper.get('#create-group-form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'new group', long_context_pricing_enabled: false, model_pricing: [],
    }))
  })

  it('hides token intervals for group cards while retaining channel defaults and per-request tiers', async () => {
    await edit()
    expect(wrapper.getComponent(PricingEntryCard).text()).not.toContain('admin.channels.form.addInterval')
    const channelCard = mount(PricingEntryCard, {
      props: { entry: wrapper.getComponent(PricingEntryCard).props('entry') },
      global: { stubs: { ModelTagInput: true, Select: true, Icon: true } },
    })
    expect(channelCard.text()).toContain('admin.channels.form.addInterval')
    channelCard.unmount()
    const card = wrapper.getComponent(PricingEntryCard)
    card.vm.$emit('update', { ...card.props('entry'), billing_mode: 'per_request', per_request_price: 0.02,
      intervals: [{ min_tokens: 0, max_tokens: null, tier_label: 'tts', input_price: null,
        output_price: null, cache_write_price: null, cache_read_price: null,
        per_request_price: 0.03, sort_order: 0 }],
    })
    await flushPromises()
    expect(card.text()).toContain('admin.channels.form.addTier')
    await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
    expect(update.mock.calls[0][1].model_pricing[0]).toMatchObject({
      billing_mode: 'per_request', per_request_price: 0.02, intervals: [{ tier_label: 'tts', per_request_price: 0.03 }],
    })
  })

  it('keeps the edit dialog open when the server rejects pricing', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    update.mockRejectedValue({ response: { data: { message: 'pricing rejected' } } })
    await edit()
    await wrapper.get('[data-testid="edit-group-long-context"]').setValue(true)
    await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
    expect(showError).toHaveBeenCalledWith('pricing rejected')
    expect(wrapper.get<HTMLInputElement>('[data-testid="edit-group-long-context"]').element.checked).toBe(true)
    consoleError.mockRestore()
  })

  it('keeps new cards mounted across updates and does not save cancelled price edits', async () => {
    await edit()
    await clickText('admin.groups.modelPricing.add')
    const cards = wrapper.findAllComponents(PricingEntryCard)
    const added = cards[1]
    added.vm.$emit('update', { ...added.props('entry'), models: ['gpt-extra'] })
    await flushPromises()
    expect(wrapper.findAllComponents(PricingEntryCard)[1].element).toBe(added.element)
    await added.findAll('input[type="number"]')[0].setValue('1.5')
    expect(wrapper.findAllComponents(PricingEntryCard)[1].element).toBe(added.element)
    await clickText('common.cancel')
    expect(update).not.toHaveBeenCalled()
    await clickText('common.edit')
    expect(wrapper.findAllComponents(PricingEntryCard)).toHaveLength(1)
    expect(wrapper.getComponent(PricingEntryCard).props('entry').input_price).toBe(2)
  })

  it('hides billing configuration in simple mode and omits pricing from submissions', async () => {
    mode.simple = true
    mountView(); await flushPromises()
    await clickText('common.edit')
    expect(wrapper.find('[data-testid="edit-group-long-context"]').exists()).toBe(false)
    expect(wrapper.findComponent(PricingEntryCard).exists()).toBe(false)
    await wrapper.get('#edit-group-form').trigger('submit'); await flushPromises()
    const payload = JSON.parse(JSON.stringify(update.mock.calls[0][1]))
    expect(payload).not.toHaveProperty('long_context_pricing_enabled')
    expect(payload).not.toHaveProperty('model_pricing')
    await clickText('admin.groups.createGroup')
    expect(wrapper.find('[data-testid="create-group-long-context"]').exists()).toBe(false)
    await wrapper.get('#create-group-form input[type="text"]').setValue('simple group')
    await wrapper.get('#create-group-form').trigger('submit'); await flushPromises()
    const createPayload = JSON.parse(JSON.stringify(create.mock.calls[0][0]))
    expect(createPayload).not.toHaveProperty('long_context_pricing_enabled')
    expect(createPayload).not.toHaveProperty('model_pricing')
  })
})

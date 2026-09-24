import { defineComponent } from 'vue'
import { DOMWrapper, enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import GroupsView from '../GroupsView.vue'
import ModelListScopeDialog from '@/components/admin/group/ModelListScopeDialog.vue'
import type { AdminGroup } from '@/types'

const { groups, showSuccess, showError, showWarning } = vi.hoisted(() => ({
  groups: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    getModelsListCandidates: vi.fn(),
    getUsageSummary: vi.fn(),
    getCapacitySummary: vi.fn(),
    getLiveCapability: vi.fn(),
  },
  showSuccess: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/admin', () => ({ adminAPI: { groups } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError, showWarning }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false }) }))
vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep: () => false }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown> | string) =>
      typeof params === 'string' ? params : params ? `${key} ${JSON.stringify(params)}` : key,
  }),
}))

const sourceGroup: AdminGroup = {
  id: 42,
  name: 'Primary',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  default_mapped_model: '',
  require_oauth_only: false,
  require_privacy_set: false,
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  models_list_config: { enabled: true, models: ['gpt-keep', 'gpt-remove'] },
  account_count: 0,
  active_account_count: 0,
  rate_limited_account_count: 0,
  sort_order: 10,
  created_at: '2026-09-24T00:00:00Z',
  updated_at: '2026-09-24T00:00:00Z',
}

type Mode = 'create' | 'edit'
type Scope = 'group' | 'global'
type Root = VueWrapper | DOMWrapper<Element>

const button = (root: Root, text: string) => {
  const found = root.findAll('button').find(node => node.text() === text)
  expect(found, `button ${text}`).toBeDefined()
  return found!
}
const form = (wrapper: VueWrapper, mode: Mode) => wrapper.get(`#${mode}-group-form`)
const modelsSection = (wrapper: VueWrapper, mode: Mode) => {
  const label = form(wrapper, mode).findAll('label')
    .find(node => node.text() === 'admin.groups.modelsList.title')!
  return new DOMWrapper(label.element.closest('.border-t')!)
}
const modelRow = (wrapper: VueWrapper, mode: Mode, model: string) => {
  const label = modelsSection(wrapper, mode).findAll('span').find(node => node.text() === model)!
  expect(label, `model ${model}`).toBeDefined()
  return new DOMWrapper(label.element.parentElement!)
}
const mutation = (mode: Mode) => mode === 'create' ? groups.create : groups.update
const lastPayload = (mode: Mode) => mutation(mode).mock.calls.at(-1)![mode === 'create' ? 0 : 1]

function mountView() {
  // Keep the form, scope dialog, BaseDialog and model-list helpers real.
  return mount(GroupsView, {
    global: {
      stubs: {
        teleport: true,
        AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        TablePageLayout: defineComponent({
          template: '<section><slot name="filters" /><slot name="table" /></section>',
        }),
        DataTable: defineComponent({
          props: ['data'],
          template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>',
        }),
        Pagination: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        EmptyState: true,
        ConfirmDialog: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        VueDraggable: true,
      },
    },
  })
}

async function openForm(wrapper: VueWrapper, mode: Mode) {
  await flushPromises()
  await button(wrapper, mode === 'create' ? 'admin.groups.createGroup' : 'common.edit').trigger('click')
  await flushPromises()
  if (mode === 'create') {
    const platformButtons = form(wrapper, mode).get('[data-tour="group-form-platform"]').findAll('button')
    await platformButtons.find(node => node.find('platform-icon-stub[platform="openai"]').exists())!.trigger('click')
    await flushPromises()
    await modelsSection(wrapper, mode).get('button').trigger('click')
  }
  await form(wrapper, mode).get('input[required]').setValue('Scoped group')
  await form(wrapper, mode).get('textarea').setValue('Unsaved description')
}

async function chooseScope(wrapper: VueWrapper, scope: Scope) {
  const dialog = wrapper.getComponent(ModelListScopeDialog)
  expect(dialog.props('show')).toBe(true)
  const choice = dialog.findAll('button').find(node => node.text().startsWith(`admin.groups.modelsList.${scope}Scope`))!
  await choice.trigger('click')
  expect(dialog.props('show')).toBe(false)
}

async function addModel(wrapper: VueWrapper, mode: Mode, scope: Scope, commit = true) {
  await button(modelsSection(wrapper, mode), 'admin.groups.modelsList.add').trigger('click')
  await chooseScope(wrapper, scope)
  const input = modelsSection(wrapper, mode).get('input[type="text"]')
  await input.setValue('  custom-model  ')
  if (commit) await input.trigger('keydown', { key: 'Enter' })
}

enableAutoUnmount(afterEach)

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  groups.list.mockResolvedValue({ items: [sourceGroup], total: 1, page: 1, page_size: 20, pages: 1 })
  groups.create.mockReset().mockResolvedValue(sourceGroup)
  groups.update.mockReset().mockResolvedValue(sourceGroup)
  groups.getModelsListCandidates.mockReset().mockImplementation((_id: number, platform: string) =>
    Promise.resolve(platform === 'gemini' ? ['gemini-fresh'] : ['gpt-keep', 'gpt-remove']),
  )
  groups.getUsageSummary.mockResolvedValue([])
  groups.getCapacitySummary.mockResolvedValue([])
  groups.getLiveCapability.mockResolvedValue({ supported: false })
})

afterEach(() => vi.restoreAllMocks())

describe.each<Mode>(['create', 'edit'])('GroupsView %s model-list workflow', mode => {
  it.each<Scope>(['group', 'global'])('submits %s add and row delete with the selected scope', async scope => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, scope)
    await button(modelRow(wrapper, mode, 'gpt-remove'), 'admin.groups.modelsList.delete').trigger('click')
    await chooseScope(wrapper, scope)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()

    expect(mutation(mode)).toHaveBeenCalledTimes(1)
    if (mode === 'edit') expect(groups.update.mock.calls[0][0]).toBe(42)
    expect(lastPayload(mode)).toMatchObject({
      name: 'Scoped group',
      platform: 'openai',
      models_list_config: { enabled: true, models: ['custom-model', 'gpt-keep'] },
      global_model_operations: scope === 'global'
        ? [{ operation: 'add', model: 'custom-model' }, { operation: 'remove', model: 'gpt-remove' }]
        : [],
    })
    expect(wrapper.find(`#${mode}-group-form`).exists()).toBe(false)
    expect(showError).not.toHaveBeenCalled()
    expect(groups.list).toHaveBeenCalledTimes(2)

    await openForm(wrapper, mode)
    expect(modelsSection(wrapper, mode).text()).not.toContain('custom-model')
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode).global_model_operations).toEqual([])
  })

  it('queues only selected rows for global bulk deletion', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await modelRow(wrapper, mode, 'gpt-keep').get('input[type="checkbox"]').setValue(false)
    await button(modelsSection(wrapper, mode), 'admin.groups.modelsList.delete').trigger('click')
    expect(wrapper.getComponent(ModelListScopeDialog).props()).toMatchObject({ operation: 'remove', selectedCount: 1 })
    await chooseScope(wrapper, 'global')
    expect(modelRow(wrapper, mode, 'gpt-keep').exists()).toBe(true)
    await modelRow(wrapper, mode, 'gpt-keep').get('input[type="checkbox"]').setValue(true)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode)).toMatchObject({
      models_list_config: { enabled: true, models: ['gpt-keep'] },
      global_model_operations: [{ operation: 'remove', model: 'gpt-remove' }],
    })
  })

  it('keeps pending global operations when the custom model list is disabled', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global')
    await modelsSection(wrapper, mode).get('button').trigger('click')
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode)).toMatchObject({
      models_list_config: { enabled: false, models: ['custom-model', 'gpt-keep', 'gpt-remove'] },
      global_model_operations: [{ operation: 'add', model: 'custom-model' }],
    })
  })

  it('does not change models or queue operations when the scope dialog is cancelled', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    for (const action of ['add', 'delete']) {
      await button(modelsSection(wrapper, mode), `admin.groups.modelsList.${action}`).trigger('click')
      await button(wrapper.getComponent(ModelListScopeDialog), 'common.cancel').trigger('click')
    }
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode)).toMatchObject({
      models_list_config: { enabled: true, models: ['gpt-keep', 'gpt-remove'] },
      global_model_operations: [],
    })
  })

  it('dismisses only the scope dialog on Escape and retains the parent form', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global')
    await button(modelsSection(wrapper, mode), 'admin.groups.modelsList.delete').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(wrapper.getComponent(ModelListScopeDialog).props('show')).toBe(false)
    expect(wrapper.find(`#${mode}-group-form`).exists()).toBe(true)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode).global_model_operations).toEqual([{ operation: 'add', model: 'custom-model' }])
  })

  it.each([
    { added_models: ['custom-model'], removed_models: null },
    { added_models: null, removed_models: ['gpt-remove'] },
    { added_models: null, removed_models: null },
    {},
  ])('finishes a successful save with nullable or missing summary arrays: %j', async summary => {
    mutation(mode).mockResolvedValueOnce({
      ...sourceGroup,
      global_model_operation_summary: { target_platform: 'openai', affected_group_count: 3, ...summary },
    })
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global')
    await form(wrapper, mode).trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.find(`#${mode}-group-form`).exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalledWith(mode === 'create' ? 'admin.groups.groupCreated' : 'admin.groups.groupUpdated')
    expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('"platform":"OpenAI"'))
    expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('"count":3'))
    if (summary.added_models) {
      expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('summaryAdded'))
      expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('custom-model'))
    }
    if (summary.removed_models) {
      expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('summaryRemoved'))
      expect(showSuccess).toHaveBeenCalledWith(expect.stringContaining('gpt-remove'))
    }
    expect(groups.list).toHaveBeenCalledTimes(2)
  })

  it('retains form edits and pending operations after failure and resubmits them on retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mutation(mode).mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global')
    await button(modelRow(wrapper, mode, 'gpt-remove'), 'admin.groups.modelsList.delete').trigger('click')
    await chooseScope(wrapper, 'global')
    await form(wrapper, mode).trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledTimes(1)
    expect(showSuccess).not.toHaveBeenCalled()
    expect(form(wrapper, mode).get<HTMLInputElement>('input[required]').element.value).toBe('Scoped group')
    expect(form(wrapper, mode).get<HTMLTextAreaElement>('textarea').element.value).toBe('Unsaved description')
    expect(modelsSection(wrapper, mode).text()).toContain('custom-model')
    expect(modelsSection(wrapper, mode).text()).not.toContain('gpt-remove')
    expect(wrapper.get(`button[form="${mode}-group-form"][type="submit"]`).attributes('disabled')).toBeUndefined()
    const failedPayload = lastPayload(mode)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(mutation(mode)).toHaveBeenCalledTimes(2)
    expect(lastPayload(mode)).toEqual(failedPayload)
    expect(lastPayload(mode).global_model_operations).toEqual([
      { operation: 'add', model: 'custom-model' },
      { operation: 'remove', model: 'gpt-remove' },
    ])
    expect(wrapper.find(`#${mode}-group-form`).exists()).toBe(false)
  })

  it('includes an active global-add draft when the form is submitted without blur', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global', false)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode)).toMatchObject({
      models_list_config: { enabled: true, models: ['custom-model', 'gpt-keep', 'gpt-remove'] },
      global_model_operations: [{ operation: 'add', model: 'custom-model' }],
    })
  })

  it('retains the active global-add draft on failure and includes it again on retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mutation(mode).mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await addModel(wrapper, mode, 'global', false)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()

    expect(modelsSection(wrapper, mode).get<HTMLInputElement>('input[type="text"]').element.value).toBe('  custom-model  ')
    expect(lastPayload(mode).global_model_operations).toEqual([{ operation: 'add', model: 'custom-model' }])
    const failedPayload = lastPayload(mode)
    await form(wrapper, mode).trigger('submit')
    await flushPromises()
    expect(lastPayload(mode)).toEqual(failedPayload)
    expect(wrapper.find(`#${mode}-group-form`).exists()).toBe(false)
  })

  it('closes the pending scope dialog when its parent form is closed', async () => {
    const wrapper = mountView()
    await openForm(wrapper, mode)
    await button(modelsSection(wrapper, mode), 'admin.groups.modelsList.add').trigger('click')
    const parentDialog = new DOMWrapper(form(wrapper, mode).element.closest('[role="dialog"]')!)
    await parentDialog.get('[aria-label="Close modal"]').trigger('click')
    expect(wrapper.getComponent(ModelListScopeDialog).props('show')).toBe(false)
    await openForm(wrapper, mode)
    expect(modelsSection(wrapper, mode).find('input[type="text"]').exists()).toBe(false)
  })
})

describe('GroupsView model-list platform changes', () => {
  it('clears pending operations, closes the scope dialog and submits only new-platform models', async () => {
    const wrapper = mountView()
    await openForm(wrapper, 'create')
    await addModel(wrapper, 'create', 'global')
    await button(modelRow(wrapper, 'create', 'gpt-remove'), 'admin.groups.modelsList.delete').trigger('click')
    const platformButtons = form(wrapper, 'create').get('[data-tour="group-form-platform"]').findAll('button')
    await platformButtons.find(node => node.find('platform-icon-stub[platform="gemini"]').exists())!.trigger('click')
    await flushPromises()

    expect(showWarning).toHaveBeenCalledWith('admin.groups.modelsList.platformChangeCleared')
    expect(wrapper.getComponent(ModelListScopeDialog).props('show')).toBe(false)
    expect(groups.getModelsListCandidates).toHaveBeenLastCalledWith(0, 'gemini')
    await modelsSection(wrapper, 'create').get('button').trigger('click')
    expect(modelsSection(wrapper, 'create').text()).toContain('gemini-fresh')
    expect(modelsSection(wrapper, 'create').text()).not.toContain('custom-model')
    await form(wrapper, 'create').trigger('submit')
    await flushPromises()
    expect(lastPayload('create')).toMatchObject({
      platform: 'gemini',
      models_list_config: { enabled: true, models: ['gemini-fresh'] },
      global_model_operations: [],
    })
  })
})

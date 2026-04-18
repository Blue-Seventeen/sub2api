import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, beforeEach, vi } from 'vitest'

const {
  getAutoOpsConfigMock,
  getAutoOpsLogsMock,
  getAutoOpsSamplesMock,
  getAutoOpsModelOptionsMock,
  updateAutoOpsConfigMock,
  showErrorMock,
  showSuccessMock
} = vi.hoisted(() => ({
  getAutoOpsConfigMock: vi.fn(),
  getAutoOpsLogsMock: vi.fn(),
  getAutoOpsSamplesMock: vi.fn(),
  getAutoOpsModelOptionsMock: vi.fn(),
  updateAutoOpsConfigMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAutoOpsConfig: getAutoOpsConfigMock,
      getAutoOpsLogs: getAutoOpsLogsMock,
      getAutoOpsSamples: getAutoOpsSamplesMock,
      getAutoOpsModelOptions: getAutoOpsModelOptionsMock,
      updateAutoOpsConfig: updateAutoOpsConfigMock
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock
  })
}))

vi.mock('vue-draggable-plus', () => ({
  VueDraggable: defineComponent({
    name: 'VueDraggable',
    template: '<div><slot /></div>'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (!params) return key
        return Object.entries(params).reduce(
          (text, [name, value]) => text.replace(`{${name}}`, String(value)),
          key
        )
      }
    })
  }
})

import AccountAutoOpsModal from '../AccountAutoOpsModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const SelectStub = defineComponent({
  name: 'Select',
  props: {
    modelValue: {
      type: [String, Number],
      default: null
    },
    options: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <select
      class="select-stub"
      :value="modelValue ?? ''"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option value="">placeholder</option>
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `
})

function buildConfig() {
  return {
    enabled: true,
    interval_minutes: 15,
    configured: true,
    test_models_by_platform: {
      openai: ['gpt-5.4-mini']
    },
    rules: [
      {
        id: 'rule-b',
        name: 'Token expired -> refresh',
        subject: 'test_response',
        priority: 20,
        match_type: 'contains',
        pattern: 'token_expired',
        action: 'refresh_token'
      },
      {
        id: 'rule-a',
        name: 'Hi -> recover',
        subject: 'test_response',
        priority: 10,
        match_type: 'contains',
        pattern: 'Hi',
        action: 'recover_state'
      }
    ]
  }
}

function buildLogs() {
  return {
    runs: [
      {
        id: 101,
        trigger_mode: 'manual',
        status: 'completed',
        requested_account_ids: [1, 2],
        total_accounts: 2,
        eligible_accounts: 2,
        completed_accounts: 2,
        error_message: '',
        started_at: '2026-04-18T18:00:00+08:00',
        finished_at: '2026-04-18T18:00:05+08:00',
        created_at: '2026-04-18T18:00:00+08:00',
        updated_at: '2026-04-18T18:00:05+08:00',
        steps: [
          {
            id: 1,
            run_id: 101,
            account_id: 1,
            account_name: 'strict-match-account',
            step_index: 1,
            subject: 'test_response',
            action: 'recover_state',
            status: 'action_executed',
            matched_rule_id: 'rule-a',
            matched_rule_name: 'Hi -> recover',
            response_text: '{"error_message":"this account has been deactivated"}',
            response_hash: 'hash-1',
            action_result_text: '{"ClearedError":true}',
            created_at: '2026-04-18T18:00:01+08:00'
          },
          {
            id: 2,
            run_id: 101,
            account_id: 2,
            account_name: 'token-account',
            step_index: 2,
            subject: 'test_response',
            action: 'refresh_token',
            status: 'action_executed',
            matched_rule_id: 'rule-b',
            matched_rule_name: 'Token expired -> refresh',
            response_text: '{"code":"token_expired","message":"Provided authentication token is expired."}',
            response_hash: 'hash-2',
            action_result_text: '{"refresh":"ok"}',
            created_at: '2026-04-18T18:00:02+08:00'
          },
          {
            id: 3,
            run_id: 101,
            account_id: 3,
            account_name: 'hidden-default-step',
            step_index: 3,
            subject: 'test_response',
            action: 'retest',
            status: 'no_rule_matched',
            matched_rule_id: 'default_retest',
            matched_rule_name: 'Default Retest',
            response_text: 'should stay hidden',
            response_hash: 'hash-3',
            action_result_text: '',
            created_at: '2026-04-18T18:00:03+08:00'
          },
          {
            id: 4,
            run_id: 101,
            account_id: 4,
            account_name: 'hidden-unmatched-step',
            step_index: 4,
            subject: 'test_response',
            action: 'retest',
            status: 'check_completed',
            matched_rule_id: '',
            matched_rule_name: '',
            response_text: 'should stay hidden too',
            response_hash: 'hash-4',
            action_result_text: '',
            created_at: '2026-04-18T18:00:04+08:00'
          }
        ]
      },
      {
        id: 102,
        trigger_mode: 'automatic',
        status: 'completed',
        requested_account_ids: [5],
        total_accounts: 1,
        eligible_accounts: 1,
        completed_accounts: 1,
        error_message: '',
        started_at: '2026-04-18T19:00:00+08:00',
        finished_at: '2026-04-18T19:00:03+08:00',
        created_at: '2026-04-18T19:00:00+08:00',
        updated_at: '2026-04-18T19:00:03+08:00',
        steps: []
      }
    ]
  }
}

function buildSamples() {
  return {
    samples: [
      {
        subject: 'test_response',
        response_hash: 'hash-1',
        response_text: '{"error_message":"Provided authentication token is expired."}',
        occurrences: 3,
        last_seen_at: '2026-04-18T18:30:00+08:00'
      }
    ]
  }
}

function mountModal() {
  return mount(AccountAutoOpsModal, {
    props: {
      show: false
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true
      }
    }
  })
}

async function openModal() {
  const wrapper = mountModal()
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('AccountAutoOpsModal', () => {
  beforeEach(() => {
    getAutoOpsConfigMock.mockResolvedValue(buildConfig())
    getAutoOpsLogsMock.mockResolvedValue(buildLogs())
    getAutoOpsSamplesMock.mockResolvedValue(buildSamples())
    getAutoOpsModelOptionsMock.mockResolvedValue({
      model_options: {
        openai: [{ id: 'gpt-5.4-mini', display_name: 'GPT 5.4 Mini' }]
      }
    })
    updateAutoOpsConfigMock.mockResolvedValue(buildConfig())
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
  })

  it('按优先级展示规则，且同一时间只展开一个编辑面板', async () => {
    const wrapper = await openModal()

    const rows = wrapper.findAll('[data-testid^="auto-ops-rule-row-"]')
    expect(rows).toHaveLength(2)
    expect(rows[0].attributes('data-testid')).toBe('auto-ops-rule-row-rule-a')
    expect(rows[1].attributes('data-testid')).toBe('auto-ops-rule-row-rule-b')

    await wrapper.get('[data-testid="auto-ops-edit-rule-b"]').trigger('click')
    expect(wrapper.find('[data-testid="auto-ops-edit-panel-rule-b"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auto-ops-edit-panel-rule-a"]').exists()).toBe(false)

    await wrapper.get('[data-testid="auto-ops-edit-rule-a"]').trigger('click')
    expect(wrapper.find('[data-testid="auto-ops-edit-panel-rule-a"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="auto-ops-edit-panel-rule-b"]').exists()).toBe(false)

    const priorityInput = wrapper.get('[data-testid="auto-ops-priority-rule-a"]')
    await priorityInput.setValue('30')
    await priorityInput.trigger('blur')
    await flushPromises()

    const reorderedRows = wrapper.findAll('[data-testid^="auto-ops-rule-row-"]')
    expect(reorderedRows[0].attributes('data-testid')).toBe('auto-ops-rule-row-rule-b')
    expect(reorderedRows[1].attributes('data-testid')).toBe('auto-ops-rule-row-rule-a')
  })

  it('仅展示命中规则的步骤，并对英文匹配高亮使用严格边界', async () => {
    const wrapper = await openModal()

    const visibleSteps = wrapper.findAll('[data-testid^="auto-ops-step-"]')
    expect(visibleSteps).toHaveLength(2)
    expect(wrapper.html()).not.toContain('hidden-default-step')
    expect(wrapper.html()).not.toContain('hidden-unmatched-step')

    const strictMatchStep = wrapper.get('[data-testid="auto-ops-step-1"]')
    expect(strictMatchStep.text()).toContain('strict-match-account')
    expect(strictMatchStep.html()).not.toContain('<mark')

    const tokenStep = wrapper.get('[data-testid="auto-ops-step-2"]')
    expect(tokenStep.html()).toContain('<mark')
    expect(tokenStep.html()).toContain('token_expired')

    const emptyRun = wrapper.get('[data-testid="auto-ops-run-102"]')
    expect(emptyRun.text()).toContain('admin.accounts.autoOpsDialog.noMatchedRules')
  })
})

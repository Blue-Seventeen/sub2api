import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModelsMock } = vi.hoisted(() => ({
  getAvailableModelsMock: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels: getAvailableModelsMock
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
  })
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

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: { type: [String, Number, Boolean, null], default: '' },
    options: { type: Array, default: () => [] },
    valueKey: { type: String, default: 'value' },
    labelKey: { type: String, default: 'label' }
  },
  emits: ['update:modelValue'],
  template: `
    <select
      v-bind="$attrs"
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option
        v-for="option in options"
        :key="option[valueKey]"
        :value="option[valueKey]"
      >
        {{ option[labelKey] }}
      </option>
    </select>
  `
})

const TextAreaStub = defineComponent({
  name: 'TextArea',
  props: {
    modelValue: { type: String, default: '' }
  },
  emits: ['update:modelValue'],
  template: `
    <textarea
      v-bind="$attrs"
      :value="modelValue"
      @input="$emit('update:modelValue', $event.target.value)"
    />
  `
})

function buildAccount(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: 'OpenAI OAuth',
    platform: 'openai',
    type: 'oauth',
    status: 'active',
    credentials: {},
    extra: {},
    concurrency: 1,
    priority: 1,
    proxy_id: null,
    auto_pause_on_expired: false,
    ...overrides
  } as any
}

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

describe('AccountTestModal', () => {
  const originalFetch = global.fetch

  beforeEach(() => {
    let objectURLCounter = 0
    getAvailableModelsMock.mockReset()
    getAvailableModelsMock.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: vi.fn().mockResolvedValue({ done: true, value: undefined })
        })
      }
    } as any)
    localStorage.setItem('auth_token', 'test-token')
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn(() => `blob:sub2api-audio-${++objectURLCounter}`),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true
    })
  })

  afterEach(() => {
    global.fetch = originalFetch
    localStorage.clear()
  })

  it('posts compact mode for OpenAI compact probe', async () => {
    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount()
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body)).toMatchObject({
      model_id: 'gpt-5.4',
      mode: 'compact',
      test_type: 'auto'
    })
  })

  it('posts selected explicit test type without compact mode', async () => {
    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          type: 'apikey',
          extra: { newapi_style_interface_enabled: true }
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    ;(wrapper.vm as any).testType = 'embedding'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body)).toMatchObject({
      model_id: 'gpt-5.4',
      mode: 'default',
      test_type: 'embedding'
    })
  })

  it('posts TTS voice and saves it after a successful test', async () => {
    getAvailableModelsMock.mockResolvedValue([
      { id: 'GLM-TTS', display_name: 'GLM-TTS' }
    ])
    ;(global.fetch as any).mockResolvedValueOnce(
      createStreamResponse([
        'data: {"type":"audio","audio_url":"data:audio/wav;base64,UklGRg==","mime_type":"audio/wav","data":{"bytes":4}}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    )

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'GLM-TTS'
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    ;(wrapper.vm as any).ttsVoice = 'custom-voice'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body)).toMatchObject({
      model_id: 'GLM-TTS',
      prompt: 'hello',
      mode: 'default',
      test_type: 'tts',
      test_options: { voice: 'custom-voice' }
    })
    const audio = wrapper.find('audio')
    expect(audio.exists()).toBe(true)
    expect(audio.attributes('src')).toBe('blob:sub2api-audio-1')
    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob))
    const tokenHash = Math.abs('test-token'.split('').reduce((hash, char) => ((hash << 5) - hash + char.charCodeAt(0)) | 0, 0)).toString(36)
    expect(localStorage.getItem(`sub2api:account-test:tts-voices:${tokenHash}:zhipu`)).toBe(JSON.stringify(['custom-voice']))
  })

  it('shows every explicit test type for any account', async () => {
    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()

    expect((wrapper.vm as any).testTypeOptions.map((option: { value: string }) => option.value)).toEqual([
      'auto',
      'text',
      'image',
      'asr',
      'tts',
      'video',
      'task',
      'embedding',
      'rerank'
    ])
  })

  it('posts selected model_id for ASR even when selected model is not an audio model', async () => {
    getAvailableModelsMock.mockResolvedValue([
      { id: 'glm-4.5', display_name: 'GLM 4.5' }
    ])

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'glm-4.5'
    ;(wrapper.vm as any).testType = 'asr'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body).model_id).toBe('glm-4.5')
  })

  it('posts selected model_id for TTS even when selected model is not an audio model', async () => {
    getAvailableModelsMock.mockResolvedValue([
      { id: 'glm-4.5', display_name: 'GLM 4.5' }
    ])

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'glm-4.5'
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, options] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(options.body)).toMatchObject({
      model_id: 'glm-4.5',
      test_type: 'tts'
    })
  })

  it('does not start ASR probes when no model is selected', async () => {
    getAvailableModelsMock.mockResolvedValue([])

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    expect((wrapper.vm as any).selectedModelId).toBe('')
    ;(wrapper.vm as any).testType = 'asr'
    expect((wrapper.vm as any).canStartTest).toBe(false)
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('does not start TTS probes when no model is selected', async () => {
    getAvailableModelsMock.mockResolvedValue([])

    const wrapper = mount(AccountTestModal, {
      props: {
        show: true,
        account: buildAccount({
          platform: 'zhipu',
          type: 'apikey'
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: SelectStub,
          TextArea: TextAreaStub,
          Icon: true
        }
      }
    })

    await flushPromises()
    expect((wrapper.vm as any).selectedModelId).toBe('')
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    expect((wrapper.vm as any).canStartTest).toBe(false)
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })
})

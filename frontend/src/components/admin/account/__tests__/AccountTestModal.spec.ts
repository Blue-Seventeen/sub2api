import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        return messages[key] || key
      }
    })
  }
})

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

function mountModal(show = false, accountOverrides: Record<string, unknown> = {}) {
  return mount(AccountTestModal, {
    props: {
      show,
      account: {
        id: 42,
        name: 'Gemini Image Test',
        platform: 'gemini',
        type: 'apikey',
        status: 'active',
        ...accountOverrides
      }
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    let objectURLCounter = 0
    getAvailableModels.mockResolvedValue([
      { id: 'gemini-2.0-flash', display_name: 'Gemini 2.0 Flash' },
      { id: 'gemini-2.5-flash-image', display_name: 'Gemini 2.5 Flash Image' },
      { id: 'gemini-3.1-flash-image', display_name: 'Gemini 3.1 Flash Image' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn(() => `blob:sub2api-audio-${++objectURLCounter}`),
      configurable: true
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gemini-2.5-flash-image"}\n',
        'data: {"type":"image","image_url":"data:image/png;base64,QUJD","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends prompt and renders image preview for Gemini image model tests', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const promptInput = wrapper.find('textarea.textarea-stub')
    expect(promptInput.exists()).toBe(true)
    await promptInput.setValue('draw a tiny orange cat astronaut')

    const buttons = wrapper.findAll('button')
    const startButton = buttons.find((button) => button.text().includes('admin.accounts.startTest'))
    expect(startButton).toBeTruthy()

    await startButton!.trigger('click')
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gemini-3.1-flash-image',
      prompt: 'draw a tiny orange cat astronaut',
      mode: 'default',
      test_type: 'auto'
    })

    const preview = wrapper.find('img[alt="test-image-1"]')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('src')).toBe('data:image/png;base64,QUJD')
  })

  it('normalizes displayName model fields', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'claude-sonnet-4-5', displayName: 'Claude Sonnet 4.5', type: 'model' }
    ])

    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getAvailableModels).toHaveBeenCalled()
    expect((wrapper.vm as any).availableModels).toEqual([
      {
        id: 'claude-sonnet-4-5',
        display_name: 'Claude Sonnet 4.5',
        type: 'model',
        created_at: ''
      }
    ])
    expect((wrapper.vm as any).selectedModelId).toBe('claude-sonnet-4-5')
  })

  it('loads models when initially shown', async () => {
    const wrapper = mountModal(true)
    await flushPromises()

    expect(getAvailableModels).toHaveBeenCalledTimes(1)
    expect((wrapper.vm as any).selectedModelId).toBe('gemini-3.1-flash-image')
  })

  it('posts explicit test_type without compact mode', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', displayName: 'GPT 5.4', type: 'model' }
    ])

    const wrapper = mountModal(true, {
      name: 'OpenAI New API',
      platform: 'openai',
      type: 'apikey',
      extra: { newapi_style_interface_enabled: true }
    })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    ;(wrapper.vm as any).testType = 'embedding'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'default',
      test_type: 'embedding'
    })
  })

  it('shows every explicit test type for any account', async () => {
    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
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

  it('posts TTS voice and saves it after a successful test', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'GLM-TTS', displayName: 'GLM-TTS', type: 'model' }
    ])
    ;(global.fetch as any).mockResolvedValueOnce(
      createStreamResponse([
        'data: {"type":"audio","audio_url":"data:audio/wav;base64,UklGRg==","mime_type":"audio/wav","data":{"bytes":4}}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    )

    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
    })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'GLM-TTS'
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    ;(wrapper.vm as any).ttsVoice = 'custom-voice'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
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
    expect(localStorage.setItem).toHaveBeenCalledWith(
      `sub2api:account-test:tts-voices:${Math.abs('test-token'.split('').reduce((hash, char) => ((hash << 5) - hash + char.charCodeAt(0)) | 0, 0)).toString(36)}:zhipu`,
      JSON.stringify(['custom-voice'])
    )
  })

  it('posts selected model_id for ASR even when selected model is not an audio model', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'glm-4.5', displayName: 'GLM 4.5', type: 'model' }
    ])

    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
    })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'glm-4.5'
    ;(wrapper.vm as any).testType = 'asr'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body).model_id).toBe('glm-4.5')
  })

  it('posts selected model_id for TTS even when selected model is not an audio model', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'glm-4.5', displayName: 'GLM 4.5', type: 'model' }
    ])

    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
    })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'glm-4.5'
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'glm-4.5',
      test_type: 'tts'
    })
  })

  it('does not start ASR probes when no model is selected', async () => {
    getAvailableModels.mockResolvedValue([])

    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
    })
    await flushPromises()

    expect((wrapper.vm as any).selectedModelId).toBe('')
    ;(wrapper.vm as any).testType = 'asr'
    expect((wrapper.vm as any).canStartTest).toBe(false)
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('does not start TTS probes when no model is selected', async () => {
    getAvailableModels.mockResolvedValue([])

    const wrapper = mountModal(true, {
      name: 'GLM Audio',
      platform: 'zhipu',
      type: 'apikey'
    })
    await flushPromises()

    expect((wrapper.vm as any).selectedModelId).toBe('')
    ;(wrapper.vm as any).testType = 'tts'
    ;(wrapper.vm as any).testPrompt = 'hello'
    expect((wrapper.vm as any).canStartTest).toBe(false)
    await (wrapper.vm as any).startTest()
    await flushPromises()
    await flushPromises()

    expect(global.fetch).not.toHaveBeenCalled()
  })
})

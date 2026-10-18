import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import TLSFingerprintProfilesModal from '../TLSFingerprintProfilesModal.vue'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({ adminAPI: { tlsFingerprintProfiles: mocks } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())
beforeEach(() => vi.resetAllMocks())

function profile() {
  return {
    id: 1,
    name: 'Claude Code TLS Profile',
    description: null,
    enable_grease: false,
    cipher_suites: [],
    curves: [],
    point_formats: [],
    signature_algorithms: [],
    alpn_protocols: [],
    supported_versions: [],
    key_share_groups: [],
    psk_modes: [],
    extensions: [],
    created_at: '2026-10-18T00:00:00Z',
    updated_at: '2026-10-18T00:00:00Z'
  }
}

function mountModal(show = true) {
  return mount(TLSFingerprintProfilesModal, {
    props: { show },
    global: {
      stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Icon: true
      }
    }
  })
}

describe('TLS fingerprint profiles modal', () => {
  it('loads existing profiles when first mounted open', async () => {
    mocks.list.mockResolvedValue([profile()])

    const wrapper = mountModal(true)
    await flushPromises()

    expect(mocks.list).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Claude Code TLS Profile')
  })

  it('shows the API error message when create fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.list.mockResolvedValue([])
    mocks.create.mockRejectedValue({ status: 409, message: 'A profile with this name already exists' })

    const wrapper = mountModal(true)
    await flushPromises()
    await wrapper.get('button.btn-primary').trigger('click')
    await wrapper.get('input[required]').setValue('Claude Code TLS Profile')
    await wrapper.findAll('button.btn-primary').at(-1)!.trigger('click')
    await flushPromises()

    expect(mocks.showError).toHaveBeenCalledWith('A profile with this name already exists')
  })
})

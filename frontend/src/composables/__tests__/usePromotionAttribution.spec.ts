import {
  clearStoredPromotionRef,
  getStoredPromotionRef,
  storePromotionRef
} from '@/composables/usePromotionAttribution'

describe('usePromotionAttribution', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('stores uppercase ref code', () => {
    storePromotionRef('abc123')
    expect(getStoredPromotionRef()).toBe('ABC123')
  })

  it('clears stored ref code', () => {
    storePromotionRef('abc123')
    clearStoredPromotionRef()
    expect(getStoredPromotionRef()).toBe('')
  })
})

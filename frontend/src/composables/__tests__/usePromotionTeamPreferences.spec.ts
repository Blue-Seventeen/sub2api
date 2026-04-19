import {
  getPromotionTeamPageSize,
  getPromotionTeamSortBy,
  getPromotionTeamSortOrder,
  setPromotionTeamPageSize,
  setPromotionTeamSortBy,
  setPromotionTeamSortOrder
} from '@/composables/usePromotionTeamPreferences'

describe('usePromotionTeamPreferences', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses expected defaults', () => {
    expect(getPromotionTeamPageSize()).toBe(10)
    expect(getPromotionTeamSortBy()).toBe('today_contribution')
    expect(getPromotionTeamSortOrder()).toBe('desc')
  })

  it('persists page size and sorting in localStorage', () => {
    setPromotionTeamPageSize(20)
    setPromotionTeamSortBy('joined_at')
    setPromotionTeamSortOrder('asc')

    expect(getPromotionTeamPageSize()).toBe(20)
    expect(getPromotionTeamSortBy()).toBe('joined_at')
    expect(getPromotionTeamSortOrder()).toBe('asc')
  })
})

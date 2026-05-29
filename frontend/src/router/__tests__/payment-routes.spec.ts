import { describe, expect, it } from 'vitest'

describe('payment routes', () => {
  it('registers Airwallex checkout route used by PaymentView', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'AirwallexPayment')

    expect(route?.path).toBe('/payment/airwallex')
    expect(route?.meta.requiresAuth).toBe(false)
    expect(route?.meta.requiresPayment).toBe(false)
  })
})

import { describe, expect, it } from 'vitest'
import { isolatePopupOpener, PROVIDER_CONFIG_FIELDS } from '@/components/payment/providerConfig'

function findField(key: string) {
  const fields = PROVIDER_CONFIG_FIELDS.wxpay || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('publicKeyId')?.optional).toBeFalsy()
    expect(findField('certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('mpAppId')).toBeUndefined()
    expect(findField('h5AppName')).toBeUndefined()
    expect(findField('h5AppUrl')).toBeUndefined()
  })
})

describe('isolatePopupOpener', () => {
  it('clears opener on ordinary payment popups', () => {
    const win = { opener: { location: 'about:blank' } } as unknown as Window

    isolatePopupOpener(win)

    expect(win.opener).toBeNull()
  })

  it('does not throw if the browser blocks opener assignment', () => {
    const win = {}
    Object.defineProperty(win, 'opener', {
      set() {
        throw new Error('blocked')
      },
    })

    expect(() => isolatePopupOpener(win as Window)).not.toThrow()
  })
})

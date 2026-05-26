declare module '@airwallex/components-sdk' {
  export interface AirwallexInitOptions {
    env: 'demo' | 'prod'
    enabledElements?: string[]
    locale?: string
  }

  export interface AirwallexCheckoutOptions {
    intent_id: string
    client_secret: string
    currency: string
    country_code: string
    successUrl: string
  }

  export interface AirwallexPayments {
    redirectToCheckout(options: AirwallexCheckoutOptions): string | void | Promise<string | void>
  }

  export function init(options: AirwallexInitOptions): Promise<{
    payments?: AirwallexPayments
  }>
}

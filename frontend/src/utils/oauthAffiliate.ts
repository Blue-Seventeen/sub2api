// Compatibility shim for upstream affiliate referral hooks.
// This fork keeps the existing Promotion flow and intentionally disables
// upstream Affiliate payload propagation.

export function resolveAffiliateReferralCode(..._values: unknown[]): string {
  return ''
}

export function storeOAuthAffiliateCode(_code: string | null | undefined): void {
  clearAllAffiliateReferralCodes()
}

export function loadOAuthAffiliateCode(): string {
  return ''
}

export function loadAffiliateReferralCode(): string {
  return ''
}

export function clearAffiliateReferralCode(): void {
  clearAllAffiliateReferralCodes()
}

export function clearAllAffiliateReferralCodes(): void {
  try {
    window.localStorage.removeItem('sub2api_affiliate_referral_code')
    window.sessionStorage.removeItem('sub2api_oauth_affiliate_referral_code')
  } catch {
    // Storage can be unavailable in SSR-like tests or restricted browsers.
  }
}

export function oauthAffiliatePayload(_code: string | null | undefined): Record<string, never> {
  return {}
}

export const BILLING_MODE_TOKEN = 'token'
export const BILLING_MODE_PER_REQUEST = 'per_request'
export const BILLING_MODE_IMAGE = 'image'
export const BILLING_MODE_DURATION = 'duration'
export const BILLING_MODE_CHARACTER = 'character'

export function getBillingModeLabel(mode: string | null | undefined, t: (key: string) => string): string {
  switch (mode) {
    case BILLING_MODE_PER_REQUEST: return t('admin.usage.billingModePerRequest')
    case BILLING_MODE_IMAGE: return t('admin.usage.billingModeImage')
    case BILLING_MODE_DURATION: return t('admin.usage.billingModeDuration')
    case BILLING_MODE_CHARACTER: return t('admin.usage.billingModeCharacter')
    default: return t('admin.usage.billingModeToken')
  }
}

export function getBillingModeBadgeClass(mode: string | null | undefined): string {
  switch (mode) {
    case BILLING_MODE_PER_REQUEST: return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
    case BILLING_MODE_IMAGE: return 'bg-pink-100 text-pink-700 dark:bg-pink-900/30 dark:text-pink-300'
    case BILLING_MODE_DURATION: return 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300'
    case BILLING_MODE_CHARACTER: return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    default: return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  }
}

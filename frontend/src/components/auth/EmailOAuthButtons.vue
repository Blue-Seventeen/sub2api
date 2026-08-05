<template>
  <div v-if="hasProviders" class="space-y-4">
    <div :class="providerGridClass">
      <button
        v-for="provider in visibleProviders"
        :key="provider"
        type="button"
        :disabled="disabled"
        class="btn btn-secondary h-12 w-full justify-center gap-2"
        @click="startLogin(provider)"
      >
        <GitHubMark v-if="provider === 'github'" class="h-5 w-5 text-gray-800 dark:text-gray-100" />
        <GoogleMark v-else class="h-5 w-5" />
        <span class="font-medium">{{ providerLabel(provider) }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import GitHubMark from './GitHubMark.vue'
import GoogleMark from './GoogleMark.vue'
import type { OAuthLoginStart } from '@/api/auth'
import { resolveAffiliateReferralCode, storeOAuthAffiliateCode } from '@/utils/oauthAffiliate'

type EmailOAuthProvider = 'github' | 'google'
const EMAIL_OAUTH_PENDING_PROVIDER_KEY = 'email_oauth_pending_provider'

const props = withDefaults(defineProps<{
  disabled?: boolean
  githubEnabled?: boolean
  googleEnabled?: boolean
  affCode?: string
  showDivider?: boolean
}>(), {
  showDivider: true
})
const emit = defineEmits<{
  start: [request: OAuthLoginStart]
}>()

const route = useRoute()
const { t } = useI18n()

const visibleProviders = computed<EmailOAuthProvider[]>(() => {
  const providers: EmailOAuthProvider[] = []
  if (props.githubEnabled) providers.push('github')
  if (props.googleEnabled) providers.push('google')
  return providers
})

const hasProviders = computed(() => visibleProviders.value.length > 0)
const providerGridClass = computed(() => [
  'grid',
  'grid-cols-1',
  'gap-3',
  visibleProviders.value.length > 1 ? 'sm:grid-cols-2' : ''
])

function providerLabel(provider: EmailOAuthProvider): string {
  const name = provider === 'github' ? 'GitHub' : 'Google'
  if (visibleProviders.value.length > 1) return name
  return t('auth.emailOAuth.signIn', { providerName: name }, `Continue with ${name}`)
}

function startLogin(provider: EmailOAuthProvider): void {
  const redirectTo = (route.query.redirect as string) || '/dashboard'
  window.sessionStorage.setItem(EMAIL_OAUTH_PENDING_PROVIDER_KEY, provider)
  const affiliateCode = resolveAffiliateReferralCode(props.affCode, route.query.aff, route.query.aff_code)
  storeOAuthAffiliateCode(affiliateCode)
  const params: Record<string, string> = { redirect: redirectTo }
  if (affiliateCode) {
    params.aff_code = affiliateCode
  }
  emit('start', { provider, params })
}
</script>

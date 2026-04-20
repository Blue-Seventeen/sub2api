<template>
  <div class="space-y-6">
    <section class="grid gap-6 lg:grid-cols-[1.04fr_0.96fr] lg:items-stretch">
      <div class="promo-panel self-start lg:flex lg:h-full lg:flex-col">
        <div class="flex items-center justify-between gap-3 lg:min-h-[72px]">
          <div>
            <h3 class="promo-section-title">
              <Icon name="key" size="sm" class="text-cyan-300" />
              我的推广码
            </h3>
            <p class="promo-section-note">每个用户的推广码唯一，用户带 ref 注册并绑定后才会建立推广关系。</p>
          </div>
          <span class="promo-chip">唯一码</span>
        </div>

        <div class="mt-5 rounded-[24px] border border-cyan-400/20 bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 px-6 py-8 text-center shadow-[0_0_32px_-22px_rgba(34,211,238,0.55)]">
          <div class="text-xs uppercase tracking-[0.34em] text-slate-500">Invite Code</div>
          <div class="mt-3 text-3xl font-semibold tracking-[0.3em] text-cyan-300">{{ overview?.invite_code || '--' }}</div>
          <div class="mt-3 text-sm text-slate-400">好友注册后自动绑定，后续按真实消费金额计算返佣。</div>
        </div>

        <div class="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          <button type="button" class="promo-btn promo-btn-secondary" @click="copyText(overview?.invite_code || '', '推广码')">
            <Icon name="copy" size="sm" />
            复制推广码
          </button>
          <button type="button" class="promo-btn promo-btn-secondary" @click="copyText(resolvedInviteLink, '推广链接')">
            <Icon name="link" size="sm" />
            复制邀请链接
          </button>
          <button type="button" class="promo-btn promo-btn-primary" :disabled="downloading || !overview?.invite_code" @click="downloadPoster">
            <Icon :name="downloading ? 'refresh' : 'download'" size="sm" :class="downloading ? 'animate-spin' : ''" />
            {{ downloading ? '正在生成海报' : '下载海报 PNG' }}
          </button>
        </div>

        <div class="mt-5 rounded-2xl border border-white/10 bg-white/5 p-4">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">推广链接</div>
          <div class="mt-2 break-all text-sm leading-7 text-slate-200">{{ resolvedInviteLink || '-' }}</div>
        </div>

        <div class="mt-5 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl border border-emerald-400/20 bg-emerald-500/10 p-4">
            <div class="text-xs uppercase tracking-[0.24em] text-emerald-300/80">激活奖励</div>
            <div class="mt-2 text-2xl font-semibold text-emerald-300">${{ money(overview?.activation_bonus_amount) }}</div>
            <div class="mt-2 text-xs text-slate-400">被邀请用户累计真实消费严格大于 ${{ money(overview?.activation_threshold_amount) }} 时触发。</div>
          </div>
          <div class="rounded-2xl border border-cyan-400/20 bg-cyan-500/10 p-4">
            <div class="text-xs uppercase tracking-[0.24em] text-cyan-300/80">一级返利</div>
            <div class="mt-2 text-2xl font-semibold text-cyan-300">{{ rate(overview?.current_direct_rate) }}%</div>
            <div class="mt-2 text-xs text-slate-400">按一级下级当日真实消费聚合，次日统一结算。</div>
          </div>
          <div class="rounded-2xl border border-purple-400/20 bg-purple-500/10 p-4">
            <div class="text-xs uppercase tracking-[0.24em] text-purple-300/80">二级返利</div>
            <div class="mt-2 text-2xl font-semibold text-purple-300">{{ rate(overview?.current_indirect_rate) }}%</div>
            <div class="mt-2 text-xs text-slate-400">按二级链路用户真实消费聚合，次日统一发放。</div>
          </div>
        </div>
      </div>

      <div class="promo-panel self-start lg:flex lg:h-full lg:flex-col">
        <div class="flex items-center justify-between gap-3 lg:min-h-[72px]">
          <div>
            <h3 class="promo-section-title">
              <Icon name="document" size="sm" class="text-cyan-300" />
              海报预览
            </h3>
            <p class="promo-section-note">下载时会基于当前海报 HTML 直接截图导出 PNG，并自动放大 20%。</p>
          </div>
          <span class="promo-chip">html2canvas</span>
        </div>

        <div class="mt-5 flex justify-center lg:flex-1 lg:items-center">
          <div :style="posterPreviewFrameStyle">
            <div :style="posterPreviewScaledPosterStyle">
              <div :id="posterElementId" ref="posterRef" xmlns="http://www.w3.org/1999/xhtml" :style="posterStyle">
                <div :style="posterGlowStyle"></div>
                <div :style="posterGlowAltStyle"></div>
                <div :style="posterHeaderStyle">
                  <div v-if="posterConfig.logo_url" :style="posterLogoImageWrapStyle">
                    <img :src="posterConfig.logo_url" alt="poster logo" :style="posterLogoImageStyle" />
                  </div>
                  <div v-else :style="posterLogoStyle">{{ posterLogoText }}</div>
                  <div>
                    <div :style="posterEyebrowStyle">PROMOTION CENTER</div>
                    <div data-poster-role="title" :style="posterTitleStyle">{{ posterConfig.title }}</div>
                  </div>
                </div>
                <div :style="posterHeadlineStyle">{{ posterConfig.headline }}</div>
                <div :style="posterSubtitleStyle">{{ posterConfig.description }}</div>
                <div :style="posterMetaRowStyle">
                  <span v-for="tag in posterConfig.tags" :key="tag" data-poster-role="chip" :style="posterMetaChipStyle">{{ tag }}</span>
                </div>
                <div :style="posterQrWrapStyle">
                  <img v-if="qrCodeDataUrl" :src="qrCodeDataUrl" alt="promotion qrcode" :style="posterQrStyle" />
                </div>
                <div :style="posterHintStyle">{{ posterConfig.scan_hint }}</div>
                <div :style="posterCodeStyle">{{ posterInviteCode }}</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h3 class="promo-section-title">
            <Icon name="chatBubble" size="sm" class="text-cyan-300" />
            推广话术
          </h3>
          <p class="promo-section-note">话术支持后台维护与占位符渲染，复制后会自动累计使用次数。</p>
        </div>
        <button type="button" class="promo-btn promo-btn-secondary" :disabled="loadingScripts" @click="fetchScripts">
          <Icon name="refresh" size="sm" :class="loadingScripts ? 'animate-spin' : ''" />
          刷新话术
        </button>
      </div>

      <div v-if="loadingScripts" class="promo-empty-state">推广话术加载中...</div>
      <div v-else-if="!scripts.length" class="promo-empty-state">暂未配置推广话术</div>
      <div v-else class="grid gap-4 xl:grid-cols-2">
        <article
          v-for="item in scripts"
          :key="item.id"
          class="rounded-2xl border border-white/10 bg-slate-950/40 p-5 transition-all duration-200 hover:border-cyan-400/20 hover:bg-cyan-500/5"
        >
          <div class="flex items-start justify-between gap-4">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded-full border px-3 py-1 text-xs font-medium" :class="tagColorClass(item.category)">{{ displayTag(item.category) }}</span>
                <span class="text-lg font-medium text-white">{{ item.name }}</span>
              </div>
              <div class="mt-2 text-xs text-slate-500">已使用 {{ item.use_count }} 次</div>
            </div>
            <button type="button" class="promo-btn promo-btn-soft px-3 py-2 text-xs" @click="copyScript(item)">
              <Icon name="copy" size="sm" />
              复制
            </button>
          </div>
          <div class="mt-4 rounded-2xl border border-white/10 bg-white/5 p-4 text-sm leading-7 whitespace-pre-wrap text-slate-200">
            {{ normalizeScriptPreview(item.rendered_preview || item.content) }}
          </div>
        </article>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h3 class="promo-section-title">
            <Icon name="calculator" size="sm" class="text-cyan-300" />
            佣金规则
          </h3>
          <p class="promo-section-note">以下文案由后台“等级与返利配置”实时渲染，可直接反映当前等级返佣比例。</p>
        </div>
        <span class="promo-chip">当前总提成 {{ rate(overview?.current_total_rate) }}%</span>
      </div>

      <div class="space-y-4">
        <article class="flex items-start gap-4 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-emerald-500/20">
            <Icon name="userPlus" size="sm" class="text-emerald-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-emerald-400">激活奖励</div>
            <p class="text-sm leading-7 text-slate-400">
              {{ overview?.rule_templates?.activation || '暂未配置规则模板' }}
            </p>
          </div>
        </article>

        <article class="flex items-start gap-4 rounded-xl border border-cyan-500/20 bg-cyan-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-cyan-500/20">
            <Icon name="chartBar" size="sm" class="text-cyan-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-cyan-400">一级返利</div>
            <p class="text-sm leading-7 text-slate-400">
              {{ overview?.rule_templates?.direct || '暂未配置规则模板' }}
            </p>
          </div>
        </article>

        <article class="flex items-start gap-4 rounded-xl border border-purple-500/20 bg-purple-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-purple-500/20">
            <Icon name="users" size="sm" class="text-purple-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-purple-400">二级返利</div>
            <p class="text-sm leading-7 text-slate-400">
              {{ overview?.rule_templates?.indirect || '暂未配置规则模板' }}
            </p>
          </div>
        </article>
      </div>

    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { saveAs } from 'file-saver'
import QRCode from 'qrcode'
import Icon from '@/components/icons/Icon.vue'
import { promotionAPI, type PromotionOverview, type PromotionScript } from '@/api/promotion'
import { useAppStore } from '@/stores'
import { renderElementToPngBlobById } from '@/utils/renderHtmlToPng'

const props = defineProps<{
  overview: PromotionOverview | null
}>()

const appStore = useAppStore()
const siteName = computed(() => appStore.siteName || 'Sub2API')
const scripts = ref<PromotionScript[]>([])
const loadingScripts = ref(false)
const qrCodeDataUrl = ref('')
const downloading = ref(false)
const posterRef = ref<HTMLElement | null>(null)
const posterElementId = 'promotion-user-poster'
const posterPreviewScale = 0.715
const posterWidth = 420
const posterHeight = 620
const posterDownloadScale = 1.2
const posterExportPixelRatio = 2

const legacyTagMap: Record<string, string> = {
  default: '默认',
  wechat: '朋友圈',
  tech: '技术群',
  social: '社交平台',
  email: '邮件'
}

const tagColorPalette = [
  'border-purple-400/20 bg-purple-500/10 text-purple-300',
  'border-cyan-400/20 bg-cyan-500/10 text-cyan-300',
  'border-emerald-400/20 bg-emerald-500/10 text-emerald-300',
  'border-amber-400/20 bg-amber-500/10 text-amber-300',
  'border-pink-400/20 bg-pink-500/10 text-pink-300',
  'border-indigo-400/20 bg-indigo-500/10 text-indigo-300',
  'border-sky-400/20 bg-sky-500/10 text-sky-300',
  'border-rose-400/20 bg-rose-500/10 text-rose-300'
]

const posterConfig = computed(() => {
  const config = props.overview?.poster_config
  return {
    invite_base_url: config?.invite_base_url || '',
    logo_url: config?.logo_url || '',
    title: config?.title || siteName.value,
    headline: config?.headline || '邀请好友，一起把消费返佣赚回来',
    description: config?.description || '一级返利 + 二级返利 + 激活奖励，统一结算到真实余额。',
    scan_hint: config?.scan_hint || '扫码快速注册',
    tags: config?.tags?.length ? config.tags.slice(0, 6) : ['真实消费返佣', '次日结算', '唯一推广码'],
    primary_invite_code: config?.primary_invite_code || props.overview?.invite_code || ''
  }
})

const posterLogoText = computed(() => {
  const raw = (posterConfig.value.title || siteName.value || 'SU').replace(/\s+/g, '')
  return raw.slice(0, 2).toUpperCase()
})

const posterInviteCode = computed(() => posterConfig.value.primary_invite_code || props.overview?.invite_code || '--')

const rawInviteLink = computed(() => props.overview?.invite_link?.trim() || '')

const resolvedInviteLink = computed(() => {
  const raw = rawInviteLink.value
  if (!raw) return ''
  if (/^https?:\/\//i.test(raw)) {
    return raw
  }
  if (typeof window !== 'undefined') {
    return new URL(raw, window.location.origin).toString()
  }
  return raw
})

watch(
  () => resolvedInviteLink.value,
  async (link) => {
    if (!link) {
      qrCodeDataUrl.value = ''
      return
    }
    qrCodeDataUrl.value = await QRCode.toDataURL(link, { width: 320, margin: 1 })
  },
  { immediate: true }
)

onMounted(() => {
  void fetchScripts()
})

async function fetchScripts() {
  loadingScripts.value = true
  try {
    scripts.value = await promotionAPI.getScripts()
  } catch (error) {
    console.error('Failed to fetch promotion scripts:', error)
    appStore.showError('加载推广话术失败')
  } finally {
    loadingScripts.value = false
  }
}

async function copyText(text: string, label: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    appStore.showSuccess(`${label}已复制`)
  } catch {
    appStore.showError(`复制${label}失败`)
  }
}

async function copyScript(script: PromotionScript) {
  const text = normalizeScriptPreview(script.rendered_preview || script.content)
  try {
    await navigator.clipboard.writeText(text)
    await promotionAPI.markScriptUsed(script.id)
    script.use_count += 1
    appStore.showSuccess('推广话术已复制')
  } catch (error) {
    console.error('Failed to copy promotion script:', error)
    appStore.showError('复制推广话术失败')
  }
}

async function downloadPoster() {
  if (!posterRef.value || !props.overview?.invite_code) return
  downloading.value = true
  try {
    const blob = await renderElementToPngBlobById({
      elementId: posterElementId,
      width: posterWidth,
      height: posterHeight,
      scaleMultiplier: posterDownloadScale,
      pixelRatio: posterExportPixelRatio,
      backgroundColor: null
    })
    if (!blob) {
      throw new Error('Poster export failed')
    }
    saveAs(blob, `promotion-poster-${props.overview.invite_code}.png`)
  } catch (error) {
    console.error('Failed to export promotion poster:', error)
    appStore.showError('下载海报失败')
  } finally {
    downloading.value = false
  }
}

function displayTag(value?: string) {
  const normalized = String(value || '').trim()
  if (!normalized) return '默认'
  return legacyTagMap[normalized] || normalized
}

function tagColorClass(value?: string) {
  const label = displayTag(value)
  let hash = 0
  for (const char of label) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0
  }
  return tagColorPalette[hash % tagColorPalette.length]
}

function normalizeScriptPreview(text?: string) {
  const source = String(text || '')
  if (!source) return source
  if (!rawInviteLink.value || !resolvedInviteLink.value || rawInviteLink.value === resolvedInviteLink.value) {
    return source
  }
  return source.split(rawInviteLink.value).join(resolvedInviteLink.value)
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function rate(value?: number) {
  const formatted = Number(value || 0).toFixed(4)
  return formatted.replace(/\.?0+$/, '')
}

const posterStyle = {
  position: 'relative',
  overflow: 'hidden',
  width: `${posterWidth}px`,
  height: `${posterHeight}px`,
  borderRadius: '28px',
  background: 'linear-gradient(145deg, #020617 0%, #0f172a 42%, #1e293b 100%)',
  border: '1.5px solid rgba(34,211,238,0.25)',
  padding: '28px',
  color: '#ffffff',
  boxSizing: 'border-box',
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'stretch',
  gap: '14px'
} as const

const posterPreviewFrameStyle = {
  width: '338px',
  height: '494px',
  padding: '20px',
  background: 'linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%)',
  borderRadius: '22px',
  boxShadow: '0 22px 66px -16.5px rgba(0, 0, 0, 0.6), inset 0 1.1px 0 rgba(255, 255, 255, 0.1)',
  boxSizing: 'border-box',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  overflow: 'hidden'
} as const

const posterPreviewScaledPosterStyle = {
  width: `${posterWidth}px`,
  height: `${posterHeight}px`,
  transform: `scale(${posterPreviewScale})`,
  transformOrigin: 'center center',
  borderRadius: '28px',
  overflow: 'hidden',
  boxShadow: '0 27.5px 88px -22px rgba(0, 0, 0, 0.8)',
  flexShrink: '0'
} as const

const posterGlowStyle = {
  position: 'absolute',
  width: '240px',
  height: '240px',
  right: '-80px',
  top: '-80px',
  borderRadius: '999px',
  background: 'radial-gradient(circle, rgba(34,211,238,0.28) 0%, rgba(34,211,238,0) 65%)',
  pointerEvents: 'none'
} as const

const posterGlowAltStyle = {
  position: 'absolute',
  width: '240px',
  height: '240px',
  left: '-90px',
  bottom: '-90px',
  borderRadius: '999px',
  background: 'radial-gradient(circle, rgba(129,140,248,0.28) 0%, rgba(129,140,248,0) 65%)',
  pointerEvents: 'none'
} as const

const posterHeaderStyle = {
  position: 'relative',
  zIndex: '1',
  display: 'flex',
  alignItems: 'center',
  gap: '14px'
} as const

const posterLogoStyle = {
  width: '56px',
  height: '56px',
  borderRadius: '18px',
  background: 'linear-gradient(135deg, #22d3ee 0%, #6366f1 100%)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  fontSize: '20px',
  fontWeight: '700',
  letterSpacing: '0.08em',
  boxShadow: '0 8px 20px -4px rgba(34, 211, 238, 0.4)'
} as const

const posterLogoImageWrapStyle = {
  width: '56px',
  height: '56px',
  borderRadius: '18px',
  overflow: 'hidden',
  boxShadow: '0 8px 20px -4px rgba(34, 211, 238, 0.4)'
} as const

const posterLogoImageStyle = {
  width: '100%',
  height: '100%',
  display: 'block',
  objectFit: 'cover'
} as const

const posterEyebrowStyle = {
  fontSize: '11px',
  letterSpacing: '0.28em',
  color: '#94a3b8',
  fontWeight: '500'
} as const

const posterTitleStyle = {
  marginTop: '4px',
  fontSize: '24px',
  fontWeight: '700',
  background: 'linear-gradient(90deg, #ffffff 0%, #67e8f9 100%)',
  WebkitBackgroundClip: 'text',
  WebkitTextFillColor: 'transparent'
} as const

const posterHeadlineStyle = {
  position: 'relative',
  zIndex: '1',
  marginTop: '6px',
  fontSize: '30px',
  lineHeight: '1.3',
  fontWeight: '700',
  letterSpacing: '-0.02em'
} as const

const posterSubtitleStyle = {
  position: 'relative',
  zIndex: '1',
  fontSize: '14px',
  lineHeight: '1.7',
  color: '#bac6d8',
  fontWeight: '400'
} as const

const posterMetaRowStyle = {
  position: 'relative',
  zIndex: '1',
  display: 'flex',
  flexWrap: 'wrap',
  gap: '8px',
  marginTop: '-6px'
} as const

const posterMetaChipStyle = {
  display: 'inline-flex',
  alignItems: 'center',
  justifyContent: 'center',
  height: '28px',
  padding: '6px 12px',
  boxSizing: 'border-box',
  borderRadius: '999px',
  border: '1px solid rgba(34,211,238,0.25)',
  background: 'rgba(34,211,238,0.08)',
  fontSize: '12px',
  lineHeight: '1',
  whiteSpace: 'nowrap',
  color: '#67e8f9',
  fontWeight: '500'
} as const

const posterQrWrapStyle = {
  position: 'relative',
  zIndex: '1',
  marginTop: '8px',
  alignSelf: 'center',
  borderRadius: '24px',
  background: '#ffffff',
  padding: '18px'
} as const

const posterQrStyle = {
  width: '220px',
  height: '220px',
  display: 'block'
} as const

const posterHintStyle = {
  position: 'relative',
  zIndex: '1',
  textAlign: 'center',
  fontSize: '13px',
  color: '#bac6d8',
  marginTop: '2px'
} as const

const posterCodeStyle = {
  position: 'relative',
  zIndex: '1',
  textAlign: 'center',
  fontSize: '18px',
  fontWeight: '700',
  letterSpacing: '0.25em',
  color: '#67e8f9',
  fontFamily: '"Courier New", monospace',
  textShadow: '0 0 20px rgba(103, 232, 249, 0.4)',
  marginTop: '2px'
} as const
</script>

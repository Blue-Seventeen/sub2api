<template>
  <BaseDialog
    :show="show"
    :title="modelValue?.id ? '编辑推广话术' : '新增推广话术'"
    width="wide"
    content-class="promo-modal"
    header-class="promo-modal-header"
    body-class="promo-modal-body"
    footer-class="promo-modal-footer"
    @close="$emit('close')"
  >
    <form id="promotion-script-form" class="space-y-5" @submit.prevent="handleSubmit">
      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-5">
        <div class="grid gap-4 md:grid-cols-[1.2fr_1fr]">
          <label class="space-y-2">
            <span class="text-sm text-slate-400">话术名称</span>
            <input v-model="form.name" type="text" class="promo-input-dark" placeholder="例如：朋友圈文案" required />
          </label>
          <label class="space-y-2">
            <span class="text-sm text-slate-400">话术标签</span>
            <input v-model="form.category" type="text" class="promo-input-dark" placeholder="例如：朋友圈 / 技术群 / 社交平台" maxlength="32" />
          </label>
        </div>
        <div class="mt-4 flex flex-wrap items-center gap-2">
          <span class="text-xs uppercase tracking-[0.2em] text-slate-500">当前标签</span>
          <span class="rounded-full border px-3 py-1 text-xs font-medium" :class="tagColorClass(form.category)">
            {{ displayTag(form.category) }}
          </span>
        </div>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-5">
        <label class="space-y-2">
          <span class="text-sm text-slate-400">话术内容</span>
          <textarea
            v-model="form.content"
            rows="8"
            class="promo-input-dark min-h-[180px] resize-none"
            placeholder="请输入推广话术，使用占位符如 {{INVITE_CODE}} 会被自动替换..."
            required
          ></textarea>
        </label>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-5">
        <div class="mb-3 text-xs uppercase tracking-[0.2em] text-slate-500">可用占位符</div>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="placeholder in placeholders"
            :key="placeholder"
            type="button"
            class="rounded-lg bg-slate-800 px-3 py-1.5 text-xs text-slate-300 transition-colors hover:bg-cyan-500/20 hover:text-cyan-300"
            @click="insertPlaceholder(placeholder)"
          >
            {{ placeholder }}
          </button>
        </div>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-5">
        <div class="mb-2 text-sm text-slate-400">预览（占位符会被替换为示例数据）</div>
        <div class="rounded-xl border border-white/10 bg-slate-900/70 p-4 text-sm leading-7 whitespace-pre-wrap text-slate-300">
          {{ previewText || '请输入话术内容后在这里查看预览效果。' }}
        </div>
      </section>

      <label class="flex items-center gap-3 rounded-2xl border border-white/10 bg-slate-950/60 p-4 text-sm text-slate-300">
        <input v-model="form.enabled" type="checkbox" class="h-4 w-4 rounded border-white/20 bg-slate-900 text-cyan-400" />
        启用该话术
      </label>
    </form>

    <template #footer>
      <div class="grid w-full grid-cols-2 gap-3">
        <button class="promo-btn promo-btn-secondary min-w-0 w-full justify-center whitespace-nowrap px-4" @click="$emit('close')">取消</button>
        <button class="promo-btn promo-btn-primary min-w-0 w-full justify-center whitespace-nowrap px-4" type="submit" form="promotion-script-form" :disabled="submitting">
          {{ submitting ? '处理中...' : '保存话术' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PromotionScript } from '@/api/promotion'

const props = defineProps<{
  show: boolean
  submitting?: boolean
  modelValue?: PromotionScript | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', payload: { id?: number; name: string; category: string; content: string; enabled: boolean }): void
}>()

const placeholders = ['{{INVITE_CODE}}', '{{REF_LINK}}', '{{USER_NAME}}', '{{SITE_NAME}}', '{{LEVEL}}', '{{TOTAL_EARNINGS}}']

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

const form = reactive({
  id: undefined as number | undefined,
  name: '',
  category: '',
  content: '',
  enabled: true
})

watch(
  () => props.show,
  (show) => {
    if (!show) return
    form.id = props.modelValue?.id
    form.name = props.modelValue?.name || ''
    form.category = props.modelValue?.category || ''
    form.content = props.modelValue?.content || ''
    form.enabled = props.modelValue?.enabled ?? true
  }
)

const previewText = computed(() => {
  return form.content
    .replace(/\{\{INVITE_CODE\}\}/g, 'DA044C326EC7BF13')
    .replace(/\{\{REF_LINK\}\}/g, 'https://api.example.test/?ref=DA044C326EC7BF13')
    .replace(/\{\{USER_NAME\}\}/g, '推广达人')
    .replace(/\{\{SITE_NAME\}\}/g, 'Sub2API')
    .replace(/\{\{LEVEL\}\}/g, 'Lv3 推广大使')
    .replace(/\{\{TOTAL_EARNINGS\}\}/g, '128.50')
})

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

function insertPlaceholder(placeholder: string) {
  form.content += placeholder
}

function handleSubmit() {
  emit('confirm', {
    id: form.id,
    name: form.name.trim(),
    category: form.category.trim(),
    content: form.content.trim(),
    enabled: form.enabled
  })
}
</script>

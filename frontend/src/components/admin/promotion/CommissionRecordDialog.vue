<template>
  <BaseDialog
    :show="show"
    :title="mode === 'edit' ? '编辑佣金记录' : '查看佣金记录'"
    width="wide"
    content-class="border-white/10 bg-slate-900 text-slate-100"
    header-class="border-white/10 bg-slate-900"
    body-class="bg-slate-900"
    footer-class="border-white/10 bg-slate-900"
    @close="$emit('close')"
  >
    <div v-if="record" class="space-y-5">
      <section class="grid gap-3 md:grid-cols-2">
        <div class="rounded-2xl border border-white/10 bg-slate-950/60 p-4">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">收益用户</div>
          <div class="mt-3 text-base font-semibold text-white">{{ record.beneficiary_email }}</div>
          <div class="mt-1 text-xs text-slate-500">来源用户：{{ record.source_user_email || '-' }}</div>
        </div>
        <div class="rounded-2xl border border-white/10 bg-slate-950/60 p-4">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">记录信息</div>
          <div class="mt-3 text-sm text-slate-300">业务日：{{ formatBusinessDate(record.business_date) }}</div>
          <div class="mt-1 text-xs text-slate-500">创建时间：{{ formatDateTime(record.created_at) }}</div>
        </div>
      </section>

      <section class="grid gap-3 md:grid-cols-3">
        <div class="rounded-2xl border border-white/10 bg-white/5 p-4 text-center">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">真实消费</div>
          <div class="mt-2 text-xl font-semibold text-slate-100">${{ money(record.base_amount) }}</div>
        </div>
        <div class="rounded-2xl border border-white/10 bg-white/5 p-4 text-center">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">返利比例</div>
          <div class="mt-2 text-xl font-semibold text-cyan-300">{{ rate(record.rate_snapshot) }}</div>
        </div>
        <div class="rounded-2xl border border-white/10 bg-white/5 p-4 text-center">
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">奖励类型</div>
          <div class="mt-2 text-xl font-semibold text-emerald-300">{{ typeLabel(record.commission_type, record.relation_depth) }}</div>
        </div>
      </section>

      <form id="promotion-commission-record-form" class="space-y-4" @submit.prevent="handleSubmit">
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">奖励金额 (USD)</span>
          <input
            v-model.number="amount"
            type="number"
            step="0.01"
            class="promo-input-dark"
            :readonly="mode === 'view'"
          />
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">返利说明 / 调整原因</span>
          <textarea
            v-model="note"
            rows="4"
            class="promo-input-dark"
            :readonly="mode === 'view'"
          ></textarea>
        </label>
      </form>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="promo-btn promo-btn-secondary" @click="$emit('close')">关闭</button>
        <button v-if="mode === 'edit'" class="promo-btn promo-btn-primary" type="submit" form="promotion-commission-record-form" :disabled="submitting || amount === 0">
          {{ submitting ? '处理中...' : '保存修改' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { PromotionCommissionItem } from '@/api/promotion'

const props = defineProps<{
  show: boolean
  submitting?: boolean
  mode: 'view' | 'edit'
  record: PromotionCommissionItem | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', payload: { id: number; amount: number; note?: string }): void
}>()

const amount = ref(0)
const note = ref('')

watch(
  () => props.record,
  (record) => {
    amount.value = record?.amount || 0
    note.value = record?.note || ''
  },
  { immediate: true }
)

function handleSubmit() {
  if (!props.record || props.mode !== 'edit') return
  emit('confirm', {
    id: props.record.id,
    amount: Number(amount.value),
    note: note.value.trim() || undefined
  })
}

function money(value?: number) {
  return Number(value || 0).toFixed(2)
}

function rate(value?: number) {
  if (value == null) return '--'
  return `${Number(value).toFixed(4).replace(/\.?0+$/, '')}%`
}

function typeLabel(type: string, depth: number) {
  if (type === 'activation') return '激活奖励'
  if (type === 'adjustment') return '差额调整'
  return depth === 2 ? '二级返利' : '佣金返利'
}

function formatDateTime(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function formatBusinessDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleDateString()
}
</script>

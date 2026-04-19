<template>
  <BaseDialog
    :show="show"
    title="设置上级代理"
    width="wide"
    content-class="border-white/10 bg-slate-900 text-slate-100"
    header-class="border-white/10 bg-slate-900"
    body-class="bg-slate-900"
    footer-class="border-white/10 bg-slate-900"
    @close="$emit('close')"
  >
    <form id="promotion-set-parent-form" class="space-y-5" @submit.prevent="handleSubmit">
      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 text-sm leading-7 text-slate-300">
        改绑只影响后续消费佣金，不会回刷历史佣金。请先确认当前用户，再通过邮箱或用户名搜索需要绑定的上级代理。
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4">
        <div class="text-xs uppercase tracking-[0.24em] text-slate-500">当前用户</div>
        <div class="mt-3 text-lg font-semibold text-white">
          {{ currentUser?.email || (initialUserId ? `用户 #${initialUserId}` : '未选择用户') }}
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-500">
          <span v-if="currentUser?.username" class="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-slate-300">用户名：{{ currentUser.username }}</span>
          <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 text-slate-200">{{ currentUser?.level_name || '未配置等级' }}</span>
        </div>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 space-y-4">
        <label class="space-y-2">
          <span class="input-label mb-0">搜索上级用户</span>
          <div class="flex gap-2">
            <input
              v-model.trim="searchKeyword"
              type="text"
              class="promo-input-dark flex-1"
              placeholder="输入邮箱或用户名搜索"
              @keyup.enter="searchCandidates"
            />
            <button type="button" class="promo-btn promo-btn-secondary" :disabled="searching" @click="searchCandidates">
              {{ searching ? '搜索中...' : '搜索' }}
            </button>
          </div>
        </label>

        <div v-if="searching" class="rounded-2xl border border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
          正在搜索候选上级...
        </div>
        <div v-else-if="searchKeyword && !candidates.length" class="rounded-2xl border border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
          未找到匹配的上级候选人
        </div>
        <div v-else-if="candidates.length" class="space-y-3">
          <article
            v-for="candidate in candidates"
            :key="candidate.user_id"
            class="flex flex-col gap-3 rounded-2xl border border-white/10 bg-white/5 p-4 lg:flex-row lg:items-center lg:justify-between"
          >
            <div class="min-w-0">
              <div class="truncate font-medium text-white">{{ candidate.email }}</div>
              <div class="mt-1 text-xs text-slate-500">用户名：{{ candidate.username || '--' }}</div>
              <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                <span class="rounded-full border border-white/10 bg-slate-900/60 px-3 py-1 text-slate-200">{{ candidate.level_name || '未配置等级' }}</span>
                <span class="rounded-full border border-white/10 bg-slate-900/60 px-3 py-1 text-slate-300">
                  {{ candidate.parent_email ? `当前上级：${candidate.parent_email}` : '当前无上级' }}
                </span>
              </div>
            </div>
            <button
              type="button"
              class="promo-btn"
              :class="selectedParent?.user_id === candidate.user_id ? 'promo-btn-secondary' : 'promo-btn-primary'"
              @click="selectParent(candidate)"
            >
              {{ selectedParent?.user_id === candidate.user_id ? '已选择' : '选择为上级' }}
            </button>
          </article>
        </div>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 space-y-4">
        <div>
          <div class="input-label mb-2">已选上级</div>
          <div v-if="selectedParent" class="rounded-2xl border border-emerald-400/20 bg-emerald-500/10 p-4">
            <div class="font-medium text-emerald-300">{{ selectedParent.email }}</div>
            <div class="mt-1 text-xs text-emerald-200/80">用户名：{{ selectedParent.username || '--' }}</div>
          </div>
          <div v-else class="rounded-2xl border border-dashed border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
            请先搜索并选择一个上级代理
          </div>
        </div>
        <label class="space-y-2">
          <span class="input-label mb-0">备注</span>
          <textarea v-model="note" rows="3" class="promo-input-dark" placeholder="例如：人工调整推广关系"></textarea>
        </label>
      </section>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="promo-btn promo-btn-secondary" @click="$emit('close')">取消</button>
        <button class="promo-btn promo-btn-primary" type="submit" form="promotion-set-parent-form" :disabled="submitting || !initialUserId || !selectedParent">
          {{ submitting ? '处理中...' : '确认设置' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { adminPromotionAPI, type PromotionRelationItem } from '@/api/admin/promotion'

const props = defineProps<{
  show: boolean
  submitting?: boolean
  initialUserId?: number | null
  currentUser?: PromotionRelationItem | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', payload: { user_id: number; parent_user_id: number; note?: string }): void
}>()

const searchKeyword = ref('')
const searching = ref(false)
const candidates = ref<PromotionRelationItem[]>([])
const selectedParent = ref<PromotionRelationItem | null>(null)
const note = ref('')

watch(
  () => props.show,
  (show) => {
    if (show) {
      searchKeyword.value = ''
      candidates.value = []
      selectedParent.value = null
      note.value = ''
    }
  }
)

async function searchCandidates() {
  const keyword = searchKeyword.value.trim()
  if (!keyword) {
    candidates.value = []
    return
  }
  searching.value = true
  try {
    const response = await adminPromotionAPI.getRelations({
      page: 1,
      page_size: 8,
      keyword
    })
    candidates.value = (response.data.items || []).filter(item => item.user_id !== props.initialUserId)
  } finally {
    searching.value = false
  }
}

function selectParent(candidate: PromotionRelationItem) {
  selectedParent.value = candidate
}

function handleSubmit() {
  if (!props.initialUserId || !selectedParent.value) return
  emit('confirm', {
    user_id: Number(props.initialUserId),
    parent_user_id: Number(selectedParent.value.user_id),
    note: note.value.trim() || undefined
  })
}
</script>

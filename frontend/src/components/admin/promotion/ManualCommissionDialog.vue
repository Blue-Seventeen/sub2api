<template>
  <BaseDialog
    :show="show"
    title="差额调整"
    width="wide"
    content-class="promo-modal"
    header-class="promo-modal-header"
    body-class="promo-modal-body"
    footer-class="promo-modal-footer"
    @close="$emit('close')"
  >
    <form id="promotion-manual-grant-form" class="space-y-5" @submit.prevent="handleSubmit">
      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 text-sm leading-7 text-slate-300">
        管理员执行的人工发放与扣减统一记为“差额调整”：金额大于 0 代表奖励，金额小于 0 代表扣除，结果会立即计入用户真实余额，并在用户收益明细中展示调整原因。
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 space-y-4">
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">搜索用户</span>
          <div class="flex gap-2">
            <input
              v-model.trim="searchKeyword"
              type="text"
              class="promo-input-dark flex-1"
              placeholder="输入邮箱 / 用户名 / 邀请码"
              @keyup.enter="searchUsers"
            />
            <button type="button" class="promo-btn promo-btn-secondary" :disabled="searching" @click="searchUsers">
              {{ searching ? '搜索中...' : '搜索' }}
            </button>
          </div>
        </label>

        <div v-if="searching" class="rounded-2xl border border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
          正在搜索用户...
        </div>
        <div v-else-if="searchKeyword && !candidates.length" class="rounded-2xl border border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
          未找到匹配用户
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
              <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-slate-400">
                <span class="rounded-full border border-white/10 bg-slate-900/60 px-3 py-1">{{ candidate.level_name || '未配置等级' }}</span>
                <span class="rounded-full border border-white/10 bg-slate-900/60 px-3 py-1">邀请码：{{ candidate.invite_code || '--' }}</span>
              </div>
            </div>
            <button
              type="button"
              class="promo-btn"
              :class="selectedUser?.user_id === candidate.user_id ? 'promo-btn-secondary' : 'promo-btn-primary'"
              @click="selectUser(candidate)"
            >
              {{ selectedUser?.user_id === candidate.user_id ? '已选择' : '选择用户' }}
            </button>
          </article>
        </div>
      </section>

      <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 space-y-4">
        <div>
          <div class="text-xs uppercase tracking-[0.24em] text-slate-500">已选用户</div>
          <div v-if="selectedUser" class="mt-3 rounded-2xl border border-cyan-400/20 bg-cyan-500/10 p-4">
            <div class="font-medium text-cyan-200">{{ selectedUser.email }}</div>
            <div class="mt-1 text-xs text-cyan-100/80">用户名：{{ selectedUser.username || '--' }}</div>
          </div>
          <div v-else class="mt-3 rounded-2xl border border-dashed border-white/10 bg-white/5 px-4 py-6 text-center text-sm text-slate-400">
            请先搜索并选择用户
          </div>
        </div>

        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">调整金额 (USD)</span>
          <input v-model.number="amount" type="number" step="0.01" class="promo-input-dark" placeholder="正数为奖励，负数为扣除" required />
        </label>

        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">调整原因</span>
          <textarea v-model="note" rows="3" class="promo-input-dark" placeholder="请输入发放或调整原因..." required></textarea>
        </label>
      </section>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="promo-btn promo-btn-secondary" @click="$emit('close')">取消</button>
        <button class="promo-btn promo-btn-primary" type="submit" form="promotion-manual-grant-form" :disabled="submitting || !selectedUser || amount === 0 || !note.trim()">
          {{ submitting ? '处理中...' : '确认调整' }}
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
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', payload: { user_id: number; amount: number; note?: string }): void
}>()

const searchKeyword = ref('')
const searching = ref(false)
const candidates = ref<PromotionRelationItem[]>([])
const selectedUser = ref<PromotionRelationItem | null>(null)
const amount = ref(0)
const note = ref('')
let debounceTimer: ReturnType<typeof setTimeout> | null = null

watch(
  () => props.show,
  (show) => {
    if (show) {
      searchKeyword.value = ''
      candidates.value = []
      selectedUser.value = null
      amount.value = 0
      note.value = ''
    }
  }
)

watch(searchKeyword, (value) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    if (value.trim()) {
      void searchUsers()
    } else {
      candidates.value = []
    }
  }, 250)
})

async function searchUsers() {
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
    candidates.value = response.data.items || []
  } finally {
    searching.value = false
  }
}

function selectUser(user: PromotionRelationItem) {
  selectedUser.value = user
}

function handleSubmit() {
  if (!selectedUser.value || amount.value === 0 || !note.value.trim()) return
  emit('confirm', {
    user_id: selectedUser.value.user_id,
    amount: Number(amount.value),
    note: note.value.trim()
  })
}
</script>

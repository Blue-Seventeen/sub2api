<template>
  <BaseDialog :show="show" :title="operation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')" width="narrow" @close="$emit('close')">
    <form v-if="user" id="balance-form" @submit.prevent="handleBalanceSubmit" class="space-y-5">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100"><span class="text-lg font-medium text-primary-700">{{ user.email.charAt(0).toUpperCase() }}</span></div>
        <div class="flex-1"><p class="font-medium text-gray-900">{{ user.email }}</p><p class="text-sm text-gray-500">{{ t('admin.users.displayBalanceLabel') }}: ${{ formatBalance(getDisplayBalance(user)) }}</p><p class="text-xs text-gray-400">{{ t('admin.users.realBalanceLabel') }}: ${{ formatBalance(getRealBalance(user)) }} · {{ t('admin.users.balanceOperationsUseReal') }}</p></div>
      </div>
      <div>
        <label class="input-label">{{ operation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount') }}</label>
        <div class="relative flex gap-2">
          <div class="relative flex-1"><div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-gray-500">$</div><input v-model.number="form.amount" type="number" step="any" min="0" required class="input pl-8" /></div>
          <button v-if="operation === 'subtract'" type="button" @click="fillAllBalance" class="btn btn-secondary whitespace-nowrap">{{ t('admin.users.withdrawAll') }}</button>
        </div>
      </div>
      <div><label class="input-label">{{ t('admin.users.notes') }}</label><textarea v-model="form.notes" rows="3" class="input"></textarea></div>
      <div v-if="form.amount > 0" class="rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950"><div class="flex items-center justify-between text-sm"><span class="text-gray-700 dark:text-gray-300">{{ t('admin.users.newBalance') }}:</span><span class="font-bold text-gray-900 dark:text-gray-100">${{ formatBalance(calculateNewBalance()) }}</span></div></div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="balance-form" :disabled="submitting || !form.amount" class="btn" :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'">{{ submitting ? t('common.saving') : t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean, user: AdminUser | null, operation: 'add' | 'subtract' }>()
const emit = defineEmits(['close', 'success']); const { t } = useI18n(); const appStore = useAppStore()

const submitting = ref(false); const form = reactive({ amount: 0, notes: '' })
// v0.1.114_Beta 增量兼容说明：
// 该弹窗中的充值/退款/重置逻辑一律按真实余额处理；
// 显示余额仅用于展示，继续优先复用旧字段 balance。
const getRealBalance = (user: AdminUser) => user.real_balance ?? user.balance ?? 0
// 管理员“显示余额”优先复用兼容旧字段 balance；display_balance 仅作为增量兼容回退字段。
const getDisplayBalance = (user: AdminUser) => user.balance ?? user.display_balance ?? getRealBalance(user)
watch(() => props.show, (v) => { if(v) { form.amount = 0; form.notes = '' } })

// 鏍煎紡鍖栦綑棰濓細鏄剧ず瀹屾暣绮惧害锛屽幓闄ゅ熬閮ㄥ浣欑殑0
const formatBalance = (value: number) => {
  if (value === 0) return '0.00'
  // 鏈€澶氫繚鐣?浣嶅皬鏁帮紝鍘婚櫎灏鹃儴鐨?
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  // 纭繚鑷冲皯鏈?浣嶅皬鏁?
  const parts = formatted.split('.')
  if (parts.length === 1) return formatted + '.00'
  if (parts[1].length === 1) return formatted + '0'
  return formatted
}

// 濉叆鍏ㄩ儴浣欓
const fillAllBalance = () => {
  if (props.user) {
    form.amount = getRealBalance(props.user)
  }
}

const calculateNewBalance = () => {
  if (!props.user) return 0
  const realBalance = getRealBalance(props.user)
  const result = props.operation === 'add' ? realBalance + form.amount : realBalance - form.amount
  // 閬垮厤娴偣鏁扮簿搴﹂棶棰樺鑷寸殑 -0.00 鏄剧ず
  return Math.abs(result) < 1e-10 ? 0 : result
}
const handleBalanceSubmit = async () => {
  if (!props.user) return
  if (!form.amount || form.amount <= 0) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }
  // 閫€娆炬椂楠岃瘉閲戦涓嶈秴杩囧疄闄呬綑棰?
  if (props.operation === 'subtract' && form.amount > getRealBalance(props.user)) {
    appStore.showError(t('admin.users.insufficientBalance'))
    return
  }
  submitting.value = true
  try {
    await adminAPI.users.updateBalance(props.user.id, form.amount, props.operation, form.notes)
    appStore.showSuccess(t('common.success')); emit('success'); emit('close')
  } catch (e: any) {
    console.error('Failed to update balance:', e)
    appStore.showError(e.response?.data?.detail || t('common.error'))
  } finally { submitting.value = false }
}
</script>


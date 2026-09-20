<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="narrow"
    @close="emit('close')"
  >
    <p class="text-sm text-gray-600 dark:text-gray-300">
      {{ description }}
    </p>
    <div class="mt-4 grid gap-3">
      <button
        type="button"
        class="rounded-lg border border-gray-200 px-4 py-3 text-left transition-colors hover:border-primary-400 hover:bg-primary-50 dark:border-dark-600 dark:hover:border-primary-500 dark:hover:bg-primary-900/20"
        @click="emit('select', 'group')"
      >
        <span class="block text-sm font-medium text-gray-900 dark:text-white">
          {{ t("admin.groups.modelsList.groupScope") }}
        </span>
        <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.modelsList.groupScopeHint") }}
        </span>
      </button>
      <button
        type="button"
        class="rounded-lg border border-gray-200 px-4 py-3 text-left transition-colors hover:border-primary-400 hover:bg-primary-50 dark:border-dark-600 dark:hover:border-primary-500 dark:hover:bg-primary-900/20"
        @click="emit('select', 'global')"
      >
        <span class="block text-sm font-medium text-gray-900 dark:text-white">
          {{ t("admin.groups.modelsList.globalScope") }}
        </span>
        <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.modelsList.globalScopeHint") }}
        </span>
      </button>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ t("common.cancel") }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import BaseDialog from "@/components/common/BaseDialog.vue";
import type { ModelListOperation, ModelListOperationScope } from "@/views/admin/groupsModelsList";

const { t } = useI18n();

const props = defineProps<{
  show: boolean;
  operation: ModelListOperation;
  selectedCount?: number;
}>();

const emit = defineEmits<{
  (event: "close"): void;
  (event: "select", scope: ModelListOperationScope): void;
}>();

const title = computed(() =>
  props.operation === "add"
    ? t("admin.groups.modelsList.addScopeTitle")
    : t("admin.groups.modelsList.deleteScopeTitle"),
);

const description = computed(() =>
  props.operation === "add"
    ? t("admin.groups.modelsList.addScopeDescription")
    : t("admin.groups.modelsList.deleteScopeDescription", {
        count: props.selectedCount ?? 1,
      }),
);
</script>

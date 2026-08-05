<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div
          class="flex flex-col justify-between gap-4 lg:flex-row lg:items-start"
        >
          <!-- Left: fuzzy search + filters (can wrap to multiple lines) -->
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-64">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.groups.searchGroups')"
                class="input pl-10"
                @input="handleSearch"
              />
            </div>
            <Select
              v-model="filters.platform"
              :options="platformFilterOptions"
              :placeholder="t('admin.groups.allPlatforms')"
              class="w-44"
              @change="loadGroups"
            />
            <Select
              v-model="filters.status"
              :options="statusOptions"
              :placeholder="t('admin.groups.allStatus')"
              class="w-40"
              @change="loadGroups"
            />
            <Select
              v-model="filters.is_exclusive"
              :options="exclusiveOptions"
              :placeholder="t('admin.groups.allGroups')"
              class="w-44"
              @change="loadGroups"
            />
          </div>

          <!-- Right: actions -->
          <div
            class="flex w-full flex-shrink-0 flex-wrap items-center justify-end gap-3 lg:w-auto"
          >
            <button
              @click="loadGroups"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon
                name="refresh"
                size="md"
                :class="loading ? 'animate-spin' : ''"
              />
            </button>
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary"
                :title="t('admin.groups.columnSettings')"
              >
                <Icon name="grid" size="md" class="mr-2" />
                <span class="hidden md:inline">{{
                  t("admin.groups.columnSettings")
                }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in toggleableColumns"
                  :key="col.key"
                  @click="toggleColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
            <button
              @click="openSortModal"
              class="btn btn-secondary"
              :title="t('admin.groups.sortOrder')"
            >
              <Icon name="arrowsUpDown" size="md" class="mr-2" />
              {{ t("admin.groups.sortOrder") }}
            </button>
            <button
              @click="openCreateModal"
              class="btn btn-primary"
              data-tour="groups-create-btn"
            >
              <Icon name="plus" size="md" class="mr-2" />
              {{ t("admin.groups.createGroup") }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="groups"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="sort_order"
          default-sort-order="asc"
          @sort="handleSort"
        >
          <template #cell-name="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">{{
              value
            }}</span>
          </template>

          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-gray-400"
              >#{{ value }}</span
            >
          </template>

          <template #cell-platform="{ value }">
            <span
              :class="[
                'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium',
                value === 'anthropic'
                  ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                  : value === 'openai'
                        ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                    : value === 'antigravity'
                      ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                      : value === 'grok'
                        ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
                      : value === 'zhipu'
                        ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                        : value === 'deepseek'
                          ? 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
                          : value === 'volcengine'
                            ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
                            : value === 'ali'
                              ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
                              : value === 'moonshot'
                                ? 'bg-fuchsia-100 text-fuchsia-700 dark:bg-fuchsia-900/30 dark:text-fuchsia-400'
                      : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
              ]"
            >
              <PlatformIcon :platform="value" size="xs" />
              {{ t("admin.groups.platforms." + value) }}
            </span>
          </template>

          <template #cell-billing_type="{ row }">
            <div class="space-y-1">
              <!-- Type Badge -->
              <span
                :class="[
                  'inline-block rounded-full px-2 py-0.5 text-xs font-medium',
                  row.subscription_type === 'subscription'
                    ? 'bg-violet-100 text-violet-700 dark:bg-violet-900/30 dark:text-violet-400'
                    : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
                ]"
              >
                {{
                  row.subscription_type === "subscription"
                    ? t("admin.groups.subscription.subscription")
                    : t("admin.groups.subscription.standard")
                }}
              </span>
              <!-- Subscription Limits - compact single line -->
              <div
                v-if="row.subscription_type === 'subscription'"
                class="space-y-0.5 text-xs text-gray-500 dark:text-gray-400"
              >
                <div
                  v-if="
                    row.daily_limit_usd ||
                    row.weekly_limit_usd ||
                    row.monthly_limit_usd ||
                    (row.custom_limit_hours && row.custom_limit_usd)
                  "
                  class="flex flex-wrap items-center gap-x-1 gap-y-0.5"
                >
                  <span v-if="row.daily_limit_usd" class="whitespace-nowrap">
                    <span
                      v-if="usageLoading"
                      class="font-medium text-gray-400 dark:text-gray-500"
                      >-</span
                    >
                    <span
                      v-else
                      :class="
                        getQuotaUsageClass(
                          usageMap.get(row.id)?.today_cost ?? 0,
                          row.daily_limit_usd
                        )
                      "
                      >{{
                        formatUsd(usageMap.get(row.id)?.today_cost ?? 0)
                      }}</span
                    >
                    <span class="text-gray-400 dark:text-gray-500">
                      / {{ formatUsd(row.daily_limit_usd) }}/{{
                        t("admin.groups.limitDay")
                      }}</span
                    >
                  </span>
                  <span
                    v-if="row.daily_limit_usd && (row.weekly_limit_usd || row.monthly_limit_usd || (row.custom_limit_hours && row.custom_limit_usd))"
                    class="mx-1 text-gray-300 dark:text-gray-600"
                    >·</span
                  >
                  <span v-if="row.weekly_limit_usd" class="whitespace-nowrap"
                    >{{ formatUsd(row.weekly_limit_usd) }}/{{
                      t("admin.groups.limitWeek")
                    }}</span
                  >
                  <span
                    v-if="row.weekly_limit_usd && (row.monthly_limit_usd || (row.custom_limit_hours && row.custom_limit_usd))"
                    class="mx-1 text-gray-300 dark:text-gray-600"
                    >·</span
                  >
                  <span v-if="row.monthly_limit_usd" class="whitespace-nowrap"
                    >{{ formatUsd(row.monthly_limit_usd) }}/{{
                      t("admin.groups.limitMonth")
                    }}</span
                  >
                  <span
                    v-if="row.monthly_limit_usd && row.custom_limit_hours && row.custom_limit_usd"
                    class="mx-1 text-gray-300 dark:text-gray-600"
                    >·</span
                  >
                  <span v-if="row.custom_limit_hours && row.custom_limit_usd" class="whitespace-nowrap"
                    >{{ formatUsd(row.custom_limit_usd) }}/{{
                      row.custom_limit_hours
                    }}H</span
                  >
                </div>
                <span v-else class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.subscription.noLimit")
                }}</span>
                <div class="text-gray-400 dark:text-gray-500">
                  {{ t("admin.groups.usageTotal") }}
                  <span class="ml-1 font-medium text-gray-600 dark:text-gray-300"
                    >{{
                      usageLoading
                        ? "-"
                        : formatUsd(usageMap.get(row.id)?.total_cost ?? 0)
                    }}</span
                  >
                </div>
              </div>
            </div>
          </template>

          <template #cell-rate_multiplier="{ value, row }">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-sm text-gray-700 dark:text-gray-300"
                >{{ value }}x</span
              >
              <span
                v-if="hasGroupPeakRate(row)"
                class="inline-flex"
              >
                <span
                  class="cursor-help rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700 outline-none ring-amber-300 transition-colors hover:bg-amber-200 focus:ring-2 dark:bg-amber-900/30 dark:text-amber-300 dark:hover:bg-amber-900/50"
                  role="button"
                  tabindex="0"
                  :aria-label="groupPeakRateAccessibleLabel(row)"
                  data-test="group-rate-peak"
                  @mouseenter="showGroupPeakRateTooltip(row, $event)"
                  @focus="showGroupPeakRateTooltip(row, $event)"
                  @mouseleave="hideGroupPeakRateTooltip"
                  @blur="hideGroupPeakRateTooltip"
                  @keydown.escape="hideGroupPeakRateTooltip"
                >
                  {{ groupPeakRateText(row) }}
                </span>
              </span>
            </div>
          </template>

          <template #cell-is_exclusive="{ value }">
            <span :class="['badge', value ? 'badge-primary' : 'badge-gray']">
              {{
                value ? t("admin.groups.exclusive") : t("admin.groups.public")
              }}
            </span>
          </template>

          <template #cell-account_count="{ row }">
            <div class="space-y-0.5 text-xs">
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsAvailable")
                }}</span>
                <span
                  class="ml-1 font-medium text-emerald-600 dark:text-emerald-400"
                  >{{ row.active_account_count || 0 }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
              <div v-if="row.rate_limited_account_count">
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsRateLimited")
                }}</span>
                <span
                  class="ml-1 font-medium text-amber-600 dark:text-amber-400"
                  >{{ row.rate_limited_account_count }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
              <div>
                <span class="text-gray-500 dark:text-gray-400">{{
                  t("admin.groups.accountsTotal")
                }}</span>
                <span
                  class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{ row.account_count || 0 }}</span
                >
                <span
                  class="ml-1 inline-flex items-center rounded bg-gray-100 px-1.5 py-0.5 font-medium text-gray-800 dark:bg-dark-600 dark:text-gray-300"
                  >{{ t("admin.groups.accountsUnit") }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <GroupCapacityBadge
              v-if="capacityMap.get(row.id)"
              :concurrency-used="capacityMap.get(row.id)!.concurrencyUsed"
              :concurrency-max="capacityMap.get(row.id)!.concurrencyMax"
              :sessions-used="capacityMap.get(row.id)!.sessionsUsed"
              :sessions-max="capacityMap.get(row.id)!.sessionsMax"
              :rpm-used="capacityMap.get(row.id)!.rpmUsed"
              :rpm-max="capacityMap.get(row.id)!.rpmMax"
            />
            <span v-else class="text-xs text-gray-400">-</span>
          </template>

          <template #cell-usage="{ row }">
            <div v-if="usageLoading" class="text-xs text-gray-400">-</div>
            <div v-else class="space-y-0.5 text-xs">
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageToday")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{
                    formatDisplayCost(usageMap.get(row.id)?.real_today_cost ?? usageMap.get(row.id)?.today_cost ?? 0)
                  }}</span
                >
              </div>
              <div class="text-gray-500 dark:text-gray-400">
                <span class="text-gray-400 dark:text-gray-500">{{
                  t("admin.groups.usageTotal")
                }}</span>
                <span class="ml-1 font-medium text-gray-700 dark:text-gray-300"
                  >{{
                    formatDisplayCost(usageMap.get(row.id)?.real_total_cost ?? usageMap.get(row.id)?.total_cost ?? 0)
                  }}</span
                >
              </div>
            </div>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active' ? 'badge-success' : 'badge-danger',
              ]"
            >
              {{ t("admin.accounts.status." + value) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                @click="handleEdit(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t("common.edit") }}</span>
              </button>
              <button
                data-testid="group-duplicate"
                :title="
                  duplicatingGroupIds.has(row.id)
                    ? t('admin.groups.duplicating')
                    : t('admin.groups.duplicate')
                "
                :disabled="duplicatingGroupIds.has(row.id)"
                @click="handleDuplicate(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              >
                <Icon name="copy" size="sm" />
                <span class="text-xs">
                  {{
                    duplicatingGroupIds.has(row.id)
                      ? t("admin.groups.duplicating")
                      : t("admin.groups.duplicate")
                  }}
                </span>
              </button>
              <button
                v-if="row.platform === 'composite'"
                @click="handleCompositeRoutes(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-cyan-600 dark:hover:bg-dark-700 dark:hover:text-cyan-400"
              >
                <Icon name="swap" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.compositeRoutes.action")
                }}</span>
              </button>
              <button
                @click="handleRateMultipliers(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-purple-600 dark:hover:bg-dark-700 dark:hover:text-purple-400"
              >
                <Icon name="dollar" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.rateMultipliers")
                }}</span>
              </button>
              <button
                @click="handleRPMOverrides(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-orange-600 dark:hover:bg-dark-700 dark:hover:text-orange-400"
              >
                <Icon name="bolt" size="sm" />
                <span class="text-xs">{{
                  t("admin.groups.rpmOverrides")
                }}</span>
              </button>
              <button
                @click="handleDelete(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t("common.delete") }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.groups.noGroupsYet')"
              :description="t('admin.groups.createFirstGroup')"
              :action-text="t('admin.groups.createGroup')"
              @action="openCreateModal"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Create Group Modal -->
    <BaseDialog
      :show="showCreateModal"
      :title="t('admin.groups.createGroup')"
      width="normal"
      @close="closeCreateModal"
    >
      <form
        id="create-group-form"
        @submit.prevent="handleCreateGroup"
        class="space-y-5"
      >
        <div>
          <label class="input-label">{{ t("admin.groups.form.name") }}</label>
          <input
            v-model="createForm.name"
            type="text"
            required
            class="input"
            :placeholder="t('admin.groups.enterGroupName')"
            data-tour="group-form-name"
          />
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.description")
          }}</label>
          <textarea
            v-model="createForm.description"
            rows="3"
            class="input"
            :placeholder="t('admin.groups.optionalDescription')"
          ></textarea>
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.platform")
          }}</label>
          <div class="mt-2 space-y-3">
            <div class="relative">
              <Icon
                name="search"
                size="sm"
                class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="createPlatformSearchQuery"
                type="text"
                class="input pl-10"
                placeholder="搜索平台，如 Anthropic / GLM / DeepSeek / Qwen"
              />
            </div>

            <div
              class="rounded-xl bg-gray-100 p-2 dark:bg-dark-700"
              data-tour="group-form-platform"
            >
              <div
                ref="createPlatformScrollRef"
                class="platform-scroll-strip flex gap-2 overflow-x-auto pb-1"
                @wheel.prevent="handleCreatePlatformWheel"
              >
                <button
                  v-for="option in filteredCreatePlatformOptions"
                  :key="option.value"
                  type="button"
                  @click="selectCreatePlatform(option.value)"
                  :class="[
                    'group min-w-[156px] shrink-0 rounded-xl border px-3 py-3 text-left transition-all',
                    createForm.platform === option.value
                      ? option.activeClass
                      : 'border-transparent bg-transparent hover:bg-white/80 hover:shadow-sm dark:hover:bg-dark-600/80',
                  ]"
                >
                  <div class="flex items-center gap-3">
                    <div
                      :class="[
                        'flex h-9 w-9 shrink-0 items-center justify-center rounded-lg transition-colors',
                        createForm.platform === option.value
                          ? option.iconActiveClass
                          : 'bg-white/80 text-gray-500 dark:bg-dark-600 dark:text-gray-400',
                      ]"
                    >
                      <PlatformIcon :platform="option.value" size="md" />
                    </div>
                    <div class="min-w-0">
                      <div
                        :class="[
                          'truncate text-sm font-semibold',
                          createForm.platform === option.value
                            ? option.textActiveClass
                            : 'text-gray-700 dark:text-gray-200',
                        ]"
                      >
                        {{ option.label }}
                      </div>
                      <div
                        class="truncate text-xs text-gray-500 dark:text-gray-400"
                      >
                        {{ option.subtitle }}
                      </div>
                    </div>
                  </div>
                </button>
              </div>

              <p
                v-if="filteredCreatePlatformOptions.length === 0"
                class="px-2 py-3 text-sm text-gray-500 dark:text-gray-400"
              >
                没有匹配的平台，请换个关键词试试
              </p>
            </div>
          </div>
          <p class="input-hint">{{ t("admin.groups.platformHint") }}</p>
        </div>
        <!-- 浠庡垎缁勫鍒惰处鍙?-->
        <div v-if="copyAccountsGroupOptions.length > 0">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.copyAccounts.title") }}
            </label>
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.copyAccounts.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <!-- 宸查€夊垎缁勬爣绛?-->
          <div
            v-if="createForm.copy_accounts_from_group_ids.length > 0"
            class="flex flex-wrap gap-1.5 mb-2"
          >
            <span
              v-for="groupId in createForm.copy_accounts_from_group_ids"
              :key="groupId"
              class="inline-flex items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
            >
              {{
                copyAccountsGroupOptions.find((o) => o.value === groupId)
                  ?.label || `#${groupId}`
              }}
              <button
                type="button"
                @click="
                  createForm.copy_accounts_from_group_ids =
                    createForm.copy_accounts_from_group_ids.filter(
                      (id) => id !== groupId,
                    )
                "
                class="ml-0.5 text-primary-500 hover:text-primary-700 dark:hover:text-primary-200"
              >
                <Icon name="x" size="xs" />
              </button>
            </span>
          </div>
          <!-- 分组选择下拉 -->
          <select
            class="input"
            @change="
              (e) => {
                const val = Number((e.target as HTMLSelectElement).value);
                if (
                  val &&
                  !createForm.copy_accounts_from_group_ids.includes(val)
                ) {
                  createForm.copy_accounts_from_group_ids.push(val);
                }
                (e.target as HTMLSelectElement).value = '';
              }
            "
          >
            <option value="">
              {{ t("admin.groups.copyAccounts.selectPlaceholder") }}
            </option>
            <option
              v-for="opt in copyAccountsGroupOptions"
              :key="opt.value"
              :value="opt.value"
              :disabled="
                createForm.copy_accounts_from_group_ids.includes(opt.value)
              "
            >
              {{ opt.label }}
            </option>
          </select>
          <p class="input-hint">{{ t("admin.groups.copyAccounts.hint") }}</p>
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.rateMultiplier")
          }}</label>
          <input
            v-model.number="createForm.rate_multiplier"
            type="number"
            step="0.001"
            min="0"
            required
            class="input"
            data-tour="group-form-multiplier"
          />
          <p class="input-hint">{{ t("admin.groups.rateMultiplierHint") }}</p>
        </div>
        <div>
          <label class="input-label">{{ t("admin.groups.form.rpmLimit") }}</label>
          <input
            v-model.number="createForm.rpm_limit"
            type="number"
            min="0"
            step="1"
            class="input"
            :placeholder="t('admin.groups.form.rpmLimitPlaceholder')"
          />
          <p class="input-hint">{{ t("admin.groups.form.rpmLimitHint") }}</p>
        </div>
        <ReasoningEffortPolicyFields
          v-if="supportsReasoningEffortPolicyPlatform(createForm.platform)"
          ref="createReasoningEffortPolicyRef"
          id-prefix="create-group-reasoning"
          :platform="createForm.platform"
          v-model:max-effort="createForm.max_reasoning_effort"
          v-model:mappings="createForm.reasoning_effort_mappings"
        />
        <div
          class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-700/40"
        >
          <div class="flex items-center justify-between gap-4">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                启动 New-API 风格接口
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                开启后，该分组下通用网关选中的账号优先走 New-API 风格 adapter；关闭时保留账号级开关和原链路。
              </p>
            </div>
            <button
              type="button"
              @click="
                createForm.newapi_style_interface_enabled =
                  !createForm.newapi_style_interface_enabled
              "
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors',
                createForm.newapi_style_interface_enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createForm.newapi_style_interface_enabled
                    ? 'translate-x-6'
                    : 'translate-x-1',
                ]"
              />
            </button>
          </div>
        </div>
        <div
          v-if="createForm.subscription_type !== 'subscription'"
          data-tour="group-form-exclusive"
        >
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.form.exclusive") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <!-- Tooltip Popover -->
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="mb-2 text-xs font-medium">
                    {{ t("admin.groups.exclusiveTooltip.title") }}
                  </p>
                  <p class="mb-2 text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.exclusiveTooltip.description") }}
                  </p>
                  <div class="rounded bg-gray-800 p-2 dark:bg-gray-700">
                    <p class="text-xs leading-relaxed text-gray-300">
                      <span
                        class="inline-flex items-center gap-1 text-primary-400"
                        ><Icon name="lightbulb" size="xs" />
                        {{ t("admin.groups.exclusiveTooltip.example") }}</span
                      >
                      {{ t("admin.groups.exclusiveTooltip.exampleContent") }}
                    </p>
                  </div>
                  <!-- Arrow -->
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="createForm.is_exclusive = !createForm.is_exclusive"
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                createForm.is_exclusive
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createForm.is_exclusive ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                createForm.is_exclusive
                  ? t("admin.groups.exclusive")
                  : t("admin.groups.public")
              }}
            </span>
          </div>
        </div>

        <!-- Subscription Configuration -->
        <div class="mt-4 border-t pt-4">
          <div>
            <label class="input-label">{{
              t("admin.groups.subscription.type")
            }}</label>
            <Select
              v-model="createForm.subscription_type"
              :options="subscriptionTypeOptions"
            />
            <p class="input-hint">
              {{ t("admin.groups.subscription.typeHint") }}
            </p>
          </div>

          <!-- Subscription limits (only show when subscription type is selected) -->
          <div
            v-if="createForm.subscription_type === 'subscription'"
            class="space-y-4 border-l-2 border-primary-200 pl-4 dark:border-primary-800"
          >
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.dailyLimit")
              }}</label>
              <input
                v-model.number="createForm.daily_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.weeklyLimit")
              }}</label>
              <input
                v-model.number="createForm.weekly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.monthlyLimit")
              }}</label>
              <input
                v-model.number="createForm.monthly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.subscription.customLimit")
                }}</label>
                <input
                  v-model.number="createForm.custom_limit_usd"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input"
                  :placeholder="t('admin.groups.subscription.noLimit')"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.subscription.customWindowHours")
                }}</label>
                <input
                  v-model.number="createForm.custom_limit_hours"
                  type="number"
                  step="1"
                  min="0"
                  max="87600"
                  class="input"
                  placeholder="72"
                />
              </div>
            </div>
          </div>
        </div>

        <div class="border-t pt-4">
          <div class="mb-3 flex items-center justify-between gap-3">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t("admin.groups.modelsList.title") }}
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelsList.hint") }}
              </p>
            </div>
            <button
              type="button"
              @click="createModelsListState.enabled = !createModelsListState.enabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors',
                createModelsListState.enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createModelsListState.enabled ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
          </div>
          <div
            v-if="createModelsListState.enabled"
            class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50/50 dark:border-dark-600 dark:bg-dark-800/40"
          >
            <div
              v-if="!createModelsListLoading"
              class="flex items-center justify-between gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-600 dark:bg-dark-800"
            >
              <span class="text-gray-500 dark:text-gray-400">
                {{
                  t("admin.groups.modelsList.selectedCount", {
                    selected: createModelsListSelectedCount,
                    total: createModelsListState.items.length,
                  })
                }}
              </span>
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  class="rounded px-2 py-1 font-medium text-primary-600 transition-colors hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                  @click="addModelsListItem(createModelsListState)"
                >
                  {{ t("admin.groups.modelsList.add") }}
                </button>
                <button
                  type="button"
                  :disabled="createModelsListSelectedCount === 0"
                  class="rounded px-2 py-1 font-medium text-red-600 transition-colors hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="removeSelectedModelsListItems(createModelsListState)"
                >
                  {{ t("admin.groups.modelsList.delete") }}
                </button>
              </div>
            </div>
            <div
              class="max-h-64 space-y-2 overflow-y-auto p-2"
            >
              <p v-if="createModelsListLoading" class="text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelsList.loading") }}
              </p>
              <p
                v-else-if="createModelsListState.items.length === 0"
                class="text-xs text-gray-500 dark:text-gray-400"
              >
                {{ t("admin.groups.modelsList.empty") }}
              </p>
              <div
                v-for="(item, index) in createModelsListState.items"
                :key="item.uid"
                class="flex items-center gap-2 rounded border border-gray-200 bg-white px-3 py-2 dark:border-dark-600 dark:bg-dark-800"
              >
                <input
                  v-model="item.selected"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <input
                  v-if="item.editing"
                  v-model="item.draft"
                  type="text"
                  class="input min-w-0 flex-1 py-1 text-sm"
                  :placeholder="t('admin.groups.modelsList.modelPlaceholder')"
                  @keydown.enter.prevent="commitModelsListItemEdit(createModelsListState, item)"
                  @keydown.esc.prevent="cancelModelsListItemEdit(createModelsListState, item)"
                  @blur="commitModelsListItemEdit(createModelsListState, item)"
                />
                <span v-else class="min-w-0 flex-1 break-all text-sm text-gray-700 dark:text-gray-300">
                  {{ item.id }}
                </span>
                <button
                  type="button"
                  class="rounded px-2 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                  @click="startEditModelsListItem(item)"
                >
                  {{ t("admin.groups.modelsList.edit") }}
                </button>
                <button
                  type="button"
                  class="rounded px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="removeModelsListItem(createModelsListState, item)"
                >
                  {{ t("admin.groups.modelsList.delete") }}
                </button>
                <button
                  type="button"
                  :disabled="index === 0"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveCreateModelsListItem(index, index - 1)"
                >
                  <Icon name="arrowUp" size="sm" />
                </button>
                <button
                  type="button"
                  :disabled="index === createModelsListState.items.length - 1"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveCreateModelsListItem(index, index + 1)"
                >
                  <Icon name="arrowDown" size="sm" />
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- 鍥剧墖鐢熸垚璁¤垂閰嶇疆 -->
        <div
          v-if="supportsImagePricingPlatform(createForm.platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(imagePricingI18nKey(createForm.platform, "title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(imagePricingI18nKey(createForm.platform, "description")) }}
          </p>
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="createForm.allow_image_generation"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(imagePricingI18nKey(createForm.platform, "allowImageGeneration")) }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="createForm.image_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(imagePricingI18nKey(createForm.platform, "independentMultiplier")) }}
            </label>
          </div>
          <div
            v-if="createForm.image_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(imagePricingI18nKey(createForm.platform, "imageMultiplier"))
            }}</label>
            <input
              v-model.number="createForm.image_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="input-label">1K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="createForm.image_price_1k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(createForm.platform, 'image_price_1k')"
              />
            </div>
            <div>
              <label class="input-label">2K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="createForm.image_price_2k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(createForm.platform, 'image_price_2k')"
              />
            </div>
            <div>
              <label class="input-label">4K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="createForm.image_price_4k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(createForm.platform, 'image_price_4k')"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(imagePricingI18nKey(createForm.platform, "modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(imagePricingI18nKey(createForm.platform, "finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in createImageFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
          <div v-if="createForm.platform === 'gemini' && createForm.allow_image_generation" class="mt-4 border-t border-dashed border-gray-200 pt-4 dark:border-dark-700">
            <label
              class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="createForm.allow_batch_image_generation"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t("admin.groups.imagePricing.allowBatchImageGeneration") }}
            </label>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.groups.imagePricing.batchSectionHint") }}
            </p>
            <div
              v-if="createForm.allow_batch_image_generation"
              class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2"
            >
              <div>
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchDiscountMultiplier")
                }}</label>
                <input
                  v-model.number="createForm.batch_image_discount_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.5"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchHoldMultiplier")
                }}</label>
                <input
                  v-model.number="createForm.batch_image_hold_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.6"
                />
              </div>
            </div>
          </div>
          <p
            v-else-if="createForm.platform !== 'gemini'"
            class="mt-4 border-t border-dashed border-gray-200 pt-4 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400"
          >
            {{ t("admin.groups.imagePricing.batchGeminiOnlyHint") }}
          </p>
        </div>

        <!-- 瑙嗛鐢熸垚璁¤垂閰嶇疆锛堜粎 Grok 骞冲彴锛?-->
        <div
          v-if="supportsVideoPricingPlatform(createForm.platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(videoPricingI18nKey("title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(videoPricingI18nKey("description")) }}
          </p>
          <div class="mb-4">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="createForm.video_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(videoPricingI18nKey("independentMultiplier")) }}
            </label>
          </div>
          <div
            v-if="createForm.video_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(videoPricingI18nKey("videoMultiplier"))
            }}</label>
            <input
              v-model.number="createForm.video_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="input-label">480p ($/s)</label>
              <input
                v-model.number="createForm.video_price_480p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(createForm.platform, 'video_price_480p')"
              />
            </div>
            <div>
              <label class="input-label">720p ($/s)</label>
              <input
                v-model.number="createForm.video_price_720p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(createForm.platform, 'video_price_720p')"
              />
            </div>
            <div>
              <label class="input-label">1080p ($/s)</label>
              <input
                v-model.number="createForm.video_price_1080p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(createForm.platform, 'video_price_1080p')"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(videoPricingI18nKey("modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(videoPricingI18nKey("finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in createVideoFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
        </div>

        <!-- 楂樺嘲鏃舵鍊嶇巼閰嶇疆 -->
        <div class="border-t pt-4">
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="createForm.peak_rate_enabled"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              <span>{{ t("admin.groups.peakRate.enable") }}</span>
            </label>
          </div>
          <div
            v-if="createForm.peak_rate_enabled"
            class="mb-4 space-y-3"
          >
            <div
              v-for="(window, index) in createForm.peak_rate_windows"
              :key="`create-peak-${index}`"
              class="grid grid-cols-1 gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600 md:grid-cols-[1fr_1fr_1fr_auto]"
            >
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakStart") }}</label>
                <input v-model="window.start" type="time" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakEnd") }}</label>
                <input v-model="window.end" type="time" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakMultiplier") }}</label>
                <input
                  v-model.number="window.multiplier"
                  type="number"
                  step="0.001"
                  min="0"
                  class="input"
                  placeholder="1"
                  :title="t('admin.groups.peakRate.multiplierHint')"
                />
              </div>
              <button
                type="button"
                class="btn btn-secondary self-end"
                @click="removePeakRateWindow(createForm.peak_rate_windows, index)"
              >
                {{ t("common.delete") }}
              </button>
            </div>
            <button
              type="button"
              class="btn btn-secondary"
              @click="addPeakRateWindow(createForm.peak_rate_windows)"
            >
              + {{ t("admin.groups.peakRate.addWindow") }}
            </button>
          </div>
        </div>

        <!-- 鏀寔鐨勬ā鍨嬬郴鍒楋紙浠?antigravity 骞冲彴锛?-->
        <!-- 分组利润控制（五个平台 token 请求） -->
        <div v-if="isProfitControlPlatform(createForm.platform)" class="border-t pt-4">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="createForm.profit_control_enabled"
              type="checkbox"
              class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <span>{{ t("admin.groups.profitControl.enable") }}</span>
          </label>
          <p class="mb-3 mt-1.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              createForm.profit_control_enabled
                ? t("admin.groups.profitControl.enabledHint")
                : t("admin.groups.profitControl.disabledHint")
            }}
          </p>
          <div
            v-if="createForm.profit_control_enabled"
            class="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2"
          >
            <div>
              <label class="input-label">{{ t("admin.groups.profitControl.minMargin") }}</label>
              <input
                v-model.number="createForm.profit_min_margin_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.minMarginHint')"
              />
            </div>
            <div>
              <label class="input-label">{{ t("admin.groups.profitControl.safetyBuffer") }}</label>
              <input
                v-model.number="createForm.profit_safety_buffer_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.safetyBufferHint')"
              />
            </div>
          </div>
        </div>

        <!-- 支持的模型系列（仅 antigravity 平台） -->
        <div v-if="createForm.platform === 'antigravity'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.supportedScopes.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.supportedScopes.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="space-y-2">
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="createForm.supported_model_scopes.includes('claude')"
                @change="toggleCreateScope('claude')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.claude")
              }}</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="
                  createForm.supported_model_scopes.includes('gemini_text')
                "
                @change="toggleCreateScope('gemini_text')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.geminiText")
              }}</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="
                  createForm.supported_model_scopes.includes('gemini_image')
                "
                @change="toggleCreateScope('gemini_image')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.geminiImage")
              }}</span>
            </label>
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.groups.supportedScopes.hint") }}
          </p>
        </div>

        <!-- MCP XML 鍗忚娉ㄥ叆锛堜粎 antigravity 骞冲彴锛?-->
        <div v-if="createForm.platform === 'antigravity'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.mcpXml.title") }}
            </label>
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.mcpXml.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="createForm.mcp_xml_inject = !createForm.mcp_xml_inject"
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                createForm.mcp_xml_inject
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createForm.mcp_xml_inject ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                createForm.mcp_xml_inject
                  ? t("admin.groups.mcpXml.enabled")
                  : t("admin.groups.mcpXml.disabled")
              }}
            </span>
          </div>
        </div>

        <!-- Claude Code 瀹㈡埛绔檺鍒讹紙浠?anthropic 骞冲彴锛?-->
        <div v-if="createForm.platform === 'anthropic'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.claudeCode.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.claudeCode.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="
                createForm.claude_code_only = !createForm.claude_code_only
              "
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                createForm.claude_code_only
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createForm.claude_code_only
                    ? 'translate-x-6'
                    : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                createForm.claude_code_only
                  ? t("admin.groups.claudeCode.enabled")
                  : t("admin.groups.claudeCode.disabled")
              }}
            </span>
          </div>
          <!-- 闄嶇骇鍒嗙粍閫夋嫨锛堜粎褰撳惎鐢?claude_code_only 鏃舵樉绀猴級 -->
          <div v-if="createForm.claude_code_only" class="mt-3">
            <label class="input-label">{{
              t("admin.groups.claudeCode.fallbackGroup")
            }}</label>
            <Select
              v-model="createForm.fallback_group_id"
              :options="fallbackGroupOptions"
              :placeholder="t('admin.groups.claudeCode.noFallback')"
            />
            <p class="input-hint">
              {{ t("admin.groups.claudeCode.fallbackHint") }}
            </p>
          </div>
        </div>

        <!-- OpenAI Messages 璋冨害閰嶇疆锛堜粎 openai 骞冲彴锛?-->
        <!-- Codex 网页搜索按次计费（仅 openai 平台） -->
        <div
          v-if="createForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.webSearchPricing.title") }}
          </h4>
          <div>
            <label class="input-label">{{
              t("admin.groups.webSearchPricing.pricePerCall")
            }}</label>
            <input
              v-model.number="createForm.web_search_price_per_call"
              type="number"
              step="0.001"
              min="0"
              placeholder="0.01"
              class="input"
            />
            <p class="input-hint">
              {{ t("admin.groups.webSearchPricing.pricePerCallHint") }}
            </p>
            <div
              class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300"
            >
              {{
                t("admin.groups.webSearchPricing.finalPricePreview", {
                  price: createWebSearchFinalPricePreview,
                })
              }}
            </div>
          </div>
        </div>

        <!-- OpenAI Live 开关（仅 openai 平台） -->
        <div
          v-if="createForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.openaiLive.title") }}
          </h4>
          <div class="flex items-center justify-between">
            <label class="text-sm text-gray-600 dark:text-gray-400">{{
              t("admin.groups.openaiLive.allow")
            }}</label>
            <button
              type="button"
              @click="toggleLive('create')"
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                createForm.allow_live
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="createForm.allow_live ? 'translate-x-6' : 'translate-x-1'"
              />
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiLive.hint") }}
          </p>
        </div>

        <!-- OpenAI Messages 调度配置（仅 openai 平台） -->
        <div
          v-if="createForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.openaiMessages.title") }}
          </h4>

          <!-- 鍏佽 Messages 璋冨害寮€鍏?-->
          <div class="flex items-center justify-between">
            <label class="text-sm text-gray-600 dark:text-gray-400">{{
              t("admin.groups.openaiMessages.allowDispatch")
            }}</label>
            <button
              type="button"
              @click="
                createForm.allow_messages_dispatch =
                  !createForm.allow_messages_dispatch
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                createForm.allow_messages_dispatch
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  createForm.allow_messages_dispatch
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiMessages.allowDispatchHint") }}
          </p>

          <div v-if="createForm.allow_messages_dispatch" class="mt-3">
            <div
              class="relative overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-600 dark:bg-dark-800"
            >
              <div
                class="border-b border-gray-100 bg-gray-50/80 px-4 py-3 dark:border-dark-700 dark:bg-dark-700/50"
              >
                <div class="flex items-center gap-2">
                  <div class="h-2 w-2 rounded-full bg-blue-500"></div>
                  <label
                    class="text-sm font-medium text-gray-900 dark:text-white"
                    >{{
                      t("admin.groups.openaiMessages.familyMappingTitle")
                    }}</label
                  >
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.groups.openaiMessages.familyMappingHint") }}
                </p>
              </div>
              <div class="p-4">
                <div class="grid gap-4 md:grid-cols-3">
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.opusModel")
                    }}</label>
                    <input
                      v-model="createForm.opus_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.opusModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.sonnetModel")
                    }}</label>
                    <input
                      v-model="createForm.sonnet_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.sonnetModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.haikuModel")
                    }}</label>
                    <input
                      v-model="createForm.haiku_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.haikuModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                </div>
              </div>
            </div>

            <div
              class="mt-5 relative overflow-hidden rounded-xl border border-primary-200 bg-white shadow-sm dark:border-primary-900/50 dark:bg-dark-800"
            >
              <div
                class="border-b border-primary-100 bg-primary-50/80 px-4 py-3 dark:border-primary-900/40 dark:bg-primary-900/20"
              >
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <div class="flex items-center gap-2">
                      <div class="h-2 w-2 rounded-full bg-primary-500"></div>
                      <label
                        class="text-sm font-medium text-primary-900 dark:text-primary-100"
                        >{{
                          t("admin.groups.openaiMessages.exactMappingTitle")
                        }}</label
                      >
                    </div>
                    <p
                      class="mt-1 text-xs text-primary-600/90 dark:text-primary-400/90"
                    >
                      {{ t("admin.groups.openaiMessages.exactMappingHint") }}
                    </p>
                  </div>
                </div>
              </div>

              <div class="p-4 bg-gray-50/30 dark:bg-dark-800/30">
                <div
                  v-if="createForm.exact_model_mappings.length === 0"
                  class="flex items-center justify-between gap-3 rounded-xl border-2 border-dashed border-primary-200 bg-white px-5 py-4 text-sm text-primary-700 transition-colors hover:border-primary-300 dark:border-primary-900/40 dark:bg-dark-800 dark:text-primary-300 dark:hover:border-primary-800"
                >
                  <span>{{
                    t("admin.groups.openaiMessages.noExactMappings")
                  }}</span>
                  <button
                    type="button"
                    @click="addCreateMessagesDispatchMapping"
                    class="flex items-center gap-1.5 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                  >
                    <Icon name="plus" size="sm" />
                    {{ t("admin.groups.openaiMessages.addExactMapping") }}
                  </button>
                </div>

                <div v-else class="space-y-3">
                  <div
                    v-for="row in createForm.exact_model_mappings"
                    :key="getCreateMessagesDispatchRowKey(row)"
                    class="group relative rounded-xl border border-gray-200 bg-white p-4 shadow-sm transition-all hover:border-primary-300 hover:shadow-md dark:border-dark-600 dark:bg-dark-700 dark:hover:border-primary-700"
                  >
                    <div class="flex items-center gap-4">
                      <div
                        class="grid flex-1 gap-4 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] md:items-start"
                      >
                        <div>
                          <label class="input-label">{{
                            t("admin.groups.openaiMessages.claudeModel")
                          }}</label>
                          <input
                            v-model="row.claude_model"
                            type="text"
                            :placeholder="
                              t(
                                'admin.groups.openaiMessages.claudeModelPlaceholder',
                              )
                            "
                            class="input bg-gray-50 focus:bg-white dark:bg-dark-800 dark:focus:bg-dark-900"
                          />
                        </div>
                        <div
                          class="hidden md:flex md:justify-center md:pt-7 text-primary-300 dark:text-primary-700"
                        >
                          <Icon
                            name="arrowRight"
                            size="sm"
                            class="transition-transform group-hover:translate-x-1"
                          />
                        </div>
                        <div>
                          <label class="input-label">{{
                            t("admin.groups.openaiMessages.targetModel")
                          }}</label>
                          <input
                            v-model="row.target_model"
                            type="text"
                            :placeholder="
                              t(
                                'admin.groups.openaiMessages.targetModelPlaceholder',
                              )
                            "
                            class="input bg-gray-50 focus:bg-white dark:bg-dark-800 dark:focus:bg-dark-900"
                          />
                        </div>
                      </div>
                      <button
                        type="button"
                        @click="removeCreateMessagesDispatchMapping(row)"
                        class="mt-6 flex h-9 w-9 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                        :title="
                          t('admin.groups.openaiMessages.removeExactMapping')
                        "
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>
                  </div>

                  <button
                    type="button"
                    @click="addCreateMessagesDispatchMapping"
                    class="flex w-full items-center justify-center gap-2 rounded-xl border-2 border-dashed border-gray-300 bg-white py-3 text-sm font-medium text-gray-500 transition-all hover:border-primary-300 hover:bg-primary-50/50 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-400 dark:hover:border-primary-800 dark:hover:bg-primary-900/20 dark:hover:text-primary-400"
                  >
                    <Icon name="plus" size="sm" />
                    {{ t("admin.groups.openaiMessages.addExactMapping") }}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 账号过滤控制 (OpenAI/Antigravity/Anthropic/Gemini) -->
        <div
          v-if="
            ['openai', 'antigravity', 'anthropic', 'gemini'].includes(
              createForm.platform,
            )
          "
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4 space-y-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.accountFilters.title") }}
          </h4>

          <!-- require_oauth_only toggle -->
          <div class="flex items-center justify-between">
            <div>
              <label class="text-sm text-gray-600 dark:text-gray-400"
                >{{ t("admin.groups.accountFilters.oauthOnly") }}</label
              >
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                {{
                  createForm.require_oauth_only
                    ? t("admin.groups.accountFilters.oauthOnlyEnabled")
                    : t("admin.groups.accountFilters.disabled")
                }}
              </p>
            </div>
            <button
              type="button"
              @click="
                createForm.require_oauth_only = !createForm.require_oauth_only
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                createForm.require_oauth_only
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  createForm.require_oauth_only
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>

          <!-- require_privacy_set toggle -->
          <div class="flex items-center justify-between">
            <div>
              <label class="text-sm text-gray-600 dark:text-gray-400"
                >{{ t("admin.groups.accountFilters.privacySetOnly") }}</label
              >
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                {{
                  createForm.require_privacy_set
                    ? t("admin.groups.accountFilters.privacySetOnlyEnabled")
                    : t("admin.groups.accountFilters.disabled")
                }}
              </p>
            </div>
            <button
              type="button"
              @click="
                createForm.require_privacy_set = !createForm.require_privacy_set
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                createForm.require_privacy_set
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  createForm.require_privacy_set
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>
        </div>

        <!-- 鏃犳晥璇锋眰鍏滃簳锛堜粎 anthropic/antigravity 骞冲彴锛屼笖闈炶闃呭垎缁勶級 -->
        <div
          v-if="
            ['anthropic', 'antigravity'].includes(createForm.platform) &&
            createForm.subscription_type !== 'subscription'
          "
          class="border-t pt-4"
        >
          <label class="input-label">{{
            t("admin.groups.invalidRequestFallback.title")
          }}</label>
          <Select
            v-model="createForm.fallback_group_id_on_invalid_request"
            :options="invalidRequestFallbackOptions"
            :placeholder="t('admin.groups.invalidRequestFallback.noFallback')"
          />
          <p class="input-hint">
            {{ t("admin.groups.invalidRequestFallback.hint") }}
          </p>
        </div>

        <!-- 妯″瀷璺敱閰嶇疆锛堜粎 anthropic 骞冲彴锛?-->
        <div v-if="createForm.platform === 'anthropic'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.modelRouting.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-80 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.modelRouting.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <!-- 鍚敤寮€鍏?-->
          <div class="flex items-center gap-3 mb-3">
            <button
              type="button"
              @click="
                createForm.model_routing_enabled =
                  !createForm.model_routing_enabled
              "
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                createForm.model_routing_enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  createForm.model_routing_enabled
                    ? 'translate-x-6'
                    : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                createForm.model_routing_enabled
                  ? t("admin.groups.modelRouting.enabled")
                  : t("admin.groups.modelRouting.disabled")
              }}
            </span>
          </div>
          <p
            v-if="!createForm.model_routing_enabled"
            class="text-xs text-gray-500 dark:text-gray-400 mb-3"
          >
            {{ t("admin.groups.modelRouting.disabledHint") }}
          </p>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t("admin.groups.modelRouting.noRulesHint") }}
          </p>
          <!-- 璺敱瑙勫垯鍒楄〃锛堜粎鍦ㄥ惎鐢ㄦ椂鏄剧ず锛?-->
          <div v-if="createForm.model_routing_enabled" class="space-y-3">
            <div
              v-for="rule in createModelRoutingRules"
              :key="getCreateRuleRenderKey(rule)"
              class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="flex items-start gap-3">
                <div class="flex-1 space-y-2">
                  <div>
                    <label class="input-label text-xs">{{
                      t("admin.groups.modelRouting.modelPattern")
                    }}</label>
                    <input
                      v-model="rule.pattern"
                      type="text"
                      class="input text-sm"
                      :placeholder="
                        t('admin.groups.modelRouting.modelPatternPlaceholder')
                      "
                    />
                  </div>
                  <div>
                    <label class="input-label text-xs">{{
                      t("admin.groups.modelRouting.accounts")
                    }}</label>
                    <!-- 宸查€夎处鍙锋爣绛?-->
                    <div
                      v-if="rule.accounts.length > 0"
                      class="flex flex-wrap gap-1.5 mb-2"
                    >
                      <span
                        v-for="account in rule.accounts"
                        :key="account.id"
                        class="inline-flex items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                      >
                        {{ account.name }}
                        <button
                          type="button"
                          @click="removeSelectedAccount(rule, account.id)"
                          class="ml-0.5 text-primary-500 hover:text-primary-700 dark:hover:text-primary-200"
                        >
                          <Icon name="x" size="xs" />
                        </button>
                      </span>
                    </div>
                    <!-- 璐﹀彿鎼滅储杈撳叆妗?-->
                    <div class="relative account-search-container">
                      <input
                        v-model="
                          accountSearchKeyword[getCreateRuleSearchKey(rule)]
                        "
                        type="text"
                        class="input text-sm"
                        :placeholder="
                          t(
                            'admin.groups.modelRouting.searchAccountPlaceholder',
                          )
                        "
                        @input="searchAccountsByRule(rule)"
                        @focus="onAccountSearchFocus(rule)"
                      />
                      <!-- 鎼滅储缁撴灉涓嬫媺妗?-->
                      <div
                        v-if="
                          showAccountDropdown[getCreateRuleSearchKey(rule)] &&
                          accountSearchResults[getCreateRuleSearchKey(rule)]
                            ?.length > 0
                        "
                        class="absolute z-50 mt-1 max-h-48 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
                      >
                        <button
                          v-for="account in accountSearchResults[
                            getCreateRuleSearchKey(rule)
                          ]"
                          :key="account.id"
                          type="button"
                          @click="selectAccount(rule, account)"
                          class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
                          :class="{
                            'opacity-50': rule.accounts.some(
                              (a) => a.id === account.id,
                            ),
                          }"
                          :disabled="
                            rule.accounts.some((a) => a.id === account.id)
                          "
                        >
                          <span>{{ account.name }}</span>
                          <span class="ml-2 text-xs text-gray-400"
                            >#{{ account.id }}</span
                          >
                        </button>
                      </div>
                    </div>
                    <p class="text-xs text-gray-400 mt-1">
                      {{ t("admin.groups.modelRouting.accountsHint") }}
                    </p>
                  </div>
                </div>
                <button
                  type="button"
                  @click="removeCreateRoutingRule(rule)"
                  class="mt-5 p-1.5 text-gray-400 hover:text-red-500 transition-colors"
                  :title="t('admin.groups.modelRouting.removeRule')"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </div>
          </div>
          <!-- 娣诲姞瑙勫垯鎸夐挳锛堜粎鍦ㄥ惎鐢ㄦ椂鏄剧ず锛?-->
          <button
            v-if="createForm.model_routing_enabled"
            type="button"
            @click="addCreateRoutingRule"
            class="mt-3 flex items-center gap-1.5 text-sm text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
          >
            <Icon name="plus" size="sm" />
            {{ t("admin.groups.modelRouting.addRule") }}
          </button>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3 pt-4">
          <button
            @click="closeCreateModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            type="submit"
            form="create-group-form"
            :disabled="submitting || (createModelsListState.enabled && createModelsListLoading)"
            class="btn btn-primary"
            data-tour="group-form-submit"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ submitting ? t("admin.groups.creating") : t("common.create") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Edit Group Modal -->
    <BaseDialog
      :show="showEditModal"
      :title="t('admin.groups.editGroup')"
      width="normal"
      @close="closeEditModal"
    >
      <form
        v-if="editingGroup"
        id="edit-group-form"
        @submit.prevent="handleUpdateGroup"
        class="space-y-5"
      >
        <div>
          <label class="input-label">{{ t("admin.groups.form.name") }}</label>
          <input
            v-model="editForm.name"
            type="text"
            required
            class="input"
            data-tour="edit-group-form-name"
          />
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.description")
          }}</label>
          <textarea
            v-model="editForm.description"
            rows="3"
            class="input"
          ></textarea>
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.platform")
          }}</label>
          <Select
            v-model="editForm.platform"
            :options="platformOptions"
            :disabled="true"
            data-tour="group-form-platform"
          />
          <p class="input-hint">{{ t("admin.groups.platformNotEditable") }}</p>
        </div>
        <!-- 浠庡垎缁勫鍒惰处鍙凤紙缂栬緫鏃讹級 -->
        <div v-if="copyAccountsGroupOptionsForEdit.length > 0">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.copyAccounts.title") }}
            </label>
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.copyAccounts.tooltipEdit") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <!-- 宸查€夊垎缁勬爣绛?-->
          <div
            v-if="editForm.copy_accounts_from_group_ids.length > 0"
            class="flex flex-wrap gap-1.5 mb-2"
          >
            <span
              v-for="groupId in editForm.copy_accounts_from_group_ids"
              :key="groupId"
              class="inline-flex items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
            >
              {{
                copyAccountsGroupOptionsForEdit.find((o) => o.value === groupId)
                  ?.label || `#${groupId}`
              }}
              <button
                type="button"
                @click="
                  editForm.copy_accounts_from_group_ids =
                    editForm.copy_accounts_from_group_ids.filter(
                      (id) => id !== groupId,
                    )
                "
                class="ml-0.5 text-primary-500 hover:text-primary-700 dark:hover:text-primary-200"
              >
                <Icon name="x" size="xs" />
              </button>
            </span>
          </div>
          <!-- 分组选择下拉 -->
          <select
            class="input"
            @change="
              (e) => {
                const val = Number((e.target as HTMLSelectElement).value);
                if (
                  val &&
                  !editForm.copy_accounts_from_group_ids.includes(val)
                ) {
                  editForm.copy_accounts_from_group_ids.push(val);
                }
                (e.target as HTMLSelectElement).value = '';
              }
            "
          >
            <option value="">
              {{ t("admin.groups.copyAccounts.selectPlaceholder") }}
            </option>
            <option
              v-for="opt in copyAccountsGroupOptionsForEdit"
              :key="opt.value"
              :value="opt.value"
              :disabled="
                editForm.copy_accounts_from_group_ids.includes(opt.value)
              "
            >
              {{ opt.label }}
            </option>
          </select>
          <p class="input-hint">
            {{ t("admin.groups.copyAccounts.hintEdit") }}
          </p>
        </div>
        <div>
          <label class="input-label">{{
            t("admin.groups.form.rateMultiplier")
          }}</label>
          <input
            v-model.number="editForm.rate_multiplier"
            type="number"
            step="0.001"
            min="0"
            required
            class="input"
            data-tour="group-form-multiplier"
          />
        </div>
        <div>
          <label class="input-label">{{ t("admin.groups.form.rpmLimit") }}</label>
          <input
            v-model.number="editForm.rpm_limit"
            type="number"
            min="0"
            step="1"
            class="input"
            :placeholder="t('admin.groups.form.rpmLimitPlaceholder')"
          />
          <p class="input-hint">{{ t("admin.groups.form.rpmLimitHint") }}</p>
        </div>
        <div
          class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-700/40"
        >
          <div class="flex items-center justify-between gap-4">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                启动 New-API 风格接口
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                开启后，该分组下通用网关选中的账号优先走 New-API 风格 adapter；关闭时保留账号级开关和原链路。
              </p>
            </div>
            <button
              type="button"
              @click="
                editForm.newapi_style_interface_enabled =
                  !editForm.newapi_style_interface_enabled
              "
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors',
                editForm.newapi_style_interface_enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editForm.newapi_style_interface_enabled
                    ? 'translate-x-6'
                    : 'translate-x-1',
                ]"
              />
            </button>
          </div>
        </div>
        <ReasoningEffortPolicyFields
          v-if="supportsReasoningEffortPolicyPlatform(editForm.platform)"
          ref="editReasoningEffortPolicyRef"
          id-prefix="edit-group-reasoning"
          :platform="editForm.platform"
          v-model:max-effort="editForm.max_reasoning_effort"
          v-model:mappings="editForm.reasoning_effort_mappings"
        />
        <div v-if="editForm.subscription_type !== 'subscription'">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.form.exclusive") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <!-- Tooltip Popover -->
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="mb-2 text-xs font-medium">
                    {{ t("admin.groups.exclusiveTooltip.title") }}
                  </p>
                  <p class="mb-2 text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.exclusiveTooltip.description") }}
                  </p>
                  <div class="rounded bg-gray-800 p-2 dark:bg-gray-700">
                    <p class="text-xs leading-relaxed text-gray-300">
                      <span
                        class="inline-flex items-center gap-1 text-primary-400"
                        ><Icon name="lightbulb" size="xs" />
                        {{ t("admin.groups.exclusiveTooltip.example") }}</span
                      >
                      {{ t("admin.groups.exclusiveTooltip.exampleContent") }}
                    </p>
                  </div>
                  <!-- Arrow -->
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="editForm.is_exclusive = !editForm.is_exclusive"
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                editForm.is_exclusive
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editForm.is_exclusive ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                editForm.is_exclusive
                  ? t("admin.groups.exclusive")
                  : t("admin.groups.public")
              }}
            </span>
          </div>
        </div>
        <div>
          <label class="input-label">{{ t("admin.groups.form.status") }}</label>
          <Select v-model="editForm.status" :options="editStatusOptions" />
        </div>

        <!-- Subscription Configuration -->
        <div class="mt-4 border-t pt-4">
          <div>
            <label class="input-label">{{
              t("admin.groups.subscription.type")
            }}</label>
            <Select
              v-model="editForm.subscription_type"
              :options="subscriptionTypeOptions"
              :disabled="true"
            />
            <p class="input-hint">
              {{ t("admin.groups.subscription.typeNotEditable") }}
            </p>
          </div>

          <!-- Subscription limits (only show when subscription type is selected) -->
          <div
            v-if="editForm.subscription_type === 'subscription'"
            class="space-y-4 border-l-2 border-primary-200 pl-4 dark:border-primary-800"
          >
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.dailyLimit")
              }}</label>
              <input
                v-model.number="editForm.daily_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.weeklyLimit")
              }}</label>
              <input
                v-model.number="editForm.weekly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div>
              <label class="input-label">{{
                t("admin.groups.subscription.monthlyLimit")
              }}</label>
              <input
                v-model.number="editForm.monthly_limit_usd"
                type="number"
                step="0.01"
                min="0"
                class="input"
                :placeholder="t('admin.groups.subscription.noLimit')"
              />
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.subscription.customLimit")
                }}</label>
                <input
                  v-model.number="editForm.custom_limit_usd"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input"
                  :placeholder="t('admin.groups.subscription.noLimit')"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.subscription.customWindowHours")
                }}</label>
                <input
                  v-model.number="editForm.custom_limit_hours"
                  type="number"
                  step="1"
                  min="0"
                  max="87600"
                  class="input"
                  placeholder="72"
                />
              </div>
            </div>
          </div>
        </div>

        <div class="border-t pt-4">
          <div class="mb-3 flex items-center justify-between gap-3">
            <div>
              <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t("admin.groups.modelsList.title") }}
              </label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelsList.hint") }}
              </p>
            </div>
            <button
              type="button"
              @click="editModelsListState.enabled = !editModelsListState.enabled"
              :class="[
                'relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors',
                editModelsListState.enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editModelsListState.enabled ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
          </div>
          <div
            v-if="editModelsListState.enabled"
            class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50/50 dark:border-dark-600 dark:bg-dark-800/40"
          >
            <div
              v-if="!editModelsListLoading"
              class="flex items-center justify-between gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-600 dark:bg-dark-800"
            >
              <span class="text-gray-500 dark:text-gray-400">
                {{
                  t("admin.groups.modelsList.selectedCount", {
                    selected: editModelsListSelectedCount,
                    total: editModelsListState.items.length,
                  })
                }}
              </span>
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  class="rounded px-2 py-1 font-medium text-primary-600 transition-colors hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                  @click="addModelsListItem(editModelsListState)"
                >
                  {{ t("admin.groups.modelsList.add") }}
                </button>
                <button
                  type="button"
                  :disabled="editModelsListSelectedCount === 0"
                  class="rounded px-2 py-1 font-medium text-red-600 transition-colors hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="removeSelectedModelsListItems(editModelsListState)"
                >
                  {{ t("admin.groups.modelsList.delete") }}
                </button>
              </div>
            </div>
            <div
              class="max-h-64 space-y-2 overflow-y-auto p-2"
            >
              <p v-if="editModelsListLoading" class="text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.modelsList.loading") }}
              </p>
              <p
                v-else-if="editModelsListState.items.length === 0"
                class="text-xs text-gray-500 dark:text-gray-400"
              >
                {{ t("admin.groups.modelsList.empty") }}
              </p>
              <div
                v-for="(item, index) in editModelsListState.items"
                :key="item.uid"
                class="flex items-center gap-2 rounded border border-gray-200 bg-white px-3 py-2 dark:border-dark-600 dark:bg-dark-800"
              >
                <input
                  v-model="item.selected"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <input
                  v-if="item.editing"
                  v-model="item.draft"
                  type="text"
                  class="input min-w-0 flex-1 py-1 text-sm"
                  :placeholder="t('admin.groups.modelsList.modelPlaceholder')"
                  @keydown.enter.prevent="commitModelsListItemEdit(editModelsListState, item)"
                  @keydown.esc.prevent="cancelModelsListItemEdit(editModelsListState, item)"
                  @blur="commitModelsListItemEdit(editModelsListState, item)"
                />
                <span v-else class="min-w-0 flex-1 break-all text-sm text-gray-700 dark:text-gray-300">
                  {{ item.id }}
                </span>
                <button
                  type="button"
                  class="rounded px-2 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
                  @click="startEditModelsListItem(item)"
                >
                  {{ t("admin.groups.modelsList.edit") }}
                </button>
                <button
                  type="button"
                  class="rounded px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  @click="removeModelsListItem(editModelsListState, item)"
                >
                  {{ t("admin.groups.modelsList.delete") }}
                </button>
                <button
                  type="button"
                  :disabled="index === 0"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveEditModelsListItem(index, index - 1)"
                >
                  <Icon name="arrowUp" size="sm" />
                </button>
                <button
                  type="button"
                  :disabled="index === editModelsListState.items.length - 1"
                  class="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 disabled:opacity-40 dark:hover:bg-dark-600 dark:hover:text-gray-200"
                  @click="moveEditModelsListItem(index, index + 1)"
                >
                  <Icon name="arrowDown" size="sm" />
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- 鍥剧墖鐢熸垚璁¤垂閰嶇疆 -->
        <div
          v-if="supportsImagePricingPlatform(editForm.platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(imagePricingI18nKey(editForm.platform, "title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(imagePricingI18nKey(editForm.platform, "description")) }}
          </p>
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.allow_image_generation"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(imagePricingI18nKey(editForm.platform, "allowImageGeneration")) }}
            </label>
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.image_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(imagePricingI18nKey(editForm.platform, "independentMultiplier")) }}
            </label>
          </div>
          <div
            v-if="editForm.image_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(imagePricingI18nKey(editForm.platform, "imageMultiplier"))
            }}</label>
            <input
              v-model.number="editForm.image_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="input-label">1K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="editForm.image_price_1k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(editForm.platform, 'image_price_1k')"
              />
            </div>
            <div>
              <label class="input-label">2K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="editForm.image_price_2k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(editForm.platform, 'image_price_2k')"
              />
            </div>
            <div>
              <label class="input-label">4K ({{ getDisplayCurrencySymbol() }})</label>
              <input
                v-model.number="editForm.image_price_4k"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getImagePricePlaceholder(editForm.platform, 'image_price_4k')"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(imagePricingI18nKey(editForm.platform, "modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(imagePricingI18nKey(editForm.platform, "finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in editImageFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
          <div v-if="editForm.platform === 'gemini' && editForm.allow_image_generation" class="mt-4 border-t border-dashed border-gray-200 pt-4 dark:border-dark-700">
            <label
              class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="editForm.allow_batch_image_generation"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t("admin.groups.imagePricing.allowBatchImageGeneration") }}
            </label>
            <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.groups.imagePricing.batchSectionHint") }}
            </p>
            <div
              v-if="editForm.allow_batch_image_generation"
              class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2"
            >
              <div>
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchDiscountMultiplier")
                }}</label>
                <input
                  v-model.number="editForm.batch_image_discount_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.5"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.imagePricing.batchHoldMultiplier")
                }}</label>
                <input
                  v-model.number="editForm.batch_image_hold_multiplier"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input"
                  placeholder="0.6"
                />
              </div>
            </div>
          </div>
          <p
            v-else-if="editForm.platform !== 'gemini'"
            class="mt-4 border-t border-dashed border-gray-200 pt-4 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400"
          >
            {{ t("admin.groups.imagePricing.batchGeminiOnlyHint") }}
          </p>
        </div>

        <!-- 瑙嗛鐢熸垚璁¤垂閰嶇疆锛堜粎 Grok 骞冲彴锛?-->
        <div
          v-if="supportsVideoPricingPlatform(editForm.platform)"
          class="border-t pt-4"
        >
          <label
            class="block mb-2 font-medium text-gray-700 dark:text-gray-300"
          >
            {{ t(videoPricingI18nKey("title")) }}
          </label>
          <p class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t(videoPricingI18nKey("description")) }}
          </p>
          <div class="mb-4">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.video_rate_independent"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              {{ t(videoPricingI18nKey("independentMultiplier")) }}
            </label>
          </div>
          <div
            v-if="editForm.video_rate_independent"
            class="mb-4"
          >
            <label class="input-label">{{
              t(videoPricingI18nKey("videoMultiplier"))
            }}</label>
            <input
              v-model.number="editForm.video_rate_multiplier"
              type="number"
              step="0.0001"
              min="0"
              class="input"
              placeholder="1"
            />
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="input-label">480p ($/s)</label>
              <input
                v-model.number="editForm.video_price_480p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(editForm.platform, 'video_price_480p')"
              />
            </div>
            <div>
              <label class="input-label">720p ($/s)</label>
              <input
                v-model.number="editForm.video_price_720p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(editForm.platform, 'video_price_720p')"
              />
            </div>
            <div>
              <label class="input-label">1080p ($/s)</label>
              <input
                v-model.number="editForm.video_price_1080p"
                type="number"
                step="0.001"
                min="0"
                class="input"
                :placeholder="getVideoPricePlaceholder(editForm.platform, 'video_price_1080p')"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ t(videoPricingI18nKey("modeHint")) }}
          </p>
          <div class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-700 dark:bg-gray-800 dark:text-gray-300">
            <div class="mb-1 font-medium">
              {{ t(videoPricingI18nKey("finalPricePreview")) }}
            </div>
            <div class="grid grid-cols-3 gap-2">
              <div
                v-for="item in editVideoFinalPricePreview"
                :key="item.label"
              >
                {{ item.label }}: {{ item.value }}
              </div>
            </div>
          </div>
        </div>

        <!-- 楂樺嘲鏃舵鍊嶇巼閰嶇疆 -->
        <div class="border-t pt-4">
          <div class="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="editForm.peak_rate_enabled"
                type="checkbox"
                class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
              />
              <span>{{ t("admin.groups.peakRate.enable") }}</span>
            </label>
          </div>
          <div
            v-if="editForm.peak_rate_enabled"
            class="mb-4 space-y-3"
          >
            <div
              v-for="(window, index) in editForm.peak_rate_windows"
              :key="`edit-peak-${index}`"
              class="grid grid-cols-1 gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600 md:grid-cols-[1fr_1fr_1fr_auto]"
            >
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakStart") }}</label>
                <input v-model="window.start" type="time" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakEnd") }}</label>
                <input v-model="window.end" type="time" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t("admin.groups.peakRate.peakMultiplier") }}</label>
                <input
                  v-model.number="window.multiplier"
                  type="number"
                  step="0.001"
                  min="0"
                  class="input"
                  placeholder="1"
                  :title="t('admin.groups.peakRate.multiplierHint')"
                />
              </div>
              <button
                type="button"
                class="btn btn-secondary self-end"
                @click="removePeakRateWindow(editForm.peak_rate_windows, index)"
              >
                {{ t("common.delete") }}
              </button>
            </div>
            <button
              type="button"
              class="btn btn-secondary"
              @click="addPeakRateWindow(editForm.peak_rate_windows)"
            >
              + {{ t("admin.groups.peakRate.addWindow") }}
            </button>
          </div>
        </div>

        <!-- 鏀寔鐨勬ā鍨嬬郴鍒楋紙浠?antigravity 骞冲彴锛?-->
        <!-- 分组利润控制（五个平台 token 请求） -->
        <div v-if="isProfitControlPlatform(editForm.platform)" class="border-t pt-4">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input
              v-model="editForm.profit_control_enabled"
              type="checkbox"
              class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <span>{{ t("admin.groups.profitControl.enable") }}</span>
          </label>
          <p class="mb-3 mt-1.5 text-xs text-gray-500 dark:text-gray-400">
            {{
              editForm.profit_control_enabled
                ? t("admin.groups.profitControl.enabledHint")
                : t("admin.groups.profitControl.disabledHint")
            }}
          </p>
          <div
            v-if="editForm.profit_control_enabled"
            class="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2"
          >
            <div>
              <label class="input-label">{{ t("admin.groups.profitControl.minMargin") }}</label>
              <input
                v-model.number="editForm.profit_min_margin_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.minMarginHint')"
              />
            </div>
            <div>
              <label class="input-label">{{ t("admin.groups.profitControl.safetyBuffer") }}</label>
              <input
                v-model.number="editForm.profit_safety_buffer_percent"
                type="number"
                step="0.1"
                min="0"
                max="99.99"
                class="input"
                placeholder="0"
                :title="t('admin.groups.profitControl.safetyBufferHint')"
              />
            </div>
          </div>
        </div>

        <!-- 支持的模型系列（仅 antigravity 平台） -->
        <div v-if="editForm.platform === 'antigravity'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.supportedScopes.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.supportedScopes.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="space-y-2">
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="editForm.supported_model_scopes.includes('claude')"
                @change="toggleEditScope('claude')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.claude")
              }}</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="
                  editForm.supported_model_scopes.includes('gemini_text')
                "
                @change="toggleEditScope('gemini_text')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.geminiText")
              }}</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                :checked="
                  editForm.supported_model_scopes.includes('gemini_image')
                "
                @change="toggleEditScope('gemini_image')"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
              />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{
                t("admin.groups.supportedScopes.geminiImage")
              }}</span>
            </label>
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.groups.supportedScopes.hint") }}
          </p>
        </div>

        <!-- MCP XML 鍗忚娉ㄥ叆锛堜粎 antigravity 骞冲彴锛?-->
        <div v-if="editForm.platform === 'antigravity'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.mcpXml.title") }}
            </label>
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.mcpXml.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="editForm.mcp_xml_inject = !editForm.mcp_xml_inject"
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                editForm.mcp_xml_inject
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editForm.mcp_xml_inject ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                editForm.mcp_xml_inject
                  ? t("admin.groups.mcpXml.enabled")
                  : t("admin.groups.mcpXml.disabled")
              }}
            </span>
          </div>
        </div>

        <!-- Claude Code 瀹㈡埛绔檺鍒讹紙浠?anthropic 骞冲彴锛?-->
        <div v-if="editForm.platform === 'anthropic'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.claudeCode.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-72 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.claudeCode.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-3">
            <button
              type="button"
              @click="editForm.claude_code_only = !editForm.claude_code_only"
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                editForm.claude_code_only
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editForm.claude_code_only ? 'translate-x-6' : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                editForm.claude_code_only
                  ? t("admin.groups.claudeCode.enabled")
                  : t("admin.groups.claudeCode.disabled")
              }}
            </span>
          </div>
          <!-- 闄嶇骇鍒嗙粍閫夋嫨锛堜粎褰撳惎鐢?claude_code_only 鏃舵樉绀猴級 -->
          <div v-if="editForm.claude_code_only" class="mt-3">
            <label class="input-label">{{
              t("admin.groups.claudeCode.fallbackGroup")
            }}</label>
            <Select
              v-model="editForm.fallback_group_id"
              :options="fallbackGroupOptionsForEdit"
              :placeholder="t('admin.groups.claudeCode.noFallback')"
            />
            <p class="input-hint">
              {{ t("admin.groups.claudeCode.fallbackHint") }}
            </p>
          </div>
        </div>

        <!-- OpenAI Messages 璋冨害閰嶇疆锛堜粎 openai 骞冲彴锛?-->
        <!-- Codex 网页搜索按次计费（仅 openai 平台） -->
        <div
          v-if="editForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.webSearchPricing.title") }}
          </h4>
          <div>
            <label class="input-label">{{
              t("admin.groups.webSearchPricing.pricePerCall")
            }}</label>
            <input
              v-model.number="editForm.web_search_price_per_call"
              type="number"
              step="0.001"
              min="0"
              placeholder="0.01"
              class="input"
            />
            <p class="input-hint">
              {{ t("admin.groups.webSearchPricing.pricePerCallHint") }}
            </p>
            <div
              class="mt-2 rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300"
            >
              {{
                t("admin.groups.webSearchPricing.finalPricePreview", {
                  price: editWebSearchFinalPricePreview,
                })
              }}
            </div>
          </div>
        </div>

        <!-- OpenAI Live 开关（仅 openai 平台） -->
        <div
          v-if="editForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.openaiLive.title") }}
          </h4>
          <div class="flex items-center justify-between">
            <label class="text-sm text-gray-600 dark:text-gray-400">{{
              t("admin.groups.openaiLive.allow")
            }}</label>
            <button
              type="button"
              @click="toggleLive('edit')"
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                editForm.allow_live
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="editForm.allow_live ? 'translate-x-6' : 'translate-x-1'"
              />
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiLive.hint") }}
          </p>
        </div>

        <!-- OpenAI Messages 调度配置（仅 openai 平台） -->
        <div
          v-if="editForm.platform === 'openai'"
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.openaiMessages.title") }}
          </h4>

          <!-- 鍏佽 Messages 璋冨害寮€鍏?-->
          <div class="flex items-center justify-between">
            <label class="text-sm text-gray-600 dark:text-gray-400">{{
              t("admin.groups.openaiMessages.allowDispatch")
            }}</label>
            <button
              type="button"
              @click="
                editForm.allow_messages_dispatch =
                  !editForm.allow_messages_dispatch
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                editForm.allow_messages_dispatch
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  editForm.allow_messages_dispatch
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
            {{ t("admin.groups.openaiMessages.allowDispatchHint") }}
          </p>

          <div v-if="editForm.allow_messages_dispatch" class="mt-3">
            <div
              class="relative overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-600 dark:bg-dark-800"
            >
              <div
                class="border-b border-gray-100 bg-gray-50/80 px-4 py-3 dark:border-dark-700 dark:bg-dark-700/50"
              >
                <div class="flex items-center gap-2">
                  <div class="h-2 w-2 rounded-full bg-blue-500"></div>
                  <label
                    class="text-sm font-medium text-gray-900 dark:text-white"
                    >{{
                      t("admin.groups.openaiMessages.familyMappingTitle")
                    }}</label
                  >
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ t("admin.groups.openaiMessages.familyMappingHint") }}
                </p>
              </div>
              <div class="p-4">
                <div class="grid gap-4 md:grid-cols-3">
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.opusModel")
                    }}</label>
                    <input
                      v-model="editForm.opus_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.opusModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.sonnetModel")
                    }}</label>
                    <input
                      v-model="editForm.sonnet_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.sonnetModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                  <div>
                    <label class="input-label">{{
                      t("admin.groups.openaiMessages.haikuModel")
                    }}</label>
                    <input
                      v-model="editForm.haiku_mapped_model"
                      type="text"
                      :placeholder="
                        t('admin.groups.openaiMessages.haikuModelPlaceholder')
                      "
                      class="input"
                    />
                  </div>
                </div>
              </div>
            </div>

            <div
              class="mt-5 relative overflow-hidden rounded-xl border border-primary-200 bg-white shadow-sm dark:border-primary-900/50 dark:bg-dark-800"
            >
              <div
                class="border-b border-primary-100 bg-primary-50/80 px-4 py-3 dark:border-primary-900/40 dark:bg-primary-900/20"
              >
                <div class="flex items-start justify-between gap-3">
                  <div>
                    <div class="flex items-center gap-2">
                      <div class="h-2 w-2 rounded-full bg-primary-500"></div>
                      <label
                        class="text-sm font-medium text-primary-900 dark:text-primary-100"
                        >{{
                          t("admin.groups.openaiMessages.exactMappingTitle")
                        }}</label
                      >
                    </div>
                    <p
                      class="mt-1 text-xs text-primary-600/90 dark:text-primary-400/90"
                    >
                      {{ t("admin.groups.openaiMessages.exactMappingHint") }}
                    </p>
                  </div>
                </div>
              </div>

              <div class="p-4 bg-gray-50/30 dark:bg-dark-800/30">
                <div
                  v-if="editForm.exact_model_mappings.length === 0"
                  class="flex items-center justify-between gap-3 rounded-xl border-2 border-dashed border-primary-200 bg-white px-5 py-4 text-sm text-primary-700 transition-colors hover:border-primary-300 dark:border-primary-900/40 dark:bg-dark-800 dark:text-primary-300 dark:hover:border-primary-800"
                >
                  <span>{{
                    t("admin.groups.openaiMessages.noExactMappings")
                  }}</span>
                  <button
                    type="button"
                    @click="addEditMessagesDispatchMapping"
                    class="flex items-center gap-1.5 text-sm font-medium text-primary-600 transition-colors hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                  >
                    <Icon name="plus" size="sm" />
                    {{ t("admin.groups.openaiMessages.addExactMapping") }}
                  </button>
                </div>

                <div v-else class="space-y-3">
                  <div
                    v-for="row in editForm.exact_model_mappings"
                    :key="getEditMessagesDispatchRowKey(row)"
                    class="group relative rounded-xl border border-gray-200 bg-white p-4 shadow-sm transition-all hover:border-primary-300 hover:shadow-md dark:border-dark-600 dark:bg-dark-700 dark:hover:border-primary-700"
                  >
                    <div class="flex items-center gap-4">
                      <div
                        class="grid flex-1 gap-4 md:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] md:items-start"
                      >
                        <div>
                          <label class="input-label">{{
                            t("admin.groups.openaiMessages.claudeModel")
                          }}</label>
                          <input
                            v-model="row.claude_model"
                            type="text"
                            :placeholder="
                              t(
                                'admin.groups.openaiMessages.claudeModelPlaceholder',
                              )
                            "
                            class="input bg-gray-50 focus:bg-white dark:bg-dark-800 dark:focus:bg-dark-900"
                          />
                        </div>
                        <div
                          class="hidden md:flex md:justify-center md:pt-7 text-primary-300 dark:text-primary-700"
                        >
                          <Icon
                            name="arrowRight"
                            size="sm"
                            class="transition-transform group-hover:translate-x-1"
                          />
                        </div>
                        <div>
                          <label class="input-label">{{
                            t("admin.groups.openaiMessages.targetModel")
                          }}</label>
                          <input
                            v-model="row.target_model"
                            type="text"
                            :placeholder="
                              t(
                                'admin.groups.openaiMessages.targetModelPlaceholder',
                              )
                            "
                            class="input bg-gray-50 focus:bg-white dark:bg-dark-800 dark:focus:bg-dark-900"
                          />
                        </div>
                      </div>
                      <button
                        type="button"
                        @click="removeEditMessagesDispatchMapping(row)"
                        class="mt-6 flex h-9 w-9 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                        :title="
                          t('admin.groups.openaiMessages.removeExactMapping')
                        "
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>
                  </div>

                  <button
                    type="button"
                    @click="addEditMessagesDispatchMapping"
                    class="flex w-full items-center justify-center gap-2 rounded-xl border-2 border-dashed border-gray-300 bg-white py-3 text-sm font-medium text-gray-500 transition-all hover:border-primary-300 hover:bg-primary-50/50 hover:text-primary-600 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-400 dark:hover:border-primary-800 dark:hover:bg-primary-900/20 dark:hover:text-primary-400"
                  >
                    <Icon name="plus" size="sm" />
                    {{ t("admin.groups.openaiMessages.addExactMapping") }}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 账号过滤控制 (OpenAI/Antigravity/Anthropic/Gemini) -->
        <div
          v-if="
            ['openai', 'antigravity', 'anthropic', 'gemini'].includes(
              editForm.platform,
            )
          "
          class="border-t border-gray-200 dark:border-dark-400 pt-4 mt-4 space-y-4"
        >
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
            {{ t("admin.groups.accountFilters.title") }}
          </h4>

          <!-- require_oauth_only toggle -->
          <div class="flex items-center justify-between">
            <div>
              <label class="text-sm text-gray-600 dark:text-gray-400"
                >{{ t("admin.groups.accountFilters.oauthOnly") }}</label
              >
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                {{
                  editForm.require_oauth_only
                    ? t("admin.groups.accountFilters.oauthOnlyEnabled")
                    : t("admin.groups.accountFilters.disabled")
                }}
              </p>
            </div>
            <button
              type="button"
              @click="
                editForm.require_oauth_only = !editForm.require_oauth_only
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                editForm.require_oauth_only
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  editForm.require_oauth_only
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>

          <!-- require_privacy_set toggle -->
          <div class="flex items-center justify-between">
            <div>
              <label class="text-sm text-gray-600 dark:text-gray-400"
                >{{ t("admin.groups.accountFilters.privacySetOnly") }}</label
              >
              <p class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                {{
                  editForm.require_privacy_set
                    ? t("admin.groups.accountFilters.privacySetOnlyEnabled")
                    : t("admin.groups.accountFilters.disabled")
                }}
              </p>
            </div>
            <button
              type="button"
              @click="
                editForm.require_privacy_set = !editForm.require_privacy_set
              "
              class="relative inline-flex h-6 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
              :class="
                editForm.require_privacy_set
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600'
              "
            >
              <span
                class="pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="
                  editForm.require_privacy_set
                    ? 'translate-x-6'
                    : 'translate-x-1'
                "
              />
            </button>
          </div>
        </div>

        <!-- 鏃犳晥璇锋眰鍏滃簳锛堜粎 anthropic/antigravity 骞冲彴锛屼笖闈炶闃呭垎缁勶級 -->
        <div
          v-if="
            ['anthropic', 'antigravity'].includes(editForm.platform) &&
            editForm.subscription_type !== 'subscription'
          "
          class="border-t pt-4"
        >
          <label class="input-label">{{
            t("admin.groups.invalidRequestFallback.title")
          }}</label>
          <Select
            v-model="editForm.fallback_group_id_on_invalid_request"
            :options="invalidRequestFallbackOptionsForEdit"
            :placeholder="t('admin.groups.invalidRequestFallback.noFallback')"
          />
          <p class="input-hint">
            {{ t("admin.groups.invalidRequestFallback.hint") }}
          </p>
        </div>

        <!-- 妯″瀷璺敱閰嶇疆锛堜粎 anthropic 骞冲彴锛?-->
        <div v-if="editForm.platform === 'anthropic'" class="border-t pt-4">
          <div class="mb-1.5 flex items-center gap-1">
            <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
              {{ t("admin.groups.modelRouting.title") }}
            </label>
            <!-- Help Tooltip -->
            <div class="group relative inline-flex">
              <Icon
                name="questionCircle"
                size="sm"
                :stroke-width="2"
                class="cursor-help text-gray-400 transition-colors hover:text-primary-500 dark:text-gray-500 dark:hover:text-primary-400"
              />
              <div
                class="pointer-events-none absolute bottom-full left-0 z-50 mb-2 w-80 opacity-0 transition-all duration-200 group-hover:pointer-events-auto group-hover:opacity-100"
              >
                <div
                  class="rounded-lg bg-gray-900 p-3 text-white shadow-lg dark:bg-gray-800"
                >
                  <p class="text-xs leading-relaxed text-gray-300">
                    {{ t("admin.groups.modelRouting.tooltip") }}
                  </p>
                  <div
                    class="absolute -bottom-1.5 left-3 h-3 w-3 rotate-45 bg-gray-900 dark:bg-gray-800"
                  ></div>
                </div>
              </div>
            </div>
          </div>
          <!-- 鍚敤寮€鍏?-->
          <div class="flex items-center gap-3 mb-3">
            <button
              type="button"
              @click="
                editForm.model_routing_enabled = !editForm.model_routing_enabled
              "
              :class="[
                'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                editForm.model_routing_enabled
                  ? 'bg-primary-500'
                  : 'bg-gray-300 dark:bg-dark-600',
              ]"
            >
              <span
                :class="[
                  'inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform',
                  editForm.model_routing_enabled
                    ? 'translate-x-6'
                    : 'translate-x-1',
                ]"
              />
            </button>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{
                editForm.model_routing_enabled
                  ? t("admin.groups.modelRouting.enabled")
                  : t("admin.groups.modelRouting.disabled")
              }}
            </span>
          </div>
          <p
            v-if="!editForm.model_routing_enabled"
            class="text-xs text-gray-500 dark:text-gray-400 mb-3"
          >
            {{ t("admin.groups.modelRouting.disabledHint") }}
          </p>
          <p v-else class="text-xs text-gray-500 dark:text-gray-400 mb-3">
            {{ t("admin.groups.modelRouting.noRulesHint") }}
          </p>
          <!-- 璺敱瑙勫垯鍒楄〃锛堜粎鍦ㄥ惎鐢ㄦ椂鏄剧ず锛?-->
          <div v-if="editForm.model_routing_enabled" class="space-y-3">
            <div
              v-for="rule in editModelRoutingRules"
              :key="getEditRuleRenderKey(rule)"
              class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="flex items-start gap-3">
                <div class="flex-1 space-y-2">
                  <div>
                    <label class="input-label text-xs">{{
                      t("admin.groups.modelRouting.modelPattern")
                    }}</label>
                    <input
                      v-model="rule.pattern"
                      type="text"
                      class="input text-sm"
                      :placeholder="
                        t('admin.groups.modelRouting.modelPatternPlaceholder')
                      "
                    />
                  </div>
                  <div>
                    <label class="input-label text-xs">{{
                      t("admin.groups.modelRouting.accounts")
                    }}</label>
                    <!-- 宸查€夎处鍙锋爣绛?-->
                    <div
                      v-if="rule.accounts.length > 0"
                      class="flex flex-wrap gap-1.5 mb-2"
                    >
                      <span
                        v-for="account in rule.accounts"
                        :key="account.id"
                        class="inline-flex items-center gap-1 rounded-full bg-primary-100 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                      >
                        {{ account.name }}
                        <button
                          type="button"
                          @click="removeSelectedAccount(rule, account.id, true)"
                          class="ml-0.5 text-primary-500 hover:text-primary-700 dark:hover:text-primary-200"
                        >
                          <Icon name="x" size="xs" />
                        </button>
                      </span>
                    </div>
                    <!-- 璐﹀彿鎼滅储杈撳叆妗?-->
                    <div class="relative account-search-container">
                      <input
                        v-model="
                          accountSearchKeyword[getEditRuleSearchKey(rule)]
                        "
                        type="text"
                        class="input text-sm"
                        :placeholder="
                          t(
                            'admin.groups.modelRouting.searchAccountPlaceholder',
                          )
                        "
                        @input="searchAccountsByRule(rule, true)"
                        @focus="onAccountSearchFocus(rule, true)"
                      />
                      <!-- 鎼滅储缁撴灉涓嬫媺妗?-->
                      <div
                        v-if="
                          showAccountDropdown[getEditRuleSearchKey(rule)] &&
                          accountSearchResults[getEditRuleSearchKey(rule)]
                            ?.length > 0
                        "
                        class="absolute z-50 mt-1 max-h-48 w-full overflow-auto rounded-lg border bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800"
                      >
                        <button
                          v-for="account in accountSearchResults[
                            getEditRuleSearchKey(rule)
                          ]"
                          :key="account.id"
                          type="button"
                          @click="selectAccount(rule, account, true)"
                          class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
                          :class="{
                            'opacity-50': rule.accounts.some(
                              (a) => a.id === account.id,
                            ),
                          }"
                          :disabled="
                            rule.accounts.some((a) => a.id === account.id)
                          "
                        >
                          <span>{{ account.name }}</span>
                          <span class="ml-2 text-xs text-gray-400"
                            >#{{ account.id }}</span
                          >
                        </button>
                      </div>
                    </div>
                    <p class="text-xs text-gray-400 mt-1">
                      {{ t("admin.groups.modelRouting.accountsHint") }}
                    </p>
                  </div>
                </div>
                <button
                  type="button"
                  @click="removeEditRoutingRule(rule)"
                  class="mt-5 p-1.5 text-gray-400 hover:text-red-500 transition-colors"
                  :title="t('admin.groups.modelRouting.removeRule')"
                >
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </div>
          </div>
          <!-- 娣诲姞瑙勫垯鎸夐挳锛堜粎鍦ㄥ惎鐢ㄦ椂鏄剧ず锛?-->
          <button
            v-if="editForm.model_routing_enabled"
            type="button"
            @click="addEditRoutingRule"
            class="mt-3 flex items-center gap-1.5 text-sm text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
          >
            <Icon name="plus" size="sm" />
            {{ t("admin.groups.modelRouting.addRule") }}
          </button>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3 pt-4">
          <button
            @click="closeEditModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            type="submit"
            form="edit-group-form"
            :disabled="submitting || (editModelsListState.enabled && editModelsListLoading)"
            class="btn btn-primary"
            data-tour="group-form-submit"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ submitting ? t("admin.groups.updating") : t("common.update") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.groups.deleteGroup')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />

    <ConfirmDialog
      :show="showUnsupportedLiveConfirm"
      :title="t('admin.groups.openaiLive.unsupportedTitle')"
      :message="t('admin.groups.openaiLive.unsupportedMessage')"
      :confirm-text="t('admin.groups.openaiLive.acknowledge')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="acknowledgeUnsupportedLive"
      @cancel="cancelUnsupportedLive"
    />

    <!-- Sort Order Modal -->
    <BaseDialog
      :show="showSortModal"
      :title="t('admin.groups.sortOrder')"
      width="normal"
      @close="closeSortModal"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.sortOrderHint") }}
        </p>
        <VueDraggable
          v-model="sortableGroups"
          :animation="200"
          class="space-y-2"
        >
          <div
            v-for="group in sortableGroups"
            :key="group.id"
            class="flex cursor-grab items-center gap-3 rounded-lg border border-gray-200 bg-white p-3 transition-shadow hover:shadow-md active:cursor-grabbing dark:border-dark-600 dark:bg-dark-700"
          >
            <div class="text-gray-400">
              <Icon name="menu" size="md" />
            </div>
            <div class="flex-1">
              <div class="font-medium text-gray-900 dark:text-white">
                {{ group.name }}
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                <span
                  :class="[
                    'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
                    group.platform === 'anthropic'
                      ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
                      : group.platform === 'openai'
                        ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                    : group.platform === 'antigravity'
                      ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
                      : group.platform === 'grok'
                        ? 'bg-zinc-200 text-zinc-800 dark:bg-zinc-700 dark:text-zinc-100'
                          : group.platform === 'zhipu'
                            ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
                            : group.platform === 'deepseek'
                              ? 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
                              : group.platform === 'volcengine'
                                ? 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-400'
                                : group.platform === 'ali'
                                  ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
                                  : group.platform === 'moonshot'
                                    ? 'bg-fuchsia-100 text-fuchsia-700 dark:bg-fuchsia-900/30 dark:text-fuchsia-400'
                          : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
                  ]"
                >
                  {{ t("admin.groups.platforms." + group.platform) }}
                </span>
              </div>
            </div>
            <div class="text-sm text-gray-400">#{{ group.id }}</div>
          </div>
        </VueDraggable>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3 pt-4">
          <button
            @click="closeSortModal"
            type="button"
            class="btn btn-secondary"
          >
            {{ t("common.cancel") }}
          </button>
          <button
            @click="saveSortOrder"
            :disabled="sortSubmitting"
            class="btn btn-primary"
          >
            <svg
              v-if="sortSubmitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ sortSubmitting ? t("common.saving") : t("common.save") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Composite Routes Modal -->
    <BaseDialog
      :show="showCompositeRoutesModal"
      :title="
        compositeRoutesGroup
          ? t('admin.groups.compositeRoutes.titleWithGroup', {
              name: compositeRoutesGroup.name,
            })
          : t('admin.groups.compositeRoutes.title')
      "
      width="wide"
      @close="closeCompositeRoutesModal"
    >
      <div class="grid gap-5 lg:grid-cols-[minmax(0,1.15fr)_minmax(320px,0.85fr)]">
        <section class="min-w-0">
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t("admin.groups.compositeRoutes.routes") }}
            </h3>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="compositeRoutesLoading"
              @click="loadCompositeRoutes"
            >
              <Icon
                name="refresh"
                size="sm"
                :class="compositeRoutesLoading ? 'animate-spin' : ''"
              />
            </button>
          </div>

          <div
            class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600"
          >
            <div
              v-if="compositeRoutesLoading"
              class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
            >
              {{ t("common.loading") }}
            </div>
            <div
              v-else-if="compositeRoutes.length === 0"
              class="flex h-36 items-center justify-center text-sm text-gray-500 dark:text-gray-400"
            >
              {{ t("admin.groups.compositeRoutes.empty") }}
            </div>
            <div v-else class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-600">
                <thead class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
                  <tr>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.publicModel") }}
                    </th>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.target") }}
                    </th>
                    <th class="px-3 py-2">
                      {{ t("admin.groups.compositeRoutes.scope") }}
                    </th>
                    <th class="px-3 py-2 text-right">
                      {{ t("admin.groups.columns.actions") }}
                    </th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                  <tr
                    v-for="route in compositeRoutes"
                    :key="route.id"
                    :class="!route.enabled && 'opacity-60'"
                  >
                    <td class="max-w-[15rem] px-3 py-2">
                      <div class="break-all font-medium text-gray-900 dark:text-white">
                        {{ route.public_model }}
                      </div>
                      <div class="mt-1 flex flex-wrap items-center gap-1.5">
                        <span class="badge badge-gray">{{
                          compositeRouteMatchLabel(route.match_type)
                        }}</span>
                        <span
                          v-if="!route.enabled"
                          class="badge badge-danger"
                        >
                          {{ t("admin.accounts.status.inactive") }}
                        </span>
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="flex items-center gap-1.5 text-gray-900 dark:text-white">
                        <PlatformIcon :platform="route.target_platform" size="xs" />
                        <span>{{ formatCompositePlatform(route.target_platform) }}</span>
                      </div>
                      <div class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">
                        {{ route.upstream_model || route.public_model }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="text-gray-700 dark:text-gray-300">
                        {{ formatCompositeEndpoint(route.endpoint) }}
                      </div>
                      <div class="text-xs text-gray-500 dark:text-gray-400">
                        {{ t("admin.groups.compositeRoutes.priority") }}:
                        {{ route.priority }}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="flex justify-end gap-1">
                        <button
                          type="button"
                          class="rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                          :title="t('common.edit')"
                          @click="editCompositeRoute(route)"
                        >
                          <Icon name="edit" size="sm" />
                        </button>
                        <button
                          type="button"
                          class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                          :title="t('common.delete')"
                          @click="deleteCompositeRoute(route)"
                        >
                          <Icon name="trash" size="sm" />
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </section>

        <section class="space-y-5">
          <form class="space-y-3" @submit.prevent="saveCompositeRoute">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
                {{
                  compositeRouteEditingId
                    ? t("admin.groups.compositeRoutes.editRoute")
                    : t("admin.groups.compositeRoutes.addRoute")
                }}
              </h3>
              <button
                v-if="compositeRouteEditingId"
                type="button"
                class="text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                @click="resetCompositeRouteForm"
              >
                {{ t("common.cancel") }}
              </button>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.publicModel")
              }}</label>
              <input
                v-model.trim="compositeRouteForm.public_model"
                type="text"
                class="input"
                required
                placeholder="openrouter/gpt-5"
              />
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.matchType")
                }}</label>
                <Select
                  v-model="compositeRouteForm.match_type"
                  :options="compositeRouteMatchOptions"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.endpoint")
                }}</label>
                <Select
                  v-model="compositeRouteForm.endpoint"
                  :options="compositeRouteEndpointOptions"
                />
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.targetPlatform")
                }}</label>
                <Select
                  v-model="compositeRouteForm.target_platform"
                  :options="compositeRoutePlatformOptions"
                />
              </div>
              <div>
                <label class="input-label">{{
                  t("admin.groups.compositeRoutes.priority")
                }}</label>
                <input
                  v-model.number="compositeRouteForm.priority"
                  type="number"
                  min="1"
                  step="1"
                  class="input"
                />
              </div>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.upstreamModel")
              }}</label>
              <input
                v-model.trim="compositeRouteForm.upstream_model"
                type="text"
                class="input"
                placeholder="gpt-5"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.groups.compositeRoutes.upstreamModelHint") }}
              </p>
            </div>

            <div>
              <label class="input-label">{{
                t("admin.groups.compositeRoutes.notes")
              }}</label>
              <textarea
                v-model.trim="compositeRouteForm.notes"
                rows="2"
                class="input"
              ></textarea>
            </div>

            <div class="flex items-center justify-between gap-3">
              <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input
                  v-model="compositeRouteForm.enabled"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
                />
                {{ t("admin.groups.compositeRoutes.enabled") }}
              </label>
              <button
                type="submit"
                class="btn btn-primary"
                :disabled="compositeRouteSaving"
              >
                <Icon
                  v-if="!compositeRouteSaving"
                  name="check"
                  size="sm"
                  class="mr-2"
                />
                {{ compositeRouteEditingId ? t("common.update") : t("common.create") }}
              </button>
            </div>
          </form>

          <div class="border-t border-gray-200 pt-4 dark:border-dark-600">
            <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-white">
              {{ t("admin.groups.compositeRoutes.preview") }}
            </h3>
            <div class="space-y-3">
              <input
                v-model.trim="compositePreviewModel"
                type="text"
                class="input"
                placeholder="openrouter/gpt-5"
                @keyup.enter="previewCompositeRoute"
              />
              <div class="flex gap-2">
                <Select
                  v-model="compositePreviewEndpoint"
                  :options="compositeRouteEndpointOptions"
                  class="min-w-0 flex-1"
                />
                <button
                  type="button"
                  class="btn btn-secondary"
                  :disabled="compositePreviewLoading || !compositePreviewModel"
                  @click="previewCompositeRoute"
                >
                  <Icon name="play" size="sm" />
                </button>
              </div>

              <div
                v-if="compositePreviewDecision"
                class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-600 dark:bg-dark-800"
              >
                <div class="mb-2 flex items-center gap-2">
                  <span
                    :class="[
                      'badge',
                      compositePreviewDecision.matched
                        ? 'badge-success'
                        : 'badge-danger',
                    ]"
                  >
                    {{
                      compositePreviewDecision.matched
                        ? t("admin.groups.compositeRoutes.matched")
                        : t("admin.groups.compositeRoutes.notMatched")
                    }}
                  </span>
                  <span class="badge badge-gray">
                    {{
                      compositeRouteSourceLabel(
                        compositePreviewDecision.source,
                      )
                    }}
                  </span>
                </div>
                <div
                  v-if="compositePreviewDecision.matched"
                  class="space-y-1 text-gray-700 dark:text-gray-300"
                >
                  <div>
                    {{ t("admin.groups.compositeRoutes.targetPlatform") }}:
                    {{
                      formatCompositePlatform(
                        compositePreviewDecision.target_platform,
                      )
                    }}
                  </div>
                  <div class="break-all">
                    {{ t("admin.groups.compositeRoutes.upstreamModel") }}:
                    {{ compositePreviewDecision.upstream_model }}
                  </div>
                </div>
                <div
                  v-else
                  class="text-gray-500 dark:text-gray-400"
                >
                  {{ compositePreviewDecision.reason }}
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>

      <template #footer>
        <div class="flex justify-end pt-4">
          <button
            type="button"
            class="btn btn-secondary"
            @click="closeCompositeRoutesModal"
          >
            {{ t("common.close") }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Group Rate Multipliers Modal -->
    <GroupRateMultipliersModal
      :show="showRateMultipliersModal"
      :group="rateMultipliersGroup"
      @close="showRateMultipliersModal = false"
      @success="loadGroups"
    />

    <!-- Group RPM Overrides Modal -->
    <GroupRPMOverridesModal
      :show="showRPMOverridesModal"
      :group="rpmOverridesGroup"
      @close="showRPMOverridesModal = false"
      @success="loadGroups"
    />
    <Teleport to="body">
      <Transition
        enter-active-class="transition duration-150 ease-out"
        enter-from-class="opacity-0 scale-95"
        enter-to-class="opacity-100 scale-100"
        leave-active-class="transition duration-100 ease-in"
        leave-from-class="opacity-100 scale-100"
        leave-to-class="opacity-0 scale-95"
      >
        <div
          v-if="peakRateTooltip"
          class="pointer-events-none fixed z-[9999] rounded-xl border border-amber-200 bg-white px-3 py-2.5 text-left shadow-[0_24px_70px_-28px_rgba(120,53,15,0.55)] ring-1 ring-amber-100 dark:border-amber-800/70 dark:bg-dark-800 dark:ring-amber-900/40"
          :class="peakRateTooltip.placement === 'top' ? '-translate-y-full' : ''"
          :style="peakRateTooltip.style"
          data-test="group-rate-peak-tooltip"
        >
          <div class="text-[11px] font-semibold text-amber-700 dark:text-amber-300">
            {{ groupPeakRateTooltipHeader(peakRateTooltip.group) }}
          </div>
          <div class="mt-1.5 space-y-1">
            <div
              v-for="line in groupPeakRateWindowLines(peakRateTooltip.group)"
              :key="line"
              class="rounded-md bg-amber-50 px-2 py-1 font-mono text-[11px] leading-4 text-amber-900 break-words dark:bg-amber-950/40 dark:text-amber-100"
              data-test="group-rate-peak-window"
            >
              {{ line }}
            </div>
          </div>
          <div class="mt-1.5 text-[11px] leading-4 text-gray-500 break-words dark:text-gray-400">
            {{ groupPeakRateBillingNote }}
          </div>
          <div
            class="absolute h-3 w-3 rotate-45 border-amber-200 bg-white dark:border-amber-800/70 dark:bg-dark-800"
            :class="
              peakRateTooltip.placement === 'top'
                ? 'top-full -translate-x-1/2 -translate-y-1/2 border-b border-r'
                : 'bottom-full -translate-x-1/2 translate-y-1/2 border-l border-t'
            "
            :style="peakRateTooltip.arrowStyle"
          ></div>
        </div>
      </Transition>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useAppStore } from "@/stores/app";
import { useOnboardingStore } from "@/stores/onboarding";
import { adminAPI } from "@/api/admin";
import type {
  AdminGroup,
  CompositeModelRoute,
  CompositeModelRouteInput,
  CompositeRouteDecision,
  CompositeRouteEndpoint,
  CompositeRouteMatchType,
  CreateGroupRequest,
  GroupPlatform,
  SubscriptionType,
  UpdateGroupRequest,
} from "@/types";
import type { Column } from "@/components/common/types";
import AppLayout from "@/components/layout/AppLayout.vue";
import TablePageLayout from "@/components/layout/TablePageLayout.vue";
import DataTable from "@/components/common/DataTable.vue";
import Pagination from "@/components/common/Pagination.vue";
import BaseDialog from "@/components/common/BaseDialog.vue";
import ConfirmDialog from "@/components/common/ConfirmDialog.vue";
import EmptyState from "@/components/common/EmptyState.vue";
import Select from "@/components/common/Select.vue";
import PlatformIcon from "@/components/common/PlatformIcon.vue";
import Icon from "@/components/icons/Icon.vue";
import GroupRateMultipliersModal from "@/components/admin/group/GroupRateMultipliersModal.vue";
import GroupRPMOverridesModal from "@/components/admin/group/GroupRPMOverridesModal.vue";
import GroupCapacityBadge from "@/components/common/GroupCapacityBadge.vue";
import ReasoningEffortPolicyFields from "@/components/admin/group/ReasoningEffortPolicyFields.vue";
import { VueDraggable } from "vue-draggable-plus";
import { createStableObjectKeyResolver } from "@/utils/stableObjectKey";
import { extractApiErrorMessage } from "@/utils/apiError";
import { useKeyedDebouncedSearch } from "@/composables/useKeyedDebouncedSearch";
import { getPersistedPageSize } from "@/composables/usePersistedPageSize";
import { formatCurrencyAmount, getDisplayCurrencySymbol } from "@/utils/format";
import { platformLabel } from "@/utils/platformColors";
import {
  createDefaultMessagesDispatchFormState,
  messagesDispatchConfigToFormState,
  messagesDispatchFormStateToConfig,
  resetMessagesDispatchFormState,
  type MessagesDispatchMappingRow,
} from "./groupsMessagesDispatch";
import {
  appendPeakRateWindow,
  buildPeakRatePayload,
  normalizeRateMultiplier,
  peakRateWindowsFromGroup,
  removePeakRateWindow,
  type PeakRateWindowForm,
} from "./groupsPeakRateForm";
import {
  hasPeakRate as hasPeakRateFields,
  peakRateWindowsForDisplay,
  serverTimezoneLabel,
} from "@/utils/peak-rate";
import {
  addModelsListItem,
  buildModelsListConfig,
  cancelModelsListItemEdit,
  commitModelsListItemEdit,
  createModelsListState as createInitialModelsListState,
  moveModelsListItem,
  removeModelsListItem,
  removeSelectedModelsListItems,
  setModelsListCandidates,
  startEditModelsListItem,
} from "./groupsModelsList";
import { createModelsListCandidatesTracker } from "./groupsModelsListCandidates";
import { normalizeSupportedModelScopesForPlatform } from "./groupsSupportedModelScopes";
import {
  isProfitControlPlatform,
  profitPercentToDecimal,
  profitDecimalToPercent,
  validateProfitControlFormState,
  type ProfitControlFormState,
} from "./groupsProfitControl";
import {
  normalizeReasoningEffortForPlatform,
  reasoningEffortMappingsToAPI,
  reasoningEffortMappingsToRows,
  supportsReasoningEffortPolicyPlatform,
  type ReasoningEffortMappingRow,
} from "./groupsReasoningEffort";
import {
  getDefaultImagePreviewPrice,
  getDefaultVideoPreviewPrice,
  getImagePricePlaceholder,
  getVideoPricePlaceholder,
  imagePricingI18nKey,
  supportsImagePricingPlatform,
  supportsVideoPricingPlatform,
  videoPricingI18nKey,
} from "./groupsImagePricing";

const { t } = useI18n();
const appStore = useAppStore();
const onboardingStore = useOnboardingStore();

const ALWAYS_VISIBLE_COLUMNS = new Set(["name", "actions"]);
// Default hidden columns (hidden on first load / after schema bumps).
const DEFAULT_HIDDEN_COLUMNS = ["id"];
const HIDDEN_COLUMNS_KEY = "group-hidden-columns";
// Bump when adding new default-hidden columns so existing admins pick them up once.
const COLUMN_SETTINGS_VERSION_KEY = "group-column-settings-version";
const COLUMN_SETTINGS_VERSION = 2;
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  2: ["id"],
};

const allColumns = computed<Column[]>(() => [
  { key: "name", label: t("admin.groups.columns.name"), sortable: true },
  { key: "id", label: t("admin.groups.columns.id"), sortable: true },
  {
    key: "platform",
    label: t("admin.groups.columns.platform"),
    sortable: true,
  },
  {
    key: "billing_type",
    label: t("admin.groups.columns.billingType"),
    sortable: true,
  },
  {
    key: "rate_multiplier",
    label: t("admin.groups.columns.rateMultiplier"),
    sortable: true,
  },
  {
    key: "is_exclusive",
    label: t("admin.groups.columns.type"),
    sortable: true,
  },
  {
    key: "account_count",
    label: t("admin.groups.columns.accounts"),
    sortable: true,
  },
  {
    key: "capacity",
    label: t("admin.groups.columns.capacity"),
    sortable: false,
  },
  { key: "usage", label: t("admin.groups.columns.usage"), sortable: false },
  { key: "status", label: t("admin.groups.columns.status"), sortable: true },
  { key: "actions", label: t("admin.groups.columns.actions"), sortable: false },
]);

const toggleableColumns = computed(() =>
  allColumns.value.filter((col) => !ALWAYS_VISIBLE_COLUMNS.has(col.key)),
);
const hiddenColumns = reactive<Set<string>>(new Set());
const showColumnDropdown = ref(false);
const columnDropdownRef = ref<HTMLElement | null>(null);

const getValidHiddenColumnKeys = () =>
  new Set(toggleableColumns.value.map((col) => col.key));

const loadSavedColumns = () => {
  hiddenColumns.clear();
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY);
    const validKeys = getValidHiddenColumnKeys();

    if (saved) {
      const parsed = JSON.parse(saved);
      if (Array.isArray(parsed)) {
        parsed
          .filter(
            (key): key is string =>
              typeof key === "string" && validKeys.has(key),
          )
          .forEach((key) => hiddenColumns.add(key));
      }

      // Existing admins: auto-hide columns newly added as default-hidden.
      const storedVersion = Number(
        localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? "1",
      );
      if (storedVersion < COLUMN_SETTINGS_VERSION) {
        let mutated = false;
        for (let v = storedVersion + 1; v <= COLUMN_SETTINGS_VERSION; v++) {
          for (const key of VERSION_NEW_HIDDEN_COLUMNS[v] ?? []) {
            if (validKeys.has(key) && !hiddenColumns.has(key)) {
              hiddenColumns.add(key);
              mutated = true;
            }
          }
        }
        if (mutated) {
          saveColumnsToStorage();
        } else {
          localStorage.setItem(
            COLUMN_SETTINGS_VERSION_KEY,
            String(COLUMN_SETTINGS_VERSION),
          );
        }
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        if (validKeys.has(key)) hiddenColumns.add(key);
      });
      saveColumnsToStorage();
    }
  } catch (error) {
    console.error("Failed to load group column settings:", error);
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key));
  }
};

const saveColumnsToStorage = () => {
  try {
    const validKeys = getValidHiddenColumnKeys();
    const keys = [...hiddenColumns].filter((key) => validKeys.has(key));
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(keys));
    localStorage.setItem(
      COLUMN_SETTINGS_VERSION_KEY,
      String(COLUMN_SETTINGS_VERSION),
    );
  } catch (error) {
    console.error("Failed to save group column settings:", error);
  }
};

const isColumnVisible = (key: string) => !hiddenColumns.has(key);
const hasVisibleUsageSummaryConsumer = computed(
  () => isColumnVisible("usage") || isColumnVisible("billing_type"),
);
const hasVisibleCapacityColumn = computed(() => isColumnVisible("capacity"));

const toggleColumn = (key: string) => {
  const validKeys = getValidHiddenColumnKeys();
  if (!validKeys.has(key)) return;

  const wasHidden = hiddenColumns.has(key);
  if (wasHidden) {
    hiddenColumns.delete(key);
  } else {
    hiddenColumns.add(key);
  }
  saveColumnsToStorage();

  if (wasHidden && (key === "usage" || key === "billing_type")) {
    loadUsageSummary();
  }
  if (wasHidden && key === "capacity") {
    loadCapacitySummary();
  }
};

const columns = computed<Column[]>(() =>
  allColumns.value.filter(
    (col) => ALWAYS_VISIBLE_COLUMNS.has(col.key) || !hiddenColumns.has(col.key),
  ),
);

interface PeakRateTooltipState {
  group: AdminGroup;
  placement: "top" | "bottom";
  style: Record<string, string>;
  arrowStyle: Record<string, string>;
}

const PEAK_RATE_TOOLTIP_MARGIN = 12;
const PEAK_RATE_TOOLTIP_MAX_WIDTH = 560;
const PEAK_RATE_TOOLTIP_MIN_WIDTH = 320;

const peakRateTooltip = ref<PeakRateTooltipState | null>(null);

const groupPeakRateFields = (group: AdminGroup) => ({
  peak_rate_enabled: group.peak_rate_enabled,
  peak_start: group.peak_start,
  peak_end: group.peak_end,
  peak_rate_multiplier: group.peak_rate_multiplier,
  peak_rate_windows: group.peak_rate_windows,
});

const hasGroupPeakRate = (group: AdminGroup) =>
  hasPeakRateFields(groupPeakRateFields(group));

const groupPeakRateText = (group: AdminGroup) => {
  const windows = peakRateWindowsForDisplay(groupPeakRateFields(group));
  if (windows.length === 1) {
    return t("common.peakRateCompactSingle", {
      multiplier: windows[0].multiplier ?? 1,
    });
  }
  return t("common.peakRateCompactMultiple", { count: windows.length });
};

const formatMultiplier = (value: number) => {
  if (!Number.isFinite(value)) return "1";
  return Number(value.toFixed(6)).toString();
};

const groupPeakRateBaseMultiplier = (group: AdminGroup) => group.rate_multiplier ?? 1;

const formatGroupPeakRateWindowLine = (group: AdminGroup, window: { start: string; end: string; multiplier?: number }) => {
  const base = groupPeakRateBaseMultiplier(group);
  const peak = window.multiplier ?? 1;
  return t("common.peakRateFormula", {
    window: `${window.start}-${window.end}`,
    base: formatMultiplier(base),
    peak: formatMultiplier(peak),
    final: formatMultiplier(base * peak),
  });
};

const groupPeakRateWindowLines = (group: AdminGroup) =>
  peakRateWindowsForDisplay(groupPeakRateFields(group)).map(
    (window) => formatGroupPeakRateWindowLine(group, window),
  );

const groupPeakRateTooltipHeader = (_group: AdminGroup) => {
  const base = t("common.peakRateTooltip", { window: "" }).trim();
  const tzLabel = serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset);
  return tzLabel ? `${base} (${tzLabel})` : base;
};

const groupPeakRateBillingNote = computed(() =>
  t("common.peakRateImageNote").replace(/^[；;]\s*/, ""),
);

const groupPeakRateAccessibleLabel = (group: AdminGroup) =>
  `${groupPeakRateTooltipHeader(group)} ${groupPeakRateWindowLines(group).join("; ")} ${groupPeakRateBillingNote.value}`;

const getPeakRateTooltipWidth = () => {
  if (typeof window === "undefined") return PEAK_RATE_TOOLTIP_MAX_WIDTH;
  const viewportSafeWidth = window.innerWidth - PEAK_RATE_TOOLTIP_MARGIN * 2;
  return Math.max(
    Math.min(PEAK_RATE_TOOLTIP_MIN_WIDTH, viewportSafeWidth),
    Math.min(PEAK_RATE_TOOLTIP_MAX_WIDTH, viewportSafeWidth),
  );
};

const showGroupPeakRateTooltip = (group: AdminGroup, event: MouseEvent | FocusEvent) => {
  if (typeof window === "undefined") return;

  const target = (event.currentTarget || event.target) as HTMLElement | null;
  if (!target) return;

  const rect = target.getBoundingClientRect();
  const width = getPeakRateTooltipWidth();
  const anchorCenter = rect.left + rect.width / 2;
  const left = Math.min(
    Math.max(PEAK_RATE_TOOLTIP_MARGIN, anchorCenter - width / 2),
    Math.max(PEAK_RATE_TOOLTIP_MARGIN, window.innerWidth - width - PEAK_RATE_TOOLTIP_MARGIN),
  );
  const estimatedHeight = Math.min(
    Math.max(128, 94 + groupPeakRateWindowLines(group).length * 38),
    Math.max(128, window.innerHeight - PEAK_RATE_TOOLTIP_MARGIN * 2),
  );
  const placement: PeakRateTooltipState["placement"] =
    rect.top > estimatedHeight + PEAK_RATE_TOOLTIP_MARGIN ? "top" : "bottom";
  const top =
    placement === "top"
      ? Math.max(PEAK_RATE_TOOLTIP_MARGIN, rect.top - 10)
      : Math.min(window.innerHeight - PEAK_RATE_TOOLTIP_MARGIN, rect.bottom + 10);
  const arrowLeft = Math.min(Math.max(16, anchorCenter - left), width - 16);

  peakRateTooltip.value = {
    group,
    placement,
    style: {
      left: `${left}px`,
      top: `${top}px`,
      width: `${width}px`,
      maxHeight: `${Math.max(128, window.innerHeight - PEAK_RATE_TOOLTIP_MARGIN * 2)}px`,
      overflowY: "auto",
    },
    arrowStyle: {
      left: `${arrowLeft}px`,
    },
  };
};

const hideGroupPeakRateTooltip = () => {
  peakRateTooltip.value = null;
};

if (typeof window !== "undefined") {
  loadSavedColumns();
}

// Filter options
const statusOptions = computed(() => [
  { value: "", label: t("admin.groups.allStatus") },
  { value: "active", label: t("admin.accounts.status.active") },
  { value: "inactive", label: t("admin.accounts.status.inactive") },
]);

const exclusiveOptions = computed(() => [
  { value: "", label: t("admin.groups.allGroups") },
  { value: "true", label: t("admin.groups.exclusive") },
  { value: "false", label: t("admin.groups.nonExclusive") },
]);

const platformDisplayName = (platform: string) => platformLabel(platform, t);
type VisibleGroupPlatform = GroupPlatform;
type PlatformSelectOption = { value: VisibleGroupPlatform; label: string };
const groupPlatformValues: VisibleGroupPlatform[] = [
  "anthropic",
  "openai",
  "gemini",
  "antigravity",
  "zhipu",
  "deepseek",
  "volcengine",
  "ali",
  "moonshot",
  "perplexity",
  "mistral",
  "siliconflow",
  "openrouter",
  "suno",
  "kling",
  "midjourney",
  "composite",
  "grok",
];

const platformOptions = computed<PlatformSelectOption[]>(() =>
  groupPlatformValues.map((value) => ({
    value,
    label: platformDisplayName(value),
  })),
);

interface GroupPlatformSearchOption extends PlatformSelectOption {
  subtitle: string;
  searchTerms: string[];
  activeClass: string;
  iconActiveClass: string;
  textActiveClass: string;
}

const createPlatformSearchMeta: Record<
  VisibleGroupPlatform,
  Omit<GroupPlatformSearchOption, keyof PlatformSelectOption>
> = {
  anthropic: {
    subtitle: "Claude / Messages",
    searchTerms: ["anthropic", "claude", "messages", "sonnet", "opus", "haiku"],
    activeClass:
      "border-orange-200 bg-white shadow-sm dark:border-orange-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-orange-50 text-orange-600 dark:bg-orange-900/30 dark:text-orange-300",
    textActiveClass: "text-orange-600 dark:text-orange-300",
  },
  openai: {
    subtitle: "GPT / Responses",
    searchTerms: ["openai", "gpt", "responses", "chatgpt", "o1", "o3"],
    activeClass:
      "border-emerald-200 bg-white shadow-sm dark:border-emerald-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300",
    textActiveClass: "text-emerald-600 dark:text-emerald-300",
  },
  gemini: {
    subtitle: "Google / v1beta",
    searchTerms: ["gemini", "google", "v1beta", "flash", "pro"],
    activeClass:
      "border-blue-200 bg-white shadow-sm dark:border-blue-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300",
    textActiveClass: "text-blue-600 dark:text-blue-300",
  },
  antigravity: {
    subtitle: "Mixed scheduling",
    searchTerms: ["antigravity", "mixed", "cloud", "claude", "gemini"],
    activeClass:
      "border-purple-200 bg-white shadow-sm dark:border-purple-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-purple-50 text-purple-600 dark:bg-purple-900/30 dark:text-purple-300",
    textActiveClass: "text-purple-600 dark:text-purple-300",
  },
  grok: {
    subtitle: "OAuth / xAI API Key",
    searchTerms: ["grok", "xai", "x.ai", "oauth", "api key"],
    activeClass:
      "border-zinc-300 bg-white shadow-sm dark:border-zinc-700 dark:bg-dark-600",
    iconActiveClass:
      "bg-zinc-100 text-zinc-800 dark:bg-zinc-800 dark:text-zinc-100",
    textActiveClass: "text-zinc-900 dark:text-zinc-100",
  },
  composite: {
    subtitle: "Multi-provider route",
    searchTerms: ["composite", "route", "multi", "gateway", "model"],
    activeClass:
      "border-cyan-200 bg-white shadow-sm dark:border-cyan-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-cyan-50 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300",
    textActiveClass: "text-cyan-700 dark:text-cyan-300",
  },
  zhipu: {
    subtitle: "Zhipu / GLM 系列",
    searchTerms: ["zhipu", "glm", "智谱", "bigmodel", "glm-4"],
    activeClass:
      "border-emerald-200 bg-white shadow-sm dark:border-emerald-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300",
    textActiveClass: "text-emerald-600 dark:text-emerald-300",
  },
  deepseek: {
    subtitle: "DeepSeek 系列",
    searchTerms: ["deepseek", "ds", "深度求索", "deepseek-chat", "deepseek-reasoner"],
    activeClass:
      "border-cyan-200 bg-white shadow-sm dark:border-cyan-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-cyan-50 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-300",
    textActiveClass: "text-cyan-600 dark:text-cyan-300",
  },
  volcengine: {
    subtitle: "VolcEngine / Doubao",
    searchTerms: ["volcengine", "doubao", "ark", "火山", "豆包"],
    activeClass:
      "border-rose-200 bg-white shadow-sm dark:border-rose-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-rose-50 text-rose-600 dark:bg-rose-900/30 dark:text-rose-300",
    textActiveClass: "text-rose-600 dark:text-rose-300",
  },
  ali: {
    subtitle: "DashScope / Qwen",
    searchTerms: ["ali", "alibaba", "qwen", "dashscope", "阿里", "通义"],
    activeClass:
      "border-amber-200 bg-white shadow-sm dark:border-amber-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300",
    textActiveClass: "text-amber-600 dark:text-amber-300",
  },
  moonshot: {
    subtitle: "Moonshot / Kimi",
    searchTerms: ["moonshot", "kimi", "鏈堜箣鏆楅潰", "moonshot-v1"],
    activeClass:
      "border-fuchsia-200 bg-white shadow-sm dark:border-fuchsia-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-fuchsia-50 text-fuchsia-600 dark:bg-fuchsia-900/30 dark:text-fuchsia-300",
    textActiveClass: "text-fuchsia-600 dark:text-fuchsia-300",
  },
  perplexity: {
    subtitle: "Sonar / New-API",
    searchTerms: ["perplexity", "sonar", "pplx"],
    activeClass:
      "border-sky-200 bg-white shadow-sm dark:border-sky-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-sky-50 text-sky-600 dark:bg-sky-900/30 dark:text-sky-300",
    textActiveClass: "text-sky-600 dark:text-sky-300",
  },
  mistral: {
    subtitle: "Mistral AI / New-API",
    searchTerms: ["mistral", "mixtral", "codestral", "mistral-large"],
    activeClass:
      "border-violet-200 bg-white shadow-sm dark:border-violet-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-300",
    textActiveClass: "text-violet-600 dark:text-violet-300",
  },
  siliconflow: {
    subtitle: "SiliconCloud / New-API",
    searchTerms: ["siliconflow", "silicon", "纭呭熀娴佸姩"],
    activeClass:
      "border-teal-200 bg-white shadow-sm dark:border-teal-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-teal-50 text-teal-600 dark:bg-teal-900/30 dark:text-teal-300",
    textActiveClass: "text-teal-600 dark:text-teal-300",
  },
  openrouter: {
    subtitle: "Multi-provider / New-API",
    searchTerms: ["openrouter", "router", "multi-provider"],
    activeClass:
      "border-indigo-200 bg-white shadow-sm dark:border-indigo-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-indigo-50 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-300",
    textActiveClass: "text-indigo-600 dark:text-indigo-300",
  },
  suno: {
    subtitle: "Music task / New-API",
    searchTerms: ["suno", "music", "song", "audio", "音乐"],
    activeClass:
      "border-yellow-200 bg-white shadow-sm dark:border-yellow-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-yellow-50 text-yellow-600 dark:bg-yellow-900/30 dark:text-yellow-300",
    textActiveClass: "text-yellow-600 dark:text-yellow-300",
  },
  kling: {
    subtitle: "Video task / New-API",
    searchTerms: ["kling", "video", "kuaishou", "可灵", "视频"],
    activeClass:
      "border-red-200 bg-white shadow-sm dark:border-red-900/50 dark:bg-dark-600",
    iconActiveClass:
      "bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300",
    textActiveClass: "text-red-600 dark:text-red-300",
  },
  midjourney: {
    subtitle: "Image task / New-API",
    searchTerms: ["midjourney", "mj", "image", "图片", "绘图"],
    activeClass:
      "border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-dark-600",
    iconActiveClass:
      "bg-slate-50 text-slate-600 dark:bg-slate-800 dark:text-slate-300",
    textActiveClass: "text-slate-600 dark:text-slate-300",
  },
};

const platformFilterOptions = computed(() => [
  { value: "", label: t("admin.groups.allPlatforms") },
  ...platformOptions.value,
]);

const compositeRouteTargetPlatforms: Exclude<GroupPlatform, "composite">[] = [
  "anthropic",
  "openai",
  "gemini",
  "antigravity",
  "grok",
  "zhipu",
  "deepseek",
  "volcengine",
  "ali",
  "moonshot",
  "perplexity",
  "mistral",
  "siliconflow",
  "openrouter",
  "suno",
  "kling",
  "midjourney",
];

const compositeRoutePlatformOptions = computed(() =>
  compositeRouteTargetPlatforms.map((value) => ({
    value,
    label: platformDisplayName(value),
  })),
);

const compositeRouteEndpointOptions = computed(() => [
  { value: "any", label: t("admin.groups.compositeRoutes.endpoints.any") },
  {
    value: "messages",
    label: t("admin.groups.compositeRoutes.endpoints.messages"),
  },
  {
    value: "count_tokens",
    label: t("admin.groups.compositeRoutes.endpoints.countTokens"),
  },
  {
    value: "responses",
    label: t("admin.groups.compositeRoutes.endpoints.responses"),
  },
  {
    value: "chat_completions",
    label: t("admin.groups.compositeRoutes.endpoints.chatCompletions"),
  },
  {
    value: "embeddings",
    label: t("admin.groups.compositeRoutes.endpoints.embeddings"),
  },
  { value: "images", label: t("admin.groups.compositeRoutes.endpoints.images") },
  { value: "gemini", label: t("admin.groups.compositeRoutes.endpoints.gemini") },
]);

const compositeRouteMatchOptions = computed(() => [
  { value: "exact", label: t("admin.groups.compositeRoutes.match.exact") },
  { value: "prefix", label: t("admin.groups.compositeRoutes.match.prefix") },
]);

const editStatusOptions = computed(() => [
  { value: "active", label: t("admin.accounts.status.active") },
  { value: "inactive", label: t("admin.accounts.status.inactive") },
]);

const subscriptionTypeOptions = computed(() => [
  { value: "standard", label: t("admin.groups.subscription.standard") },
  { value: "subscription", label: t("admin.groups.subscription.subscription") },
]);

const fallbackGroupOptions = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.claudeCode.noFallback") },
  ];
  const eligibleGroups = groups.value.filter(
    (g) =>
      g.platform === "anthropic" &&
      !g.claude_code_only &&
      g.status === "active",
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 闄嶇骇鍒嗙粍閫夐」锛堢紪杈戞椂锛? 鎺掗櫎鑷韩
const fallbackGroupOptionsForEdit = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.claudeCode.noFallback") },
  ];
  const currentId = editingGroup.value?.id;
  const eligibleGroups = groups.value.filter(
    (g) =>
      g.platform === "anthropic" &&
      !g.claude_code_only &&
      g.status === "active" &&
      g.id !== currentId,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

const invalidRequestFallbackOptions = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.invalidRequestFallback.noFallback") },
  ];
  const eligibleGroups = groups.value.filter(
    (g) =>
      g.platform === "anthropic" &&
      g.status === "active" &&
      g.subscription_type !== "subscription" &&
      g.fallback_group_id_on_invalid_request === null,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

// 鏃犳晥璇锋眰鍏滃簳鍒嗙粍閫夐」锛堢紪杈戞椂锛? 鎺掗櫎鑷韩
const invalidRequestFallbackOptionsForEdit = computed(() => {
  const options: { value: number | null; label: string }[] = [
    { value: null, label: t("admin.groups.invalidRequestFallback.noFallback") },
  ];
  const currentId = editingGroup.value?.id;
  const eligibleGroups = groups.value.filter(
    (g) =>
      g.platform === "anthropic" &&
      g.status === "active" &&
      g.subscription_type !== "subscription" &&
      g.fallback_group_id_on_invalid_request === null &&
      g.id !== currentId,
  );
  eligibleGroups.forEach((g) => {
    options.push({ value: g.id, label: g.name });
  });
  return options;
});

const canCopyAccountsFromGroup = (targetPlatform: GroupPlatform, sourcePlatform: GroupPlatform) =>
  targetPlatform === "composite"
    ? compositeRouteTargetPlatforms.includes(sourcePlatform as Exclude<GroupPlatform, "composite">)
    : sourcePlatform === targetPlatform;

const copyAccountsGroupLabel = (g: AdminGroup) => {
  const count = g.account_count || 0;
  const platform = platformDisplayName(g.platform);
  return `${g.name} - ${platform} (${t("admin.groups.accountsCount", { count })})`;
};

const copyAccountsGroupOptions = computed(() => {
  const eligibleGroups = groups.value.filter(
    (g) =>
      canCopyAccountsFromGroup(createForm.platform, g.platform) &&
      (g.account_count || 0) > 0,
  );
  return eligibleGroups.map((g) => ({
    value: g.id,
    label: copyAccountsGroupLabel(g),
  }));
});

const copyAccountsGroupOptionsForEdit = computed(() => {
  const currentId = editingGroup.value?.id;
  const eligibleGroups = groups.value.filter(
    (g) =>
      canCopyAccountsFromGroup(editForm.platform, g.platform) &&
      (g.account_count || 0) > 0 &&
      g.id !== currentId,
  );
  return eligibleGroups.map((g) => ({
    value: g.id,
    label: copyAccountsGroupLabel(g),
  }));
});

const groups = ref<AdminGroup[]>([]);
const loading = ref(false);
const usageLoading = ref(false);
const usageMap = ref(
  new Map<number, { today_cost: number; total_cost: number; real_today_cost?: number; real_total_cost?: number }>(),
);
const capacityMap = ref<
  Map<
    number,
    {
      concurrencyUsed: number;
      concurrencyMax: number;
      sessionsUsed: number;
      sessionsMax: number;
      rpmUsed: number;
      rpmMax: number;
    }
  >
>(new Map());
const searchQuery = ref("");
const filters = reactive({
  platform: "",
  status: "",
  is_exclusive: "",
});
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0,
});
const sortState = reactive({
  sort_by: "sort_order",
  sort_order: "asc" as "asc" | "desc",
});

let abortController: AbortController | null = null;

const showCreateModal = ref(false);
const showEditModal = ref(false);
const showDeleteDialog = ref(false);
const pendingLiveForm = ref<"create" | "edit" | null>(null);
const showUnsupportedLiveConfirm = computed(
  () => pendingLiveForm.value !== null,
);
const liveCapability = ref<{ supported: boolean; reason?: string } | null>(null);
let liveCapabilityRequest: Promise<{
  supported: boolean;
  reason?: string;
}> | null = null;
const showSortModal = ref(false);
const submitting = ref(false);
const sortSubmitting = ref(false);
const editingGroup = ref<AdminGroup | null>(null);
const deletingGroup = ref<AdminGroup | null>(null);
const duplicatingGroupIds = reactive(new Set<number>());
const showRateMultipliersModal = ref(false);
const rateMultipliersGroup = ref<AdminGroup | null>(null);
const showRPMOverridesModal = ref(false);
const rpmOverridesGroup = ref<AdminGroup | null>(null);
const sortableGroups = ref<AdminGroup[]>([]);
type ConcreteGroupPlatform = Exclude<GroupPlatform, "composite">;
type CompositeRouteFormState = {
  public_model: string;
  match_type: CompositeRouteMatchType;
  target_platform: ConcreteGroupPlatform;
  upstream_model: string;
  endpoint: CompositeRouteEndpoint;
  priority: number;
  enabled: boolean;
  notes: string;
};

const showCompositeRoutesModal = ref(false);
const compositeRoutesGroup = ref<AdminGroup | null>(null);
const compositeRoutes = ref<CompositeModelRoute[]>([]);
const compositeRoutesLoading = ref(false);
const compositeRouteSaving = ref(false);
const compositeRouteEditingId = ref<number | null>(null);
const compositePreviewModel = ref("");
const compositePreviewEndpoint = ref<CompositeRouteEndpoint>("any");
const compositePreviewLoading = ref(false);
const compositePreviewDecision = ref<CompositeRouteDecision | null>(null);
const compositeRouteForm = reactive<CompositeRouteFormState>({
  public_model: "",
  match_type: "exact",
  target_platform: "openai",
  upstream_model: "",
  endpoint: "any",
  priority: 100,
  enabled: true,
  notes: "",
});
const createMessagesDispatchDefaults = createDefaultMessagesDispatchFormState();
const editMessagesDispatchDefaults = createDefaultMessagesDispatchFormState();
const createModelsListState = reactive(createInitialModelsListState());
const editModelsListState = reactive(createInitialModelsListState());
const createModelsListLoading = ref(false);
const editModelsListLoading = ref(false);
type ReasoningEffortPolicyFieldsExpose = {
  validate: () => boolean;
  resetValidation: () => void;
};
const createReasoningEffortPolicyRef = ref<ReasoningEffortPolicyFieldsExpose | null>(null);
const editReasoningEffortPolicyRef = ref<ReasoningEffortPolicyFieldsExpose | null>(null);
const modelsListCandidatesTracker = createModelsListCandidatesTracker();
const createModelsListSelectedCount = computed(
  () => createModelsListState.items.filter((item) => item.selected).length,
);
const editModelsListSelectedCount = computed(
  () => editModelsListState.items.filter((item) => item.selected).length,
);

const createForm = reactive({
  name: "",
  description: "",
  platform: "anthropic" as GroupPlatform,
  rate_multiplier: 1.0,
  is_exclusive: false,
  subscription_type: "standard" as SubscriptionType,
  daily_limit_usd: null as number | null,
  weekly_limit_usd: null as number | null,
  monthly_limit_usd: null as number | null,
  custom_limit_hours: 0 as number,
  custom_limit_usd: null as number | null,
  // 鍥剧墖鐢熸垚璁¤垂閰嶇疆
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null as number | null,
  image_price_2k: null as number | null,
  image_price_4k: null as number | null,
  video_rate_independent: false,
  // 瑙嗛鐢熸垚璁¤垂閰嶇疆锛堜粎 Grok 骞冲彴锛?  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null as number | null,
  video_price_720p: null as number | null,
  video_price_1080p: null as number | null,
  // 楂樺嘲鏃舵鍊嶇巼閰嶇疆
  // Codex 网页搜索按次计费（仅 openai 平台使用）；null = 使用默认价 0.01
  web_search_price_per_call: null as number | null,
  // 高峰时段倍率配置
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1.0,
  peak_rate_windows: [] as PeakRateWindowForm[],
  // 分组利润控制（五个 token 平台）；界面按百分比输入，提交时转小数
  profit_control_enabled: false,
  profit_min_margin_percent: 0,
  profit_safety_buffer_percent: 0,
  // Claude Code 客户端限制（仅 anthropic 平台使用）
  claude_code_only: false,
  // Claude Code 瀹㈡埛绔檺鍒讹紙浠?anthropic 骞冲彴浣跨敤锛?  claude_code_only: false,
  fallback_group_id: null as number | null,
  fallback_group_id_on_invalid_request: null as number | null,
  allow_messages_dispatch: false,
  // OpenAI Messages 璋冨害閰嶇疆锛堜粎 openai 骞冲彴浣跨敤锛?  allow_messages_dispatch: false,
  allow_live: false,
  opus_mapped_model: createMessagesDispatchDefaults.opus_mapped_model,
  sonnet_mapped_model: createMessagesDispatchDefaults.sonnet_mapped_model,
  haiku_mapped_model: createMessagesDispatchDefaults.haiku_mapped_model,
  exact_model_mappings: [] as MessagesDispatchMappingRow[],
  require_oauth_only: false,
  // 璐﹀彿杩囨护鎺у埗锛圤penAI/Antigravity 骞冲彴锛?  require_oauth_only: false,
  require_privacy_set: false,
  model_routing_enabled: false,
  supported_model_scopes: ["claude", "gemini_text", "gemini_image"] as string[],
  mcp_xml_inject: true,
  copy_accounts_from_group_ids: [] as number[],
  // 妯″瀷璺敱寮€鍏?  model_routing_enabled: false,
  // 鏀寔鐨勬ā鍨嬬郴鍒楋紙浠?antigravity 骞冲彴锛?  supported_model_scopes: ["claude", "gemini_text", "gemini_image"] as string[],
  // MCP XML 鍗忚娉ㄥ叆寮€鍏筹紙浠?antigravity 骞冲彴锛?  mcp_xml_inject: true,
  // 浠庡垎缁勫鍒惰处鍙?  copy_accounts_from_group_ids: [] as number[],
  // 鍒嗙粍绾?RPM 闄愬埗锛堟瘡鐢ㄦ埛姣忓垎閽熸渶澶ц姹傛暟锛? = 涓嶉檺鍒讹級
  rpm_limit: 0 as number,
  newapi_style_interface_enabled: false,
  max_reasoning_effort: "",
  reasoning_effort_mappings: [] as ReasoningEffortMappingRow[],
});

const createPlatformSearchQuery = ref("");
const createPlatformScrollRef = ref<HTMLElement | null>(null);

const createPlatformSearchOptions = computed<GroupPlatformSearchOption[]>(() =>
  platformOptions.value.map((option) => ({
    ...option,
    ...createPlatformSearchMeta[option.value],
  })),
);

const filteredCreatePlatformOptions = computed(() => {
  const query = createPlatformSearchQuery.value.trim().toLowerCase();
  if (!query) {
    return createPlatformSearchOptions.value;
  }

  return createPlatformSearchOptions.value.filter((option) =>
    [
      option.value,
      option.label,
      option.subtitle,
      ...option.searchTerms,
    ].some((term) => term.toLowerCase().includes(query)),
  );
});

const selectCreatePlatform = (platform: GroupPlatform) => {
  if (createForm.platform === platform) {
    return;
  }
  createForm.platform = platform;
  createForm.copy_accounts_from_group_ids = [];
};

const handleCreatePlatformWheel = (event: WheelEvent) => {
  const container = createPlatformScrollRef.value;
  if (!container) {
    return;
  }
  const delta =
    Math.abs(event.deltaY) > Math.abs(event.deltaX)
      ? event.deltaY
      : event.deltaX;
  if (delta === 0) {
    return;
  }
  container.scrollBy({ left: delta, behavior: "auto" });
};

interface SimpleAccount {
// 绠€鍗曡处鍙风被鍨嬶紙鐢ㄤ簬妯″瀷璺敱閫夋嫨锛?interface SimpleAccount {
  id: number;
  name: string;
}

// 妯″瀷璺敱瑙勫垯绫诲瀷
interface ModelRoutingRule {
  pattern: string;
  accounts: SimpleAccount[];
// 閫変腑鐨勮处鍙峰璞℃暟缁?}
}

const createModelRoutingRules = ref<ModelRoutingRule[]>([]);
// 鍒涘缓琛ㄥ崟鐨勬ā鍨嬭矾鐢辫鍒?const createModelRoutingRules = ref<ModelRoutingRule[]>([]);

const editModelRoutingRules = ref<ModelRoutingRule[]>([]);
// 缂栬緫琛ㄥ崟鐨勬ā鍨嬭矾鐢辫鍒?const editModelRoutingRules = ref<ModelRoutingRule[]>([]);

// 瑙勫垯瀵硅薄绋冲畾 key锛堥伩鍏嶄娇鐢?index 瀵艰嚧鐘舵€侀敊浣嶏級
const resolveCreateRuleKey =
  createStableObjectKeyResolver<ModelRoutingRule>("create-rule");
const resolveEditRuleKey =
  createStableObjectKeyResolver<ModelRoutingRule>("edit-rule");
const resolveCreateMessagesDispatchRowKey =
  createStableObjectKeyResolver<MessagesDispatchMappingRow>(
    "create-messages-dispatch-row",
  );
const resolveEditMessagesDispatchRowKey =
  createStableObjectKeyResolver<MessagesDispatchMappingRow>(
    "edit-messages-dispatch-row",
  );

const getCreateRuleRenderKey = (rule: ModelRoutingRule) =>
  resolveCreateRuleKey(rule);
const getEditRuleRenderKey = (rule: ModelRoutingRule) =>
  resolveEditRuleKey(rule);
const getCreateMessagesDispatchRowKey = (row: MessagesDispatchMappingRow) =>
  resolveCreateMessagesDispatchRowKey(row);
const getEditMessagesDispatchRowKey = (row: MessagesDispatchMappingRow) =>
  resolveEditMessagesDispatchRowKey(row);

const getCreateRuleSearchKey = (rule: ModelRoutingRule) =>
  `create-${resolveCreateRuleKey(rule)}`;
const getEditRuleSearchKey = (rule: ModelRoutingRule) =>
  `edit-${resolveEditRuleKey(rule)}`;

const getRuleSearchKey = (rule: ModelRoutingRule, isEdit: boolean = false) => {
  return isEdit ? getEditRuleSearchKey(rule) : getCreateRuleSearchKey(rule);
};

const accountSearchKeyword = ref<Record<string, string>>({});
// 璐﹀彿鎼滅储鐩稿叧鐘舵€?const accountSearchKeyword = ref<Record<string, string>>({});
const accountSearchResults = ref<Record<string, SimpleAccount[]>>({});
const showAccountDropdown = ref<Record<string, boolean>>({});

const clearAccountSearchStateByKey = (key: string) => {
  delete accountSearchKeyword.value[key];
  delete accountSearchResults.value[key];
  delete showAccountDropdown.value[key];
};

const clearAllAccountSearchState = () => {
  accountSearchKeyword.value = {};
  accountSearchResults.value = {};
  showAccountDropdown.value = {};
};

const accountSearchRunner = useKeyedDebouncedSearch<SimpleAccount[]>({
  delay: 300,
  search: async (keyword, { signal }) => {
    const res = await adminAPI.accounts.list(
      1,
      20,
      {
        search: keyword,
        platform: "anthropic",
      },
      { signal },
    );
    return res.items.map((account) => ({ id: account.id, name: account.name }));
  },
  onSuccess: (key, result) => {
    accountSearchResults.value[key] = result;
  },
  onError: (key) => {
    accountSearchResults.value[key] = [];
  },
});

const searchAccounts = (key: string) => {
// 鎼滅储璐﹀彿锛堜粎闄?anthropic 骞冲彴锛?const searchAccounts = (key: string) => {
  accountSearchRunner.trigger(key, accountSearchKeyword.value[key] || "");
};

const searchAccountsByRule = (
  rule: ModelRoutingRule,
  isEdit: boolean = false,
) => {
  searchAccounts(getRuleSearchKey(rule, isEdit));
};

// 选择账号
const selectAccount = (
  rule: ModelRoutingRule,
  account: SimpleAccount,
  isEdit: boolean = false,
) => {
  if (!rule) return;

  // 妫€鏌ユ槸鍚﹀凡閫夋嫨
  if (!rule.accounts.some((a) => a.id === account.id)) {
    rule.accounts.push(account);
  }

  // 清空搜索
  const key = getRuleSearchKey(rule, isEdit);
  accountSearchKeyword.value[key] = "";
  showAccountDropdown.value[key] = false;
};

const removeSelectedAccount = (
// 绉婚櫎宸查€夎处鍙?const removeSelectedAccount = (
  rule: ModelRoutingRule,
  accountId: number,
  _isEdit: boolean = false,
) => {
  if (!rule) return;

  rule.accounts = rule.accounts.filter((a) => a.id !== accountId);
};

// 鍒囨崲鍒涘缓琛ㄥ崟鐨勬ā鍨嬬郴鍒楅€夋嫨
const toggleCreateScope = (scope: string) => {
  const idx = createForm.supported_model_scopes.indexOf(scope);
  if (idx === -1) {
    createForm.supported_model_scopes.push(scope);
  } else {
    createForm.supported_model_scopes.splice(idx, 1);
  }
};

// 鍒囨崲缂栬緫琛ㄥ崟鐨勬ā鍨嬬郴鍒楅€夋嫨
const toggleEditScope = (scope: string) => {
  const idx = editForm.supported_model_scopes.indexOf(scope);
  if (idx === -1) {
    editForm.supported_model_scopes.push(scope);
  } else {
    editForm.supported_model_scopes.splice(idx, 1);
  }
};

const onAccountSearchFocus = (
// 澶勭悊璐﹀彿鎼滅储杈撳叆妗嗚仛鐒?const onAccountSearchFocus = (
  rule: ModelRoutingRule,
  isEdit: boolean = false,
) => {
  const key = getRuleSearchKey(rule, isEdit);
  showAccountDropdown.value[key] = true;
  if (!accountSearchResults.value[key]?.length) {
  // 濡傛灉娌℃湁鎼滅储缁撴灉锛岃Е鍙戜竴娆℃悳绱?  if (!accountSearchResults.value[key]?.length) {
    searchAccounts(key);
  }
};

const addCreateRoutingRule = () => {
// 娣诲姞鍒涘缓琛ㄥ崟鐨勮矾鐢辫鍒?const addCreateRoutingRule = () => {
  createModelRoutingRules.value.push({ pattern: "", accounts: [] });
};

const removeCreateRoutingRule = (rule: ModelRoutingRule) => {
// 鍒犻櫎鍒涘缓琛ㄥ崟鐨勮矾鐢辫鍒?const removeCreateRoutingRule = (rule: ModelRoutingRule) => {
  const index = createModelRoutingRules.value.indexOf(rule);
  if (index === -1) return;

  const key = getCreateRuleSearchKey(rule);
  accountSearchRunner.clearKey(key);
  clearAccountSearchStateByKey(key);
  createModelRoutingRules.value.splice(index, 1);
};

const addEditRoutingRule = () => {
// 娣诲姞缂栬緫琛ㄥ崟鐨勮矾鐢辫鍒?const addEditRoutingRule = () => {
  editModelRoutingRules.value.push({ pattern: "", accounts: [] });
};

const removeEditRoutingRule = (rule: ModelRoutingRule) => {
// 鍒犻櫎缂栬緫琛ㄥ崟鐨勮矾鐢辫鍒?const removeEditRoutingRule = (rule: ModelRoutingRule) => {
  const index = editModelRoutingRules.value.indexOf(rule);
  if (index === -1) return;

  const key = getEditRuleSearchKey(rule);
  accountSearchRunner.clearKey(key);
  clearAccountSearchStateByKey(key);
  editModelRoutingRules.value.splice(index, 1);
};

const resetModelsListState = (
  state: typeof createModelsListState,
  config?: Parameters<typeof createInitialModelsListState>[0],
) => {
  const fresh = createInitialModelsListState(config);
  state.enabled = fresh.enabled;
  state.savedEnabled = fresh.savedEnabled;
  state.savedModels = fresh.savedModels;
  state.items = fresh.items;
  state.itemsInitialized = fresh.itemsInitialized;
};

const loadModelsListCandidates = async (
  mode: "create" | "edit",
  groupID: number,
  platform: GroupPlatform,
) => {
  const request = { mode, groupID, platform };
  const requestID = modelsListCandidatesTracker.next(request);
  const state = mode === "create" ? createModelsListState : editModelsListState;
  const loadingRef = mode === "create" ? createModelsListLoading : editModelsListLoading;
  loadingRef.value = true;
  try {
    const models = await adminAPI.groups.getModelsListCandidates(groupID, platform);
    if (!modelsListCandidatesTracker.isCurrent(requestID, request)) {
      return;
    }
    setModelsListCandidates(state, models);
  } catch (error) {
    if (!modelsListCandidatesTracker.isCurrent(requestID, request)) {
      return;
    }
    console.error("Error loading group models list candidates:", error);
  } finally {
    if (modelsListCandidatesTracker.isCurrent(requestID, request)) {
      loadingRef.value = false;
    }
  }
};

const moveCreateModelsListItem = (fromIndex: number, toIndex: number) => {
  moveModelsListItem(createModelsListState, fromIndex, toIndex);
};

const moveEditModelsListItem = (fromIndex: number, toIndex: number) => {
  moveModelsListItem(editModelsListState, fromIndex, toIndex);
};

// 灏?UI 鏍煎紡鐨勮矾鐢辫鍒欒浆鎹负 API 鏍煎紡
const convertRoutingRulesToApiFormat = (
  rules: ModelRoutingRule[],
): Record<string, number[]> | null => {
  const result: Record<string, number[]> = {};
  let hasValidRules = false;

  for (const rule of rules) {
    const pattern = rule.pattern.trim();
    if (!pattern) continue;

    const accountIds = rule.accounts.map((a) => a.id).filter((id) => id > 0);

    if (accountIds.length > 0) {
      result[pattern] = accountIds;
      hasValidRules = true;
    }
  }

  return hasValidRules ? result : null;
};

// 灏?API 鏍煎紡鐨勮矾鐢辫鍒欒浆鎹负 UI 鏍煎紡锛堥渶瑕佸姞杞借处鍙峰悕绉帮級
const convertApiFormatToRoutingRules = async (
  apiFormat: Record<string, number[]> | null,
): Promise<ModelRoutingRule[]> => {
  if (!apiFormat) return [];

  const rules: ModelRoutingRule[] = [];
  for (const [pattern, accountIds] of Object.entries(apiFormat)) {
    // 加载账号信息
    const accounts: SimpleAccount[] = [];
    for (const id of accountIds) {
      try {
        const account = await adminAPI.accounts.getById(id);
        accounts.push({ id: account.id, name: account.name });
      } catch {
        // 濡傛灉璐﹀彿涓嶅瓨鍦紝浠嶇劧鏄剧ず ID
        accounts.push({ id, name: `#${id}` });
      }
    }
    rules.push({ pattern, accounts });
  }
  return rules;
};

const editForm = reactive({
  name: "",
  description: "",
  platform: "anthropic" as GroupPlatform,
  rate_multiplier: 1.0,
  is_exclusive: false,
  status: "active" as "active" | "inactive",
  subscription_type: "standard" as SubscriptionType,
  daily_limit_usd: null as number | null,
  weekly_limit_usd: null as number | null,
  monthly_limit_usd: null as number | null,
  custom_limit_hours: 0 as number,
  custom_limit_usd: null as number | null,
  // 鍥剧墖鐢熸垚璁¤垂閰嶇疆
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null as number | null,
  image_price_2k: null as number | null,
  image_price_4k: null as number | null,
  video_rate_independent: false,
  // 瑙嗛鐢熸垚璁¤垂閰嶇疆锛堜粎 Grok 骞冲彴锛?  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null as number | null,
  video_price_720p: null as number | null,
  video_price_1080p: null as number | null,
  // 楂樺嘲鏃舵鍊嶇巼閰嶇疆
  // Codex 网页搜索按次计费（仅 openai 平台使用）；null = 使用默认价 0.01
  web_search_price_per_call: null as number | null,
  // 高峰时段倍率配置
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1.0,
  peak_rate_windows: [] as PeakRateWindowForm[],
  // 分组利润控制（五个 token 平台）；界面按百分比输入，提交时转小数
  profit_control_enabled: false,
  profit_min_margin_percent: 0,
  profit_safety_buffer_percent: 0,
  // Claude Code 客户端限制（仅 anthropic 平台使用）
  claude_code_only: false,
  // Claude Code 瀹㈡埛绔檺鍒讹紙浠?anthropic 骞冲彴浣跨敤锛?  claude_code_only: false,
  fallback_group_id: null as number | null,
  fallback_group_id_on_invalid_request: null as number | null,
  allow_messages_dispatch: false,
  // OpenAI Messages 璋冨害閰嶇疆锛堜粎 openai 骞冲彴浣跨敤锛?  allow_messages_dispatch: false,
  allow_live: false,
  default_mapped_model: '',
  opus_mapped_model: editMessagesDispatchDefaults.opus_mapped_model,
  sonnet_mapped_model: editMessagesDispatchDefaults.sonnet_mapped_model,
  haiku_mapped_model: editMessagesDispatchDefaults.haiku_mapped_model,
  exact_model_mappings: [] as MessagesDispatchMappingRow[],
  require_oauth_only: false,
  // 璐﹀彿杩囨护鎺у埗锛圤penAI/Antigravity 骞冲彴锛?  require_oauth_only: false,
  require_privacy_set: false,
  model_routing_enabled: false,
  supported_model_scopes: ["claude", "gemini_text", "gemini_image"] as string[],
  mcp_xml_inject: true,
  copy_accounts_from_group_ids: [] as number[],
  // 妯″瀷璺敱寮€鍏?  model_routing_enabled: false,
  // 鏀寔鐨勬ā鍨嬬郴鍒楋紙浠?antigravity 骞冲彴锛?  supported_model_scopes: ["claude", "gemini_text", "gemini_image"] as string[],
  // MCP XML 鍗忚娉ㄥ叆寮€鍏筹紙浠?antigravity 骞冲彴锛?  mcp_xml_inject: true,
  // 浠庡垎缁勫鍒惰处鍙?  copy_accounts_from_group_ids: [] as number[],
  // 鍒嗙粍绾?RPM 闄愬埗锛堟瘡鐢ㄦ埛姣忓垎閽熸渶澶ц姹傛暟锛? = 涓嶉檺鍒讹級
  rpm_limit: 0 as number,
  newapi_style_interface_enabled: false,
  max_reasoning_effort: "",
  reasoning_effort_mappings: [] as ReasoningEffortMappingRow[],
});

type ImagePricingFormState = {
  platform: GroupPlatform;
  allow_image_generation: boolean;
  allow_batch_image_generation: boolean;
  rate_multiplier: number;
  image_rate_independent: boolean;
  image_rate_multiplier: number;
  batch_image_discount_multiplier: number;
  batch_image_hold_multiplier: number;
  image_price_1k: number | string | null;
  image_price_2k: number | string | null;
  image_price_4k: number | string | null;
  peak_rate_enabled: boolean;
  peak_start: string;
  peak_end: string;
  peak_rate_multiplier: number;
};

type VideoPricingFormState = {
  platform: GroupPlatform;
  rate_multiplier: number;
  video_rate_independent: boolean;
  video_rate_multiplier: number;
  video_price_480p: number | string | null;
  video_price_720p: number | string | null;
  video_price_1080p: number | string | null;
};

const imagePricingTiers = [
  { key: "image_price_1k", label: "1K" },
  { key: "image_price_2k", label: "2K" },
  { key: "image_price_4k", label: "4K" },
] as const;

const videoPricingTiers = [
  { key: "video_price_480p", label: "480p" },
  { key: "video_price_720p", label: "720p" },
  { key: "video_price_1080p", label: "1080p" },
] as const;

const normalizePreviewNumber = (value: number | string | null | undefined, fallback = 0) => {
  if (value === null || value === undefined || value === "") {
    return fallback;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const parsePreviewPrice = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return null;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

const formatImagePricePreview = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return t("admin.groups.imagePricing.notConfigured");
  }
  const price = Number(value);
  if (!Number.isFinite(price) || price < 0) {
    return t("admin.groups.imagePricing.notConfigured");
  }
  return `$${price.toFixed(6).replace(/0+$/, "").replace(/\.$/, "")}`;
};

const formatVideoPricePreview = (value: number | string | null | undefined) => {
  if (value === null || value === undefined || value === "") {
    return t("admin.groups.videoPricing.notConfigured");
  }
  const price = Number(value);
  if (!Number.isFinite(price) || price < 0) {
    return t("admin.groups.videoPricing.notConfigured");
  }
  return `$${price.toFixed(6).replace(/0+$/, "").replace(/\.$/, "")}`;
};

const buildImageFinalPricePreview = (form: ImagePricingFormState) => {
  const imageMultiplier = form.image_rate_independent
    ? normalizePreviewNumber(form.image_rate_multiplier, 1)
    : normalizePreviewNumber(form.rate_multiplier, 1);
  const multiplier = imageMultiplier;
  return imagePricingTiers.map((tier) => {
    const basePrice =
      parsePreviewPrice(form[tier.key]) ??
      getDefaultImagePreviewPrice(form.platform, tier.key);
    return {
      label: tier.label,
      value: basePrice !== null
        ? formatImagePricePreview(basePrice * multiplier)
        : t("admin.groups.imagePricing.notConfigured"),
    };
  });
};

const buildVideoFinalPricePreview = (form: VideoPricingFormState) => {
  const multiplier = form.video_rate_independent
    ? normalizePreviewNumber(form.video_rate_multiplier, 1)
    : normalizePreviewNumber(form.rate_multiplier, 1);
  return videoPricingTiers.map((tier) => {
    const basePrice =
      parsePreviewPrice(form[tier.key]) ??
      getDefaultVideoPreviewPrice(form.platform, tier.key);
    return {
      label: tier.label,
      value: basePrice !== null
        ? formatVideoPricePreview(basePrice * multiplier)
        : t("admin.groups.videoPricing.notConfigured"),
    };
  });
};

const createImageFinalPricePreview = computed(() =>
  buildImageFinalPricePreview(createForm),
);
const editImageFinalPricePreview = computed(() =>
  buildImageFinalPricePreview(editForm),
);
const createVideoFinalPricePreview = computed(() =>
  buildVideoFinalPricePreview(createForm),
);
const editVideoFinalPricePreview = computed(() =>
  buildVideoFinalPricePreview(editForm),
);

// Codex 网页搜索单次默认价（与后端 defaultWebSearchPricePerCall 一致，官方 $10/1000 次）
const DEFAULT_WEB_SEARCH_PRICE_PER_CALL = 0.01;

const buildWebSearchFinalPricePreview = (form: {
  web_search_price_per_call: number | string | null;
  rate_multiplier: number | string | null;
}) => {
  const basePrice =
    parsePreviewPrice(form.web_search_price_per_call) ??
    DEFAULT_WEB_SEARCH_PRICE_PER_CALL;
  const multiplier = normalizePreviewNumber(form.rate_multiplier, 1);
  return formatImagePricePreview(basePrice * multiplier);
};

const createWebSearchFinalPricePreview = computed(() =>
  buildWebSearchFinalPricePreview(createForm),
);
const editWebSearchFinalPricePreview = computed(() =>
  buildWebSearchFinalPricePreview(editForm),
);

const resetDisabledBatchImagePricing = (
  form: Pick<
    ImagePricingFormState,
    "platform" | "allow_image_generation" | "allow_batch_image_generation" | "batch_image_discount_multiplier" | "batch_image_hold_multiplier"
  >,
) => {
  if (form.platform !== "gemini" || !form.allow_image_generation) {
    form.allow_batch_image_generation = false;
  }
  if (!form.allow_batch_image_generation) {
    form.batch_image_discount_multiplier = 0.5;
    form.batch_image_hold_multiplier = 0.6;
  }
};

const deleteConfirmMessage = computed(() => {
// 鏍规嵁鍒嗙粍绫诲瀷杩斿洖涓嶅悓鐨勫垹闄ょ‘璁ゆ秷鎭?const deleteConfirmMessage = computed(() => {
  if (!deletingGroup.value) {
    return "";
  }
  if (deletingGroup.value.subscription_type === "subscription") {
    return t("admin.groups.deleteConfirmSubscription", {
      name: deletingGroup.value.name,
    });
  }
  return t("admin.groups.deleteConfirm", { name: deletingGroup.value.name });
});

const loadLiveCapability = async () => {
  if (liveCapability.value) return liveCapability.value;
  if (!liveCapabilityRequest) {
    liveCapabilityRequest = adminAPI.groups
      .getLiveCapability()
      .catch(() => ({ supported: false }))
      .finally(() => {
        liveCapabilityRequest = null;
      });
  }
  liveCapability.value = await liveCapabilityRequest;
  return liveCapability.value ?? { supported: false };
};

const toggleLive = async (target: "create" | "edit") => {
  const form = target === "create" ? createForm : editForm;
  if (form.allow_live) {
    form.allow_live = false;
    return;
  }
  const capability = await loadLiveCapability();
  if (capability.supported) {
    form.allow_live = true;
    return;
  }
  pendingLiveForm.value = target;
};

const acknowledgeUnsupportedLive = () => {
  pendingLiveForm.value = null;
};

const cancelUnsupportedLive = () => {
  pendingLiveForm.value = null;
};

const loadGroups = async () => {
  if (abortController) {
    abortController.abort();
  }
  const currentController = new AbortController();
  abortController = currentController;
  const { signal } = currentController;
  loading.value = true;
  try {
    const response = await adminAPI.groups.list(
      pagination.page,
      pagination.page_size,
      {
        platform: (filters.platform as GroupPlatform) || undefined,
        status: filters.status as any,
        is_exclusive: filters.is_exclusive
          ? filters.is_exclusive === "true"
          : undefined,
        search: searchQuery.value.trim() || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order,
      },
      { signal },
    );
    if (signal.aborted) return;
    groups.value = response.items;
    pagination.total = response.total;
    pagination.pages = response.pages;
    if (hasVisibleUsageSummaryConsumer.value) {
      loadUsageSummary();
    } else {
      usageLoading.value = false;
    }
    if (hasVisibleCapacityColumn.value) {
      loadCapacitySummary();
    }
  } catch (error: any) {
    if (
      signal.aborted ||
      error?.name === "AbortError" ||
      error?.code === "ERR_CANCELED"
    ) {
      return;
    }
    appStore.showError(t("admin.groups.failedToLoad"));
    console.error("Error loading groups:", error);
  } finally {
    if (abortController === currentController && !signal.aborted) {
      loading.value = false;
    }
  }
};

const formatCost = (cost: number): string => {
  if (cost >= 1000) return cost.toFixed(0);
  if (cost >= 100) return cost.toFixed(1);
  return cost.toFixed(2);
};

const formatUsd = (cost: number): string => formatCurrencyAmount(cost, 2);

const formatDisplayCost = (cost: number): string => `${getDisplayCurrencySymbol()}${formatCost(cost)}`;

const getQuotaUsageClass = (used: number, limit: number) => {
  if (!Number.isFinite(limit) || limit <= 0) {
    return "font-medium text-gray-600 dark:text-gray-300";
  }
  const ratio = used / limit;
  if (ratio >= 1) return "font-medium text-red-600 dark:text-red-400";
  if (ratio >= 0.8) return "font-medium text-amber-600 dark:text-amber-400";
  return "font-medium text-emerald-600 dark:text-emerald-400";
};

const loadUsageSummary = async () => {
  if (!hasVisibleUsageSummaryConsumer.value) {
    usageLoading.value = false;
    return;
  }
  usageLoading.value = true;
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const data = await adminAPI.groups.getUsageSummary(tz);
    const map = new Map<number, { today_cost: number; total_cost: number; real_today_cost?: number; real_total_cost?: number }>();
    for (const item of data) {
      map.set(item.group_id, {
        today_cost: item.today_cost,
        total_cost: item.total_cost,
        real_today_cost: item.real_today_cost,
        real_total_cost: item.real_total_cost,
      });
    }
    usageMap.value = map;
  } catch (error) {
    console.error("Error loading group usage summary:", error);
  } finally {
    usageLoading.value = false;
  }
};

const loadCapacitySummary = async () => {
  if (!hasVisibleCapacityColumn.value) {
    return;
  }
  try {
    const data = await adminAPI.groups.getCapacitySummary();
    const map = new Map<
      number,
      {
        concurrencyUsed: number;
        concurrencyMax: number;
        sessionsUsed: number;
        sessionsMax: number;
        rpmUsed: number;
        rpmMax: number;
      }
    >();
    for (const item of data) {
      map.set(item.group_id, {
        concurrencyUsed: item.concurrency_used,
        concurrencyMax: item.concurrency_max,
        sessionsUsed: item.sessions_used,
        sessionsMax: item.sessions_max,
        rpmUsed: item.rpm_used,
        rpmMax: item.rpm_max,
      });
    }
    capacityMap.value = map;
  } catch (error) {
    console.error("Error loading group capacity summary:", error);
  }
};

let searchTimeout: ReturnType<typeof setTimeout>;
const handleSearch = () => {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(() => {
    pagination.page = 1;
    loadGroups();
  }, 300);
};

const handlePageChange = (page: number) => {
  pagination.page = page;
  loadGroups();
};

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize;
  pagination.page = 1;
  loadGroups();
};

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key;
  sortState.sort_order = order;
  pagination.page = 1;
  loadGroups();
};

const openCreateModal = () => {
  showCreateModal.value = true;
  loadModelsListCandidates("create", 0, createForm.platform);
};

const closeCreateModal = () => {
  showCreateModal.value = false;
  createPlatformSearchQuery.value = "";
  createModelRoutingRules.value.forEach((rule) => {
    accountSearchRunner.clearKey(getCreateRuleSearchKey(rule));
  });
  clearAllAccountSearchState();
  createForm.name = "";
  createForm.description = "";
  createForm.platform = "anthropic";
  createForm.rate_multiplier = 1.0;
  createForm.is_exclusive = false;
  createForm.subscription_type = "standard";
  createForm.daily_limit_usd = null;
  createForm.weekly_limit_usd = null;
  createForm.monthly_limit_usd = null;
  createForm.custom_limit_hours = 0;
  createForm.custom_limit_usd = null;
  createForm.allow_image_generation = false;
  createForm.allow_batch_image_generation = false;
  createForm.image_rate_independent = false;
  createForm.image_rate_multiplier = 1;
  createForm.batch_image_discount_multiplier = 0.5;
  createForm.batch_image_hold_multiplier = 0.6;
  createForm.image_price_1k = null;
  createForm.image_price_2k = null;
  createForm.image_price_4k = null;
  createForm.video_rate_independent = false;
  createForm.video_rate_multiplier = 1;
  createForm.video_price_480p = null;
  createForm.video_price_720p = null;
  createForm.video_price_1080p = null;
  createForm.web_search_price_per_call = null;
  createForm.peak_rate_enabled = false;
  createForm.peak_start = "";
  createForm.peak_end = "";
  createForm.peak_rate_multiplier = 1.0;
  createForm.peak_rate_windows = [];
  createForm.profit_control_enabled = false;
  createForm.profit_min_margin_percent = 0;
  createForm.profit_safety_buffer_percent = 0;
  createForm.claude_code_only = false;
  createForm.fallback_group_id = null;
  createForm.fallback_group_id_on_invalid_request = null;
  resetMessagesDispatchFormState(createForm);
  createForm.allow_live = false;
  createForm.require_oauth_only = false;
  createForm.require_privacy_set = false;
  createForm.supported_model_scopes = ["claude", "gemini_text", "gemini_image"];
  createForm.mcp_xml_inject = true;
  createForm.copy_accounts_from_group_ids = [];
  createForm.rpm_limit = 0;
  createForm.newapi_style_interface_enabled = false;
  createForm.max_reasoning_effort = "";
  createForm.reasoning_effort_mappings = [];
  createReasoningEffortPolicyRef.value?.resetValidation();
  resetModelsListState(createModelsListState);
  createModelRoutingRules.value = [];
};

const normalizeOptionalLimit = (
  value: number | string | null | undefined,
): number | null => {
  if (value === null || value === undefined) {
    return null;
  }

  if (typeof value === "string") {
    const trimmed = value.trim();
    if (!trimmed) {
      return null;
    }
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
  }

  return Number.isFinite(value) && value > 0 ? value : null;
};

const MAX_CUSTOM_LIMIT_HOURS = 87600;

const normalizeOptionalHours = (
  value: number | string | null | undefined,
): number => {
  if (value === null || value === undefined || value === "") {
    return 0;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }
  return Math.min(Math.floor(parsed), MAX_CUSTOM_LIMIT_HOURS);
};

const addPeakRateWindow = (windows: PeakRateWindowForm[]) => {
  if (!appendPeakRateWindow(windows)) {
    appStore.showError(t("admin.groups.peakRate.errors.maxWindows"));
  }
};

const showPeakRateError = (key: string) => {
  appStore.showError(t(`admin.groups.peakRate.errors.${key}`));
};

// 利润控制表单辅助（换算与校验逻辑见 groupsProfitControl.ts，便于单测）。
const percentToDecimal = profitPercentToDecimal;
const decimalToPercent = profitDecimalToPercent;

const validateProfitControlForm = (form: ProfitControlFormState): boolean => {
  const errorKey = validateProfitControlFormState(form);
  if (errorKey) {
    appStore.showError(t(`admin.groups.profitControl.${errorKey}`));
    return false;
  }
  return true;
};

const handleCreateGroup = async () => {
  if (!createForm.name.trim()) {
    appStore.showError(t("admin.groups.nameRequired"));
    return;
  }
  const peakPayload = buildPeakRatePayload(createForm, showPeakRateError);
  if (!peakPayload) return;
  if (
    supportsReasoningEffortPolicyPlatform(createForm.platform) &&
    createReasoningEffortPolicyRef.value &&
    !createReasoningEffortPolicyRef.value.validate()
  ) {
    return;
  }
  if (!validateProfitControlForm(createForm)) {
    return;
  }
  submitting.value = true;
  try {
    const requestData = {
    // 鏋勫缓璇锋眰鏁版嵁锛屽寘鍚ā鍨嬭矾鐢遍厤缃?    const requestData = {
      ...createForm,
      daily_limit_usd: normalizeOptionalLimit(
        createForm.daily_limit_usd as number | string | null,
      ),
      weekly_limit_usd: normalizeOptionalLimit(
        createForm.weekly_limit_usd as number | string | null,
      ),
      monthly_limit_usd: normalizeOptionalLimit(
        createForm.monthly_limit_usd as number | string | null,
      ),
      custom_limit_hours: normalizeOptionalHours(createForm.custom_limit_hours),
      custom_limit_usd: normalizeOptionalLimit(
        createForm.custom_limit_usd as number | string | null,
      ),
      model_routing: convertRoutingRulesToApiFormat(
        createModelRoutingRules.value,
      ),
      models_list_config: buildModelsListConfig(createModelsListState),
      supported_model_scopes: normalizeSupportedModelScopesForPlatform(
        createForm.platform,
        createForm.supported_model_scopes,
      ),
      messages_dispatch_model_config:
        createForm.platform === "openai"
          ? messagesDispatchFormStateToConfig({
              allow_messages_dispatch: createForm.allow_messages_dispatch,
              opus_mapped_model: createForm.opus_mapped_model,
              sonnet_mapped_model: createForm.sonnet_mapped_model,
              haiku_mapped_model: createForm.haiku_mapped_model,
              exact_model_mappings: createForm.exact_model_mappings,
            })
          : undefined,
      reasoning_effort_mappings: reasoningEffortMappingsToAPI(
        createForm.reasoning_effort_mappings,
      ),
      // 利润控制：界面百分比转小数提交；仅五个 token 平台可启用
      profit_control_enabled:
        isProfitControlPlatform(createForm.platform) &&
        createForm.profit_control_enabled,
      profit_min_margin: percentToDecimal(createForm.profit_min_margin_percent),
      profit_safety_buffer: percentToDecimal(
        createForm.profit_safety_buffer_percent,
      ),
    };
    // v-model.number 娓呯┖杈撳叆妗嗘椂浜х敓 ""锛岃浆涓?null 璁╁悗绔涓烘棤闄愬埗
    delete (requestData as Record<string, unknown>).profit_min_margin_percent;
    delete (requestData as Record<string, unknown>).profit_safety_buffer_percent;
    // v-model.number 清空输入框时产生 ""，转为 null 让后端设为无限制
    const emptyToNull = (v: any) => (v === "" ? null : v);
    requestData.daily_limit_usd = emptyToNull(requestData.daily_limit_usd);
    requestData.weekly_limit_usd = emptyToNull(requestData.weekly_limit_usd);
    requestData.monthly_limit_usd = emptyToNull(requestData.monthly_limit_usd);
    requestData.custom_limit_usd = emptyToNull(requestData.custom_limit_usd);
    requestData.image_rate_multiplier = normalizeRateMultiplier(
      requestData.image_rate_multiplier,
    );
    resetDisabledBatchImagePricing(requestData);
    requestData.batch_image_discount_multiplier = normalizeRateMultiplier(
      requestData.batch_image_discount_multiplier,
    );
    requestData.batch_image_hold_multiplier = normalizeRateMultiplier(
      requestData.batch_image_hold_multiplier,
    );
    requestData.video_rate_multiplier = normalizeRateMultiplier(
      requestData.video_rate_multiplier,
    );
    requestData.image_price_1k = emptyToNull(requestData.image_price_1k);
    requestData.image_price_2k = emptyToNull(requestData.image_price_2k);
    requestData.image_price_4k = emptyToNull(requestData.image_price_4k);
    requestData.video_price_480p = emptyToNull(requestData.video_price_480p);
    requestData.video_price_720p = emptyToNull(requestData.video_price_720p);
    requestData.video_price_1080p = emptyToNull(requestData.video_price_1080p);
    requestData.web_search_price_per_call = emptyToNull(
      requestData.web_search_price_per_call,
    );
    const requestPayload: CreateGroupRequest = {
      ...requestData,
      ...peakPayload,
    };
    await adminAPI.groups.create(requestPayload);
    appStore.showSuccess(t("admin.groups.groupCreated"));
    closeCreateModal();
    loadGroups();
    // Only advance tour if active, on submit step, and creation succeeded
    if (onboardingStore.isCurrentStep('[data-tour="group-form-submit"]')) {
      onboardingStore.nextStep(500);
    }
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToCreate"),
    );
    console.error("Error creating group:", error);
    // Don't advance tour on error
  } finally {
    submitting.value = false;
  }
};

const handleEdit = async (group: AdminGroup) => {
  editingGroup.value = group;
  editForm.name = group.name;
  editForm.description = group.description || "";
  editForm.platform = group.platform;
  editForm.rate_multiplier = group.rate_multiplier;
  editForm.is_exclusive = group.is_exclusive;
  editForm.status = group.status;
  editForm.subscription_type = group.subscription_type || "standard";
  editForm.daily_limit_usd = group.daily_limit_usd;
  editForm.weekly_limit_usd = group.weekly_limit_usd;
  editForm.monthly_limit_usd = group.monthly_limit_usd;
  editForm.custom_limit_hours = group.custom_limit_hours ?? 0;
  editForm.custom_limit_usd = group.custom_limit_usd;
  editForm.allow_image_generation = group.allow_image_generation ?? false;
  editForm.allow_batch_image_generation =
    group.allow_batch_image_generation ?? false;
  editForm.image_rate_independent = group.image_rate_independent ?? false;
  editForm.image_rate_multiplier = group.image_rate_multiplier ?? 1;
  editForm.batch_image_discount_multiplier =
    group.batch_image_discount_multiplier ?? 0.5;
  editForm.batch_image_hold_multiplier = group.batch_image_hold_multiplier ?? 0.6;
  editForm.image_price_1k = group.image_price_1k;
  editForm.image_price_2k = group.image_price_2k;
  editForm.image_price_4k = group.image_price_4k;
  editForm.video_rate_independent = group.video_rate_independent ?? false;
  editForm.video_rate_multiplier = group.video_rate_multiplier ?? 1;
  editForm.video_price_480p = group.video_price_480p;
  editForm.video_price_720p = group.video_price_720p;
  editForm.video_price_1080p = group.video_price_1080p;
  editForm.web_search_price_per_call = group.web_search_price_per_call ?? null;
  editForm.peak_rate_enabled = group.peak_rate_enabled ?? false;
  editForm.peak_start = group.peak_start ?? "";
  editForm.peak_end = group.peak_end ?? "";
  editForm.peak_rate_multiplier = group.peak_rate_multiplier ?? 1.0;
  editForm.peak_rate_windows = peakRateWindowsFromGroup(group);
  editForm.profit_control_enabled = group.profit_control_enabled ?? false;
  editForm.profit_min_margin_percent = decimalToPercent(
    group.profit_min_margin ?? 0,
  );
  editForm.profit_safety_buffer_percent = decimalToPercent(
    group.profit_safety_buffer ?? 0,
  );
  editForm.claude_code_only = group.claude_code_only || false;
  editForm.fallback_group_id = group.fallback_group_id;
  editForm.fallback_group_id_on_invalid_request =
    group.fallback_group_id_on_invalid_request;
  const messagesDispatchFormState = messagesDispatchConfigToFormState(
    group.messages_dispatch_model_config,
  );
  editForm.allow_messages_dispatch =
    group.allow_messages_dispatch ||
    messagesDispatchFormState.allow_messages_dispatch;
  editForm.allow_live = group.allow_live ?? false;
  editForm.opus_mapped_model = messagesDispatchFormState.opus_mapped_model;
  editForm.sonnet_mapped_model = messagesDispatchFormState.sonnet_mapped_model;
  editForm.haiku_mapped_model = messagesDispatchFormState.haiku_mapped_model;
  editForm.exact_model_mappings =
    messagesDispatchFormState.exact_model_mappings;
  editForm.require_oauth_only = group.require_oauth_only ?? false;
  editForm.require_privacy_set = group.require_privacy_set ?? false;
  editForm.model_routing_enabled = group.model_routing_enabled || false;
  editForm.supported_model_scopes = group.supported_model_scopes || [
    "claude",
    "gemini_text",
    "gemini_image",
  ];
  editForm.mcp_xml_inject = group.mcp_xml_inject ?? true;
  editForm.copy_accounts_from_group_ids = [];
  editForm.rpm_limit = group.rpm_limit ?? 0;
  editForm.max_reasoning_effort = normalizeReasoningEffortForPlatform(
    group.platform,
    group.max_reasoning_effort,
  );
  editForm.reasoning_effort_mappings = reasoningEffortMappingsToRows(
    group.reasoning_effort_mappings,
    group.platform,
  );
  resetModelsListState(editModelsListState, group.models_list_config);
  // 鍔犺浇妯″瀷璺敱瑙勫垯锛堝紓姝ュ姞杞借处鍙峰悕绉帮級
  editForm.newapi_style_interface_enabled =
    group.newapi_style_interface_enabled ?? false;
  editModelRoutingRules.value = await convertApiFormatToRoutingRules(
    group.model_routing,
  );
  loadModelsListCandidates("edit", group.id, group.platform);
  showEditModal.value = true;
};

const closeEditModal = () => {
  editModelRoutingRules.value.forEach((rule) => {
    accountSearchRunner.clearKey(getEditRuleSearchKey(rule));
  });
  clearAllAccountSearchState();
  showEditModal.value = false;
  editingGroup.value = null;
  editForm.max_reasoning_effort = "";
  editForm.reasoning_effort_mappings = [];
  editReasoningEffortPolicyRef.value?.resetValidation();
  editModelRoutingRules.value = [];
  editForm.copy_accounts_from_group_ids = [];
  editForm.peak_rate_enabled = false;
  editForm.peak_start = "";
  editForm.peak_end = "";
  editForm.peak_rate_multiplier = 1.0;
  editForm.peak_rate_windows = [];
  editForm.profit_control_enabled = false;
  editForm.profit_min_margin_percent = 0;
  editForm.profit_safety_buffer_percent = 0;
  editForm.video_rate_independent = false;
  editForm.video_rate_multiplier = 1;
  editForm.video_price_480p = null;
  editForm.video_price_720p = null;
  editForm.video_price_1080p = null;
  editForm.web_search_price_per_call = null;
  resetMessagesDispatchFormState(editForm);
  editForm.allow_live = false;
  resetModelsListState(editModelsListState);
};

const handleUpdateGroup = async () => {
  if (!editingGroup.value) return;
  if (!editForm.name.trim()) {
    appStore.showError(t("admin.groups.nameRequired"));
    return;
  }
  const peakPayload = buildPeakRatePayload(editForm, showPeakRateError);
  if (!peakPayload) return;
  if (
    supportsReasoningEffortPolicyPlatform(editForm.platform) &&
    editReasoningEffortPolicyRef.value &&
    !editReasoningEffortPolicyRef.value.validate()
  ) {
    return;
  }
  if (!validateProfitControlForm(editForm)) {
    return;
  }

  submitting.value = true;
  try {
    // 杞崲 fallback_group_id: null -> 0 (鍚庣浣跨敤 0 琛ㄧず娓呴櫎)
    const payload = {
      ...editForm,
      daily_limit_usd: normalizeOptionalLimit(
        editForm.daily_limit_usd as number | string | null,
      ),
      weekly_limit_usd: normalizeOptionalLimit(
        editForm.weekly_limit_usd as number | string | null,
      ),
      monthly_limit_usd: normalizeOptionalLimit(
        editForm.monthly_limit_usd as number | string | null,
      ),
      custom_limit_hours: normalizeOptionalHours(editForm.custom_limit_hours),
      custom_limit_usd: normalizeOptionalLimit(
        editForm.custom_limit_usd as number | string | null,
      ),
      fallback_group_id:
        editForm.fallback_group_id === null ? 0 : editForm.fallback_group_id,
      fallback_group_id_on_invalid_request:
        editForm.fallback_group_id_on_invalid_request === null
          ? 0
          : editForm.fallback_group_id_on_invalid_request,
      model_routing: convertRoutingRulesToApiFormat(
        editModelRoutingRules.value,
      ),
      models_list_config: buildModelsListConfig(editModelsListState),
      supported_model_scopes: normalizeSupportedModelScopesForPlatform(
        editForm.platform,
        editForm.supported_model_scopes,
      ),
      messages_dispatch_model_config:
        editForm.platform === "openai"
          ? messagesDispatchFormStateToConfig({
              allow_messages_dispatch: editForm.allow_messages_dispatch,
              opus_mapped_model: editForm.opus_mapped_model,
              sonnet_mapped_model: editForm.sonnet_mapped_model,
              haiku_mapped_model: editForm.haiku_mapped_model,
              exact_model_mappings: editForm.exact_model_mappings,
            })
          : undefined,
      reasoning_effort_mappings: reasoningEffortMappingsToAPI(
        editForm.reasoning_effort_mappings,
      ),
      // 利润控制：界面百分比转小数提交；仅五个 token 平台可启用
      profit_control_enabled:
        isProfitControlPlatform(editForm.platform) &&
        editForm.profit_control_enabled,
      profit_min_margin: percentToDecimal(editForm.profit_min_margin_percent),
      profit_safety_buffer: percentToDecimal(
        editForm.profit_safety_buffer_percent,
      ),
    };
    // v-model.number 娓呯┖杈撳叆妗嗘椂浜х敓 ""锛岃浆涓?null 璁╁悗绔涓烘棤闄愬埗
    delete (payload as Record<string, unknown>).profit_min_margin_percent;
    delete (payload as Record<string, unknown>).profit_safety_buffer_percent;
    // v-model.number 清空输入框时产生 ""，转为 null 让后端设为无限制
    const emptyToNull = (v: any) => (v === "" ? null : v);
    payload.daily_limit_usd = emptyToNull(payload.daily_limit_usd);
    payload.weekly_limit_usd = emptyToNull(payload.weekly_limit_usd);
    payload.monthly_limit_usd = emptyToNull(payload.monthly_limit_usd);
    payload.custom_limit_usd = emptyToNull(payload.custom_limit_usd);
    payload.image_rate_multiplier = normalizeRateMultiplier(
      payload.image_rate_multiplier,
    );
    resetDisabledBatchImagePricing(payload);
    payload.batch_image_discount_multiplier = normalizeRateMultiplier(
      payload.batch_image_discount_multiplier,
    );
    payload.batch_image_hold_multiplier = normalizeRateMultiplier(
      payload.batch_image_hold_multiplier,
    );
    payload.video_rate_multiplier = normalizeRateMultiplier(
      payload.video_rate_multiplier,
    );
    const emptyPriceToClear = (v: any) => (v === "" || v === null ? -1 : v);
    payload.image_price_1k = emptyPriceToClear(payload.image_price_1k);
    payload.image_price_2k = emptyPriceToClear(payload.image_price_2k);
    payload.image_price_4k = emptyPriceToClear(payload.image_price_4k);
    payload.video_price_480p = emptyPriceToClear(payload.video_price_480p);
    payload.video_price_720p = emptyPriceToClear(payload.video_price_720p);
    payload.video_price_1080p = emptyPriceToClear(payload.video_price_1080p);
    payload.web_search_price_per_call = emptyPriceToClear(
      payload.web_search_price_per_call,
    );
    const requestPayload: UpdateGroupRequest = { ...payload, ...peakPayload };
    await adminAPI.groups.update(editingGroup.value.id, requestPayload);
    appStore.showSuccess(t("admin.groups.groupUpdated"));
    closeEditModal();
    loadGroups();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToUpdate"),
    );
    console.error("Error updating group:", error);
  } finally {
    submitting.value = false;
  }
};

const addCreateMessagesDispatchMapping = () => {
  createForm.exact_model_mappings.push({ claude_model: "", target_model: "" });
};

const removeCreateMessagesDispatchMapping = (
  row: MessagesDispatchMappingRow,
) => {
  const index = createForm.exact_model_mappings.indexOf(row);
  if (index !== -1) {
    createForm.exact_model_mappings.splice(index, 1);
  }
};

const addEditMessagesDispatchMapping = () => {
  editForm.exact_model_mappings.push({ claude_model: "", target_model: "" });
};

const removeEditMessagesDispatchMapping = (row: MessagesDispatchMappingRow) => {
  const index = editForm.exact_model_mappings.indexOf(row);
  if (index !== -1) {
    editForm.exact_model_mappings.splice(index, 1);
  }
};

const handleRateMultipliers = (group: AdminGroup) => {
  rateMultipliersGroup.value = group;
  showRateMultipliersModal.value = true;
};

const handleRPMOverrides = (group: AdminGroup) => {
  rpmOverridesGroup.value = group;
  showRPMOverridesModal.value = true;
};

const handleDuplicate = async (group: AdminGroup) => {
  if (duplicatingGroupIds.has(group.id)) return;

  duplicatingGroupIds.add(group.id);
  try {
    const duplicate = await adminAPI.groups.duplicate(group.id);
    appStore.showSuccess(
      t("admin.groups.duplicateSuccess", { name: duplicate.name }),
    );
    await loadGroups();
  } catch (error: unknown) {
    appStore.showError(
      extractApiErrorMessage(error, t("admin.groups.duplicateFailed")),
    );
  } finally {
    duplicatingGroupIds.delete(group.id);
  }
};

const compositeRouteMatchLabel = (matchType: CompositeRouteMatchType) =>
  compositeRouteMatchOptions.value.find((option) => option.value === matchType)
    ?.label || matchType;

const formatCompositeEndpoint = (endpoint: CompositeRouteEndpoint) =>
  compositeRouteEndpointOptions.value.find((option) => option.value === endpoint)
    ?.label || endpoint;

const formatCompositePlatform = (platform: string) => {
  if (!platform) return "—";
  return t(`admin.groups.platforms.${platform}`);
};

const compositeRouteSourceLabel = (source: string) => {
  if (source === "route") return t("admin.groups.compositeRoutes.sources.route");
  if (source === "detector") {
    return t("admin.groups.compositeRoutes.sources.detector");
  }
  return source || "—";
};

const resetCompositeRouteForm = () => {
  compositeRouteEditingId.value = null;
  compositeRouteForm.public_model = "";
  compositeRouteForm.match_type = "exact";
  compositeRouteForm.target_platform = "openai";
  compositeRouteForm.upstream_model = "";
  compositeRouteForm.endpoint = "any";
  compositeRouteForm.priority = 100;
  compositeRouteForm.enabled = true;
  compositeRouteForm.notes = "";
};

const toCompositeRouteInput = (): CompositeModelRouteInput => ({
  public_model: compositeRouteForm.public_model.trim(),
  match_type: compositeRouteForm.match_type,
  target_platform: compositeRouteForm.target_platform,
  upstream_model: compositeRouteForm.upstream_model.trim(),
  endpoint: compositeRouteForm.endpoint,
  priority: Number(compositeRouteForm.priority) || 100,
  enabled: compositeRouteForm.enabled,
  notes: compositeRouteForm.notes.trim(),
});

const loadCompositeRoutes = async () => {
  if (!compositeRoutesGroup.value) return;
  compositeRoutesLoading.value = true;
  try {
    const routes = await adminAPI.groups.listCompositeRoutes(
      compositeRoutesGroup.value.id,
    );
    compositeRoutes.value = routes.sort((a, b) => {
      if (a.priority !== b.priority) return a.priority - b.priority;
      return a.id - b.id;
    });
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail ||
        error.response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToLoad"),
    );
    console.error("Error loading composite routes:", error);
  } finally {
    compositeRoutesLoading.value = false;
  }
};

const handleCompositeRoutes = async (group: AdminGroup) => {
  compositeRoutesGroup.value = group;
  compositePreviewModel.value = "";
  compositePreviewEndpoint.value = "any";
  compositePreviewDecision.value = null;
  resetCompositeRouteForm();
  showCompositeRoutesModal.value = true;
  await loadCompositeRoutes();
};

const closeCompositeRoutesModal = () => {
  showCompositeRoutesModal.value = false;
  compositeRoutesGroup.value = null;
  compositeRoutes.value = [];
  compositePreviewDecision.value = null;
  resetCompositeRouteForm();
};

const editCompositeRoute = (route: CompositeModelRoute) => {
  compositeRouteEditingId.value = route.id;
  compositeRouteForm.public_model = route.public_model;
  compositeRouteForm.match_type = route.match_type;
  compositeRouteForm.target_platform = route.target_platform;
  compositeRouteForm.upstream_model = route.upstream_model;
  compositeRouteForm.endpoint = route.endpoint;
  compositeRouteForm.priority = route.priority || 100;
  compositeRouteForm.enabled = route.enabled;
  compositeRouteForm.notes = route.notes || "";
};

const saveCompositeRoute = async () => {
  if (!compositeRoutesGroup.value) return;
  if (!compositeRouteForm.public_model.trim()) {
    appStore.showError(t("admin.groups.compositeRoutes.publicModelRequired"));
    return;
  }
  compositeRouteSaving.value = true;
  try {
    const payload = toCompositeRouteInput();
    if (compositeRouteEditingId.value) {
      await adminAPI.groups.updateCompositeRoute(
        compositeRoutesGroup.value.id,
        compositeRouteEditingId.value,
        payload,
      );
      appStore.showSuccess(t("admin.groups.compositeRoutes.routeUpdated"));
    } else {
      await adminAPI.groups.createCompositeRoute(
        compositeRoutesGroup.value.id,
        payload,
      );
      appStore.showSuccess(t("admin.groups.compositeRoutes.routeCreated"));
    }
    resetCompositeRouteForm();
    await loadCompositeRoutes();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail ||
        error.response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToSave"),
    );
    console.error("Error saving composite route:", error);
  } finally {
    compositeRouteSaving.value = false;
  }
};

const deleteCompositeRoute = async (route: CompositeModelRoute) => {
  if (!compositeRoutesGroup.value) return;
  if (!window.confirm(t("admin.groups.compositeRoutes.deleteConfirm"))) return;
  try {
    await adminAPI.groups.deleteCompositeRoute(
      compositeRoutesGroup.value.id,
      route.id,
    );
    if (compositeRouteEditingId.value === route.id) {
      resetCompositeRouteForm();
    }
    appStore.showSuccess(t("admin.groups.compositeRoutes.routeDeleted"));
    await loadCompositeRoutes();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail ||
        error.response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToDelete"),
    );
    console.error("Error deleting composite route:", error);
  }
};

const previewCompositeRoute = async () => {
  if (!compositeRoutesGroup.value || !compositePreviewModel.value.trim()) {
    return;
  }
  compositePreviewLoading.value = true;
  try {
    compositePreviewDecision.value = await adminAPI.groups.previewCompositeRoute(
      compositeRoutesGroup.value.id,
      {
        model: compositePreviewModel.value.trim(),
        endpoint: compositePreviewEndpoint.value,
      },
    );
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail ||
        error.response?.data?.message ||
        t("admin.groups.compositeRoutes.failedToPreview"),
    );
    console.error("Error previewing composite route:", error);
  } finally {
    compositePreviewLoading.value = false;
  }
};

const handleDelete = (group: AdminGroup) => {
  deletingGroup.value = group;
  showDeleteDialog.value = true;
};

const confirmDelete = async () => {
  if (!deletingGroup.value) return;

  try {
    await adminAPI.groups.delete(deletingGroup.value.id);
    appStore.showSuccess(t("admin.groups.groupDeleted"));
    showDeleteDialog.value = false;
    deletingGroup.value = null;
    loadGroups();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToDelete"),
    );
    console.error("Error deleting group:", error);
  }
};

watch(
// 鐩戝惉 subscription_type 鍙樺寲锛氳闃呮ā寮忔椂 is_exclusive 榛樿涓?true锛涙爣鍑嗕綑棰濆垎缁勪篃淇濈暀楂樺嘲閰嶇疆銆?watch(
  () => createForm.subscription_type,
  (newVal) => {
    if (newVal === "subscription") {
      createForm.is_exclusive = true;
      createForm.fallback_group_id_on_invalid_request = null;
    }
  },
);

watch(
  () => createForm.peak_rate_enabled,
  (enabled) => {
    if (enabled && createForm.peak_rate_windows.length === 0) {
      addPeakRateWindow(createForm.peak_rate_windows);
    }
  },
);

watch(
  () => editForm.peak_rate_enabled,
  (enabled) => {
    if (enabled && editForm.peak_rate_windows.length === 0) {
      addPeakRateWindow(editForm.peak_rate_windows);
    }
  },
);

watch(
  () => createForm.platform,
  (newVal) => {
    if (!["anthropic", "antigravity"].includes(newVal)) {
      createForm.fallback_group_id_on_invalid_request = null;
    }
    if (newVal !== "openai") {
      resetMessagesDispatchFormState(createForm);
      createForm.allow_live = false;
    }
    if (!isProfitControlPlatform(newVal)) {
      createForm.profit_control_enabled = false;
      createForm.profit_min_margin_percent = 0;
      createForm.profit_safety_buffer_percent = 0;
    }
    createForm.max_reasoning_effort = normalizeReasoningEffortForPlatform(
      newVal,
      createForm.max_reasoning_effort,
    );
    createForm.reasoning_effort_mappings = reasoningEffortMappingsToRows(
      reasoningEffortMappingsToAPI(createForm.reasoning_effort_mappings),
      newVal,
    );
    createReasoningEffortPolicyRef.value?.resetValidation();
    if (!["openai", "antigravity", "anthropic", "gemini"].includes(newVal)) {
      createForm.require_oauth_only = false;
      createForm.require_privacy_set = false;
    }
    resetDisabledBatchImagePricing(createForm);
    resetModelsListState(createModelsListState);
    loadModelsListCandidates("create", 0, newVal);
  },
);

watch(
  () => createForm.allow_image_generation,
  () => {
    resetDisabledBatchImagePricing(createForm);
  },
);

watch(
  () => createForm.allow_batch_image_generation,
  () => {
    resetDisabledBatchImagePricing(createForm);
  },
);

watch(
  () => editForm.platform,
  (newVal) => {
    if (!["anthropic", "antigravity"].includes(newVal)) {
      editForm.fallback_group_id_on_invalid_request = null;
    }
    if (newVal !== "openai") {
      resetMessagesDispatchFormState(editForm);
      editForm.allow_live = false;
    }
    if (!isProfitControlPlatform(newVal)) {
      editForm.profit_control_enabled = false;
      editForm.profit_min_margin_percent = 0;
      editForm.profit_safety_buffer_percent = 0;
    }
    editForm.max_reasoning_effort = normalizeReasoningEffortForPlatform(
      newVal,
      editForm.max_reasoning_effort,
    );
    editForm.reasoning_effort_mappings = reasoningEffortMappingsToRows(
      reasoningEffortMappingsToAPI(editForm.reasoning_effort_mappings),
      newVal,
    );
    editReasoningEffortPolicyRef.value?.resetValidation();
    if (!["openai", "antigravity", "anthropic", "gemini"].includes(newVal)) {
      editForm.require_oauth_only = false;
      editForm.require_privacy_set = false;
    }
    resetDisabledBatchImagePricing(editForm);
    if (editingGroup.value) {
      resetModelsListState(editModelsListState, editForm.platform === editingGroup.value.platform ? editingGroup.value.models_list_config : undefined);
      loadModelsListCandidates("edit", editingGroup.value.id, newVal);
    }
  },
);

watch(
  () => editForm.allow_image_generation,
  () => {
    resetDisabledBatchImagePricing(editForm);
  },
);

watch(
  () => editForm.allow_batch_image_generation,
  () => {
    resetDisabledBatchImagePricing(editForm);
  },
);

// 点击外部关闭账号搜索下拉框
const handleClickOutside = (event: MouseEvent) => {
// 鐐瑰嚮澶栭儴鍏抽棴璐﹀彿鎼滅储涓嬫媺妗?const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement;
  // 妫€鏌ユ槸鍚︾偣鍑诲湪涓嬫媺妗嗘垨杈撳叆妗嗗唴
  if (!target.closest(".account-search-container")) {
    Object.keys(showAccountDropdown.value).forEach((key) => {
      showAccountDropdown.value[key] = false;
    });
  }
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false;
  }
};

// 打开排序弹窗
const openSortModal = async () => {
  try {
    // 鑾峰彇鎵€鏈夊垎缁勶紙涓嶅垎椤碉級
    const allGroups = await adminAPI.groups.getAll();
    // 鎸?sort_order 鎺掑簭
    sortableGroups.value = [...allGroups].sort(
      (a, b) => a.sort_order - b.sort_order,
    );
    showSortModal.value = true;
  } catch (error) {
    appStore.showError(t("admin.groups.failedToLoad"));
    console.error("Error loading groups for sorting:", error);
  }
};

// 关闭排序弹窗
const closeSortModal = () => {
  showSortModal.value = false;
  sortableGroups.value = [];
};

// 淇濆瓨鎺掑簭
const saveSortOrder = async () => {
  sortSubmitting.value = true;
  try {
    const updates = sortableGroups.value.map((g, index) => ({
      id: g.id,
      sort_order: index * 10,
    }));
    await adminAPI.groups.updateSortOrder(updates);
    appStore.showSuccess(t("admin.groups.sortOrderUpdated"));
    closeSortModal();
    loadGroups();
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail || t("admin.groups.failedToUpdateSortOrder"),
    );
    console.error("Error updating sort order:", error);
  } finally {
    sortSubmitting.value = false;
  }
};

onMounted(() => {
  loadGroups();
  void loadLiveCapability();
  loadModelsListCandidates("create", 0, createForm.platform);
  document.addEventListener("click", handleClickOutside);
  window.addEventListener("resize", hideGroupPeakRateTooltip);
  window.addEventListener("scroll", hideGroupPeakRateTooltip, true);
});

onUnmounted(() => {
  document.removeEventListener("click", handleClickOutside);
  window.removeEventListener("resize", hideGroupPeakRateTooltip);
  window.removeEventListener("scroll", hideGroupPeakRateTooltip, true);
  accountSearchRunner.clearAll();
  clearAllAccountSearchState();
});
</script>

<style scoped>
.platform-scroll-strip {
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.platform-scroll-strip::-webkit-scrollbar {
  display: none;
}
</style>

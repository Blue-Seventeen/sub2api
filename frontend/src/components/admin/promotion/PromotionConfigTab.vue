<template>
  <div class="space-y-6">
    <section class="promo-panel">
      <div class="mb-5 flex items-center gap-3">
        <Icon name="calendar" size="sm" class="text-cyan-300" />
        <div>
          <h3 class="text-lg font-semibold text-slate-100">激活与结款配置</h3>
          <p class="promo-section-note">默认按 {{ effectiveTimezone || 'Asia/Shanghai' }} 时区执行结算；固定时间聚合，当天生成待结算，审核一天后发放。</p>
        </div>
      </div>
      <div class="grid gap-4 xl:grid-cols-[1fr_1fr_1fr_240px]">
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">激活门槛 (USD)</span>
          <input v-model.number="settings.activation_threshold_amount" type="number" min="0" step="0.01" class="promo-input-dark" />
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">激活奖励 (USD)</span>
          <input v-model.number="settings.activation_bonus_amount" type="number" min="0" step="0.01" class="promo-input-dark" />
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">每日结款时间</span>
          <button type="button" class="promo-input-dark flex w-full items-center justify-between" @click="openSettlementTimeDialog">
            <span>{{ settings.daily_settlement_time || '00:00' }}</span>
            <Icon name="chevronDown" size="sm" class="text-slate-500" />
          </button>
        </label>
        <div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3 xl:self-end">
          <div class="flex min-h-[52px] items-center justify-between gap-4">
            <div class="min-w-0">
              <div class="text-xs uppercase tracking-[0.24em] text-slate-500">是否启用自动日结</div>
              <div class="mt-1.5 text-sm text-slate-300">{{ settings.settlement_enabled ? '已启用' : '已关闭' }}</div>
            </div>
            <Toggle v-model="settings.settlement_enabled" />
          </div>
        </div>
      </div>
    </section>

    <section class="promo-panel-soft">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h3 class="promo-section-title">
            <Icon name="cog" size="sm" class="text-cyan-300" />
            等级与返利配置
          </h3>
          <p class="promo-section-note">等级、返利、结款与海报文案都在这里统一维护。删除中间等级后，后续等级会自动前移，保持级别连续。</p>
        </div>
        <div class="flex flex-wrap gap-3">
          <button type="button" class="promo-btn promo-btn-secondary" @click="addLevel">
            <Icon name="plus" size="sm" />
            添加新等级
          </button>
          <button type="button" class="promo-btn promo-btn-primary" :disabled="saving" @click="saveConfig">
            <Icon :name="saving ? 'refresh' : 'checkCircle'" size="sm" :class="saving ? 'animate-spin' : ''" />
            {{ saving ? '保存中...' : '保存配置' }}
          </button>
        </div>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex items-center gap-3">
        <Icon name="badge" size="sm" class="text-cyan-300" />
        <div>
          <h3 class="text-lg font-semibold text-slate-100">等级与返利规则表</h3>
          <p class="promo-section-note">总提成列依据一级返利与二级返利计算而成；若删除中间推广级别，后面的级别会自动降级，保持级别连贯。</p>
        </div>
      </div>

      <div v-if="!levels.length" class="promo-empty-state">当前还没有配置任何等级。</div>
      <div v-else class="overflow-x-auto rounded-2xl border border-white/10">
        <table class="min-w-full text-sm text-slate-200">
          <thead class="promo-table-head">
            <tr>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">推广级别</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">等级名称</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">升级所需激活人数</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">一级返利（%）</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">二级返利（%）</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">总提成（%）</th>
              <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">是否启用</th>
              <th class="px-4 py-3 text-center text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">操作</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(level, index) in levels" :key="`level-${index}`">
              <tr class="promo-table-row">
                <td class="px-4 py-4">
                  <span class="rounded-full bg-gradient-to-r from-cyan-500 to-indigo-500 px-3 py-1 text-sm font-semibold text-white">Lv{{ index + 1 }}</span>
                </td>
                <td class="px-4 py-4 font-medium text-white">{{ level.level_name || `Lv${index + 1}` }}</td>
                <td class="px-4 py-4 text-slate-300">{{ level.required_activated_invites }}</td>
                <td class="px-4 py-4 text-cyan-300">{{ rate(level.direct_rate) }}%</td>
                <td class="px-4 py-4 text-purple-300">{{ rate(level.indirect_rate) }}%</td>
                <td class="px-4 py-4">
                  <span class="rounded-full border border-cyan-400/20 bg-cyan-500/10 px-3 py-1 text-xs font-medium text-cyan-300">
                    {{ rate(totalRate(level)) }}%
                  </span>
                </td>
                <td class="px-4 py-4">
                  <span class="rounded-full px-3 py-1 text-xs font-medium" :class="level.enabled ? 'border border-emerald-400/20 bg-emerald-500/10 text-emerald-300' : 'border border-white/10 bg-white/5 text-slate-400'">
                    {{ level.enabled ? '启用中' : '已停用' }}
                  </span>
                </td>
                <td class="px-4 py-4">
                  <div class="flex items-center justify-center gap-2">
                    <button type="button" class="promo-btn promo-btn-secondary px-3 py-2 text-xs" @click="toggleLevelEditor(index)">
                      <Icon :name="expandedLevelIndex === index ? 'chevronUp' : 'edit'" size="sm" />
                      {{ expandedLevelIndex === index ? '收起' : '编辑' }}
                    </button>
                    <button type="button" class="promo-btn promo-btn-danger px-3 py-2 text-xs" @click="removeLevel(index)">
                      <Icon name="trash" size="sm" />
                      删除
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="expandedLevelIndex === index" class="bg-slate-950/30">
                <td colspan="8" class="px-4 py-4">
                  <div class="grid gap-4 xl:grid-cols-[1.2fr_1fr_1fr_1fr_auto]">
                    <label class="space-y-2">
                      <span class="text-xs uppercase tracking-[0.24em] text-slate-500">等级名称</span>
                      <input v-model="level.level_name" type="text" class="promo-input-dark" placeholder="例如：推广达人" />
                    </label>
                    <label class="space-y-2">
                      <span class="text-xs uppercase tracking-[0.24em] text-slate-500">升级所需已激活人数</span>
                      <input v-model.number="level.required_activated_invites" type="number" min="0" class="promo-input-dark" />
                      <p v-if="index === 0" class="text-xs text-slate-500">Lv1 通常为默认等级，可设置为 0。</p>
                    </label>
                    <label class="space-y-2">
                      <span class="text-xs uppercase tracking-[0.24em] text-slate-500">一级返利 (%)</span>
                      <input v-model.number="level.direct_rate" type="number" step="0.1" min="0" class="promo-input-dark" />
                    </label>
                    <label class="space-y-2">
                      <span class="text-xs uppercase tracking-[0.24em] text-slate-500">二级返利 (%)</span>
                      <input v-model.number="level.indirect_rate" type="number" step="0.1" min="0" class="promo-input-dark" />
                    </label>
                    <div class="flex items-center justify-between gap-4 rounded-2xl border border-white/10 bg-white/5 p-4">
                      <div>
                        <div class="text-xs uppercase tracking-[0.24em] text-slate-500">是否启用</div>
                        <div class="mt-2 text-sm text-slate-300">{{ level.enabled ? '启用中' : '已停用' }}</div>
                      </div>
                      <Toggle v-model="level.enabled" />
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex items-center justify-between gap-3">
        <div class="flex items-center gap-3">
          <Icon name="document" size="sm" class="text-cyan-300" />
          <div>
            <h3 class="text-lg font-semibold text-slate-100">链接与海报配置</h3>
            <p class="promo-section-note">支持上传海报 Logo（PNG / JPG / WEBP），前端会自动按合适尺寸放入海报中。</p>
          </div>
        </div>
        <button type="button" class="promo-btn promo-btn-secondary" @click="showPosterPreview = true">
          <Icon name="eye" size="sm" />
          预览海报
        </button>
      </div>
      <div class="grid gap-4 xl:grid-cols-[1.2fr_1fr_1fr]">
        <label class="space-y-2 xl:col-span-3">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">推广链接基础地址</span>
          <input v-model="settings.invite_base_url" type="text" class="promo-input-dark" placeholder="例如：https://example.com 或 http://127.0.0.1:8080" />
          <p class="text-xs text-slate-500">如果不填写，用户侧会回退到系统 frontend_url；填写后优先使用这里。</p>
        </label>

        <div class="space-y-2 xl:col-span-3">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">海报 Logo</span>
          <div class="flex flex-col gap-4 rounded-2xl border border-white/10 bg-slate-950/40 p-4 lg:flex-row lg:items-center">
            <div class="flex h-20 w-20 items-center justify-center overflow-hidden rounded-2xl border border-white/10 bg-white/5">
              <img v-if="settings.poster_logo_url" :src="settings.poster_logo_url" alt="poster logo" class="h-full w-full object-contain" />
              <Icon v-else name="document" size="lg" class="text-slate-500" />
            </div>
            <div class="flex flex-wrap gap-3">
              <label class="promo-btn promo-btn-secondary cursor-pointer">
                <Icon name="upload" size="sm" />
                上传 Logo
                <input type="file" accept="image/png,image/jpeg,image/jpg,image/webp" class="hidden" @change="handlePosterLogoUpload" />
              </label>
              <button type="button" class="promo-btn promo-btn-danger" :disabled="!settings.poster_logo_url" @click="settings.poster_logo_url = ''">
                <Icon name="trash" size="sm" />
                移除 Logo
              </button>
            </div>
          </div>
        </div>

        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">海报标题</span>
          <input v-model="settings.poster_title" type="text" class="promo-input-dark" placeholder="例如：Sub2API" />
        </label>
        <label class="space-y-2 xl:col-span-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">海报主标题</span>
          <input v-model="settings.poster_headline" type="text" class="promo-input-dark" placeholder="例如：邀请好友，一起把消费返佣赚回来" />
        </label>
        <label class="space-y-2 xl:col-span-3">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">海报描述</span>
          <textarea v-model="settings.poster_description" rows="3" class="promo-input-dark min-h-[96px]" placeholder="例如：一级返利 + 二级返利 + 激活奖励，统一结算到真实余额。"></textarea>
        </label>
        <label class="space-y-2 xl:col-span-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">扫码提示</span>
          <input v-model="settings.poster_scan_hint" type="text" class="promo-input-dark" placeholder="例如：扫码快速注册" />
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">海报标签</span>
          <input v-model="posterTagsInput" type="text" class="promo-input-dark" placeholder="多个标签请用中文逗号、英文逗号或换行分隔" />
        </label>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex items-center justify-between gap-3">
        <div>
          <h3 class="promo-section-title">
            <Icon name="document" size="sm" class="text-cyan-300" />
            用户侧佣金规则模板
          </h3>
          <p class="promo-section-note">这里只保留用户实际能看到的 3 段规则文案，保存后会实时反映到推广中心。</p>
        </div>
        <span class="promo-chip">模板实时预览</span>
      </div>

      <div class="rounded-2xl border border-white/10 bg-white/5 p-4">
        <div class="mb-3 text-sm font-medium text-slate-200">可用占位符</div>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="placeholder in placeholders"
            :key="placeholder"
            type="button"
            class="rounded-full border border-cyan-400/20 bg-cyan-500/10 px-3 py-1 text-xs text-cyan-200 transition hover:bg-cyan-500/15"
            @click="insertPlaceholder(placeholder)"
          >
            {{ placeholder }}
          </button>
        </div>
      </div>

      <div class="mt-5 grid gap-4 xl:grid-cols-3">
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">激活奖励模板</span>
          <textarea v-model="settings.rule_activation_template" rows="4" class="promo-input-dark min-h-[120px]" @focus="activeTemplateField = 'rule_activation_template'"></textarea>
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">一级返利模板</span>
          <textarea v-model="settings.rule_direct_template" rows="4" class="promo-input-dark min-h-[120px]" @focus="activeTemplateField = 'rule_direct_template'"></textarea>
        </label>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">二级返利模板</span>
          <textarea v-model="settings.rule_indirect_template" rows="4" class="promo-input-dark min-h-[120px]" @focus="activeTemplateField = 'rule_indirect_template'"></textarea>
        </label>
      </div>
    </section>

    <section class="promo-panel">
      <div class="mb-5 flex items-center gap-3">
        <Icon name="eye" size="sm" class="text-cyan-300" />
        <div>
          <h3 class="text-lg font-semibold text-slate-100">模板预览</h3>
          <p class="promo-section-note">预览中占位符会按当前配置即时替换，用于模拟用户推广中心里的真实展示效果。</p>
        </div>
      </div>
      <div class="space-y-4">
        <article class="flex items-start gap-4 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-emerald-500/20">
            <Icon name="userPlus" size="sm" class="text-emerald-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-emerald-400">激活奖励</div>
            <p class="text-sm leading-7 text-slate-400">{{ previews.activation }}</p>
          </div>
        </article>

        <article class="flex items-start gap-4 rounded-xl border border-cyan-500/20 bg-cyan-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-cyan-500/20">
            <Icon name="chartBar" size="sm" class="text-cyan-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-cyan-400">一级返利</div>
            <p class="text-sm leading-7 text-slate-400">{{ previews.direct }}</p>
          </div>
        </article>

        <article class="flex items-start gap-4 rounded-xl border border-purple-500/20 bg-purple-500/10 p-4">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-purple-500/20">
            <Icon name="users" size="sm" class="text-purple-400" />
          </div>
          <div>
            <div class="mb-1 font-medium text-purple-400">二级返利</div>
            <p class="text-sm leading-7 text-slate-400">{{ previews.indirect }}</p>
          </div>
        </article>
      </div>
    </section>

    <BaseDialog
      :show="showSettlementTimeDialog"
      title="选择每日结款时间"
      width="narrow"
      content-class="border-white/10 bg-slate-900 text-slate-100"
      header-class="border-white/10 bg-slate-900"
      body-class="bg-slate-900"
      footer-class="border-white/10 bg-slate-900"
      @close="showSettlementTimeDialog = false"
    >
      <div class="space-y-5">
        <section class="rounded-2xl border border-white/10 bg-slate-950/60 p-4 text-sm leading-7 text-slate-300">
          请选择每天自动结款的时间（24 小时制）。
        </section>
        <label class="space-y-2">
          <span class="text-xs uppercase tracking-[0.24em] text-slate-500">结款时间</span>
          <input
            ref="settlementTimeInputRef"
            v-model="settlementDraftTime"
            type="time"
            step="60"
            class="promo-input-dark"
            @click="openNativeTimePicker"
          />
        </label>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="preset in settlementQuickOptions"
            :key="preset"
            type="button"
            class="rounded-full border px-3 py-1.5 text-xs transition"
            :class="settlementDraftTime === preset ? 'border-cyan-400/30 bg-cyan-500/10 text-cyan-300' : 'border-white/10 bg-white/5 text-slate-400 hover:border-cyan-400/20 hover:text-slate-200'"
            @click="settlementDraftTime = preset"
          >
            {{ preset }}
          </button>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="promo-btn promo-btn-secondary" @click="showSettlementTimeDialog = false">取消</button>
          <button type="button" class="promo-btn promo-btn-primary" @click="applySettlementTime">确定</button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showPosterPreview"
      title="海报预览"
      width="extra-wide"
      content-class="border-white/10 bg-slate-900 text-slate-100"
      header-class="border-white/10 bg-slate-900"
      body-class="bg-slate-900"
      footer-class="border-white/10 bg-slate-900"
      @close="showPosterPreview = false"
    >
      <div class="flex justify-center">
        <div :style="posterPreviewFrameStyle">
          <div :style="posterPreviewScaledPosterStyle">
            <div :id="posterPreviewElementId" ref="posterPreviewRef" xmlns="http://www.w3.org/1999/xhtml" :style="posterStyle">
              <div :style="posterGlowStyle"></div>
              <div :style="posterGlowAltStyle"></div>
              <div :style="posterHeaderStyle">
                <div v-if="settings.poster_logo_url" :style="posterLogoImageWrapStyle">
                  <img :src="settings.poster_logo_url" alt="poster logo" :style="posterLogoImageStyle" />
                </div>
                <div v-else :style="posterLogoStyle">{{ posterLogoText }}</div>
                <div>
                  <div :style="posterEyebrowStyle">PROMOTION CENTER</div>
                  <div data-poster-role="title" :style="posterTitleStyle">{{ settings.poster_title || 'Sub2API' }}</div>
                </div>
              </div>
              <div :style="posterHeadlineStyle">{{ settings.poster_headline || '邀请好友，一起把消费返佣赚回来' }}</div>
              <div :style="posterSubtitleStyle">{{ settings.poster_description || '一级返利 + 二级返利 + 激活奖励，统一结算到真实余额。' }}</div>
              <div :style="posterMetaRowStyle">
                <span v-for="tag in previewPosterTags" :key="tag" data-poster-role="chip" :style="posterMetaChipStyle">{{ tag }}</span>
              </div>
              <div :style="posterQrWrapStyle">
                <img v-if="previewQrCodeDataUrl" :src="previewQrCodeDataUrl" alt="promotion qrcode" :style="posterQrStyle" />
              </div>
              <div :style="posterHintStyle">{{ settings.poster_scan_hint || '扫码快速注册' }}</div>
              <div :style="posterCodeStyle">{{ demoInviteCode }}</div>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="promo-btn promo-btn-secondary" @click="showPosterPreview = false">关闭预览</button>
          <button type="button" class="promo-btn promo-btn-primary" :disabled="posterPreviewDownloading" @click="downloadPreviewPoster">
            <Icon :name="posterPreviewDownloading ? 'refresh' : 'download'" size="sm" :class="posterPreviewDownloading ? 'animate-spin' : ''" />
            {{ posterPreviewDownloading ? '生成中...' : '下载海报 PNG' }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import QRCode from 'qrcode'
import { saveAs } from 'file-saver'
import { adminPromotionAPI, type PromotionLevelConfig, type PromotionSettingsConfig } from '@/api/admin/promotion'
import { useAppStore } from '@/stores'
import { renderElementToPngBlobById } from '@/utils/renderHtmlToPng'

const appStore = useAppStore()
const saving = ref(false)
const effectiveTimezone = ref('')
const expandedLevelIndex = ref<number | null>(null)
const showSettlementTimeDialog = ref(false)
const settlementDraftTime = ref('00:00')
const settlementTimeInputRef = ref<HTMLInputElement | null>(null)
const showPosterPreview = ref(false)
const previewQrCodeDataUrl = ref('')
const posterPreviewRef = ref<HTMLElement | null>(null)
const posterPreviewElementId = 'promotion-admin-poster-preview'
const posterPreviewDownloading = ref(false)
const demoInviteCode = '174914E97ACEBFD1'

const settings = reactive<PromotionSettingsConfig>({
  activation_threshold_amount: 5,
  activation_bonus_amount: 0,
  daily_settlement_time: '00:00',
  settlement_enabled: true,
  rule_activation_template: '',
  rule_direct_template: '',
  rule_indirect_template: '',
  rule_level_summary_template: '',
  invite_base_url: '',
  poster_logo_url: '',
  poster_title: '',
  poster_headline: '',
  poster_description: '',
  poster_scan_hint: '',
  poster_tags: []
})

const levels = ref<PromotionLevelConfig[]>([])
const posterTagsInput = ref('')

type RuleTemplateField =
  | 'rule_activation_template'
  | 'rule_direct_template'
  | 'rule_indirect_template'

const activeTemplateField = ref<RuleTemplateField | null>('rule_activation_template')

const placeholders = [
  '{{ACTIVATION_THRESHOLD}}',
  '{{ACTIVATION_BONUS}}',
  '{{CURRENT_DIRECT_RATE}}',
  '{{CURRENT_INDIRECT_RATE}}',
  '{{CURRENT_SECOND_LEVEL_RATE}}',
  '{{CURRENT_TOTAL_RATE}}',
  '{{SETTLEMENT_TIME}}'
]

const settlementQuickOptions = ['00:00', '09:00', '12:00', '18:00', '23:55']

const previewPosterTags = computed(() => parsePosterTagsInput(posterTagsInput.value).length ? parsePosterTagsInput(posterTagsInput.value) : ['真实消费返佣', '次日结算', '唯一推广码'])
const posterLogoText = computed(() => ((settings.poster_title || 'SU').replace(/\s+/g, '').slice(0, 2) || 'SU').toUpperCase())
const previewInviteLink = computed(() => {
  const base = (settings.invite_base_url || 'http://127.0.0.1:8080').replace(/\/$/, '')
  return `${base}/register?ref=${demoInviteCode}`
})

const previews = computed(() => {
  const baseLevel = levels.value.find(item => item.enabled !== false) || levels.value[0]
  const values: Record<string, string> = {
    '{{ACTIVATION_THRESHOLD}}': rate(settings.activation_threshold_amount),
    '{{ACTIVATION_BONUS}}': rate(settings.activation_bonus_amount),
    '{{CURRENT_DIRECT_RATE}}': baseLevel ? rate(baseLevel.direct_rate) : '0',
    '{{CURRENT_INDIRECT_RATE}}': baseLevel ? rate(baseLevel.indirect_rate) : '0',
    '{{CURRENT_SECOND_LEVEL_RATE}}': baseLevel ? rate(baseLevel.indirect_rate) : '0',
    '{{CURRENT_TOTAL_RATE}}': baseLevel ? rate(totalRate(baseLevel)) : '0',
    '{{SETTLEMENT_TIME}}': settings.daily_settlement_time || '00:00'
  }
  const render = (text: string) =>
    Object.entries(values).reduce((acc, [key, value]) => acc.replace(new RegExp(key.replace(/[{}]/g, '\\$&'), 'g'), value), text || '')
  return {
    activation: render(settings.rule_activation_template),
    direct: render(settings.rule_direct_template),
    indirect: render(settings.rule_indirect_template)
  }
})

watch(
  () => previewInviteLink.value,
  async (link) => {
    if (!link) {
      previewQrCodeDataUrl.value = ''
      return
    }
    previewQrCodeDataUrl.value = await QRCode.toDataURL(link, { width: 320, margin: 1 })
  },
  { immediate: true }
)

onMounted(() => {
  void loadConfig()
})

async function loadConfig() {
  try {
    const response = await adminPromotionAPI.getConfig()
    Object.assign(settings, response.data.settings)
    posterTagsInput.value = (response.data.settings.poster_tags || []).join('，')
    levels.value = normalizeLevels((response.data.levels || []).map((item, index) => ({
      ...item,
      sort_order: item.sort_order || index + 1,
      enabled: item.enabled !== false
    })))
    effectiveTimezone.value = response.data.effective_timezone || ''
    expandedLevelIndex.value = null
  } catch (error) {
    console.error('Failed to load promotion config:', error)
    appStore.showError('加载推广配置失败')
  }
}

function normalizeLevels(list: PromotionLevelConfig[]) {
  return [...list]
    .sort((a, b) => (a.sort_order || a.level_no) - (b.sort_order || b.level_no))
    .map((item, index) => ({
      ...item,
      level_no: index + 1,
      sort_order: index + 1,
      enabled: item.enabled !== false
    }))
}

function addLevel() {
  const nextLevelNo = levels.value.length + 1
  levels.value = normalizeLevels([
    ...levels.value,
    {
      level_no: nextLevelNo,
      level_name: `Lv${nextLevelNo}`,
      required_activated_invites: 0,
      direct_rate: 0,
      indirect_rate: 0,
      sort_order: nextLevelNo,
      enabled: true
    }
  ])
  expandedLevelIndex.value = levels.value.length - 1
}

function removeLevel(index: number) {
  const next = [...levels.value]
  next.splice(index, 1)
  levels.value = normalizeLevels(next)
  if (expandedLevelIndex.value != null && expandedLevelIndex.value >= levels.value.length) {
    expandedLevelIndex.value = levels.value.length ? levels.value.length - 1 : null
  }
}

function toggleLevelEditor(index: number) {
  expandedLevelIndex.value = expandedLevelIndex.value === index ? null : index
}

function totalRate(level: PromotionLevelConfig) {
  return Number(level.direct_rate || 0) + Number(level.indirect_rate || 0)
}

async function openSettlementTimeDialog() {
  settlementDraftTime.value = normalizeSettlementTime(settings.daily_settlement_time || '00:00')
  showSettlementTimeDialog.value = true
  await nextTick()
  openNativeTimePicker()
}

function applySettlementTime() {
  settings.daily_settlement_time = normalizeSettlementTime(settlementDraftTime.value)
  showSettlementTimeDialog.value = false
}

function openNativeTimePicker() {
  const input = settlementTimeInputRef.value as (HTMLInputElement & { showPicker?: () => void }) | null
  if (!input) return
  input.focus()
  try {
    input.showPicker?.()
  } catch {
    // 某些浏览器需要再次点击输入框才会弹出原生时间选择器
  }
}

function normalizeSettlementTime(value: string) {
  return /^\d{2}:\d{2}$/.test(value) ? value : '00:00'
}

function insertPlaceholder(placeholder: string) {
  const field = activeTemplateField.value
  if (!field) return
  settings[field] = `${settings[field] || ''}${placeholder}`
}

async function saveConfig() {
  saving.value = true
  try {
    levels.value = normalizeLevels(levels.value)
    const payload = {
      settings: {
        activation_threshold_amount: Number(settings.activation_threshold_amount) || 0,
        activation_bonus_amount: Number(settings.activation_bonus_amount) || 0,
        daily_settlement_time: settings.daily_settlement_time || '00:00',
        settlement_enabled: settings.settlement_enabled,
        rule_activation_template: settings.rule_activation_template,
        rule_direct_template: settings.rule_direct_template,
        rule_indirect_template: settings.rule_indirect_template,
        rule_level_summary_template: settings.rule_level_summary_template,
        invite_base_url: settings.invite_base_url?.trim() || '',
        poster_logo_url: settings.poster_logo_url?.trim() || '',
        poster_title: settings.poster_title?.trim() || '',
        poster_headline: settings.poster_headline?.trim() || '',
        poster_description: settings.poster_description?.trim() || '',
        poster_scan_hint: settings.poster_scan_hint?.trim() || '',
        poster_tags: parsePosterTagsInput(posterTagsInput.value)
      },
      levels: levels.value.map((item, index) => ({
        ...item,
        level_no: index + 1,
        required_activated_invites: Number(item.required_activated_invites) || 0,
        direct_rate: Number(item.direct_rate) || 0,
        indirect_rate: Number(item.indirect_rate) || 0,
        sort_order: index + 1,
        enabled: item.enabled !== false
      }))
    }
    const response = await adminPromotionAPI.updateConfig(payload)
    Object.assign(settings, response.data.settings)
    posterTagsInput.value = (response.data.settings.poster_tags || []).join('，')
    levels.value = normalizeLevels((response.data.levels || []).map((item, index) => ({
      ...item,
      sort_order: item.sort_order || index + 1,
      enabled: item.enabled !== false
    })))
    effectiveTimezone.value = response.data.effective_timezone || ''
    expandedLevelIndex.value = null
    appStore.showSuccess('推广配置已保存')
  } catch (error) {
    console.error('Failed to save promotion config:', error)
    appStore.showError('保存推广配置失败')
  } finally {
    saving.value = false
  }
}

function rate(value?: number) {
  const formatted = Number(value || 0).toFixed(4)
  return formatted.replace(/\.?0+$/, '')
}

function parsePosterTagsInput(value: string) {
  return value
    .split(/[\n,，]/)
    .map(item => item.trim())
    .filter(Boolean)
    .slice(0, 6)
}

function handlePosterLogoUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (!file.type.startsWith('image/')) {
    appStore.showError('请上传 PNG / JPG / WEBP 图片')
    input.value = ''
    return
  }
  const reader = new FileReader()
  reader.onload = (e) => {
    settings.poster_logo_url = String(e.target?.result || '')
  }
  reader.onerror = () => {
    appStore.showError('读取图片失败')
  }
  reader.readAsDataURL(file)
  input.value = ''
}

async function downloadPreviewPoster() {
  if (!posterPreviewRef.value) return
  posterPreviewDownloading.value = true
  try {
    const blob = await renderElementToPngBlobById({
      elementId: posterPreviewElementId,
      width: 420,
      height: 620,
      pixelRatio: 2,
      backgroundColor: null
    })
    if (!blob) throw new Error('Poster export failed')
    saveAs(blob, 'promotion-poster-preview.png')
  } catch (error) {
    console.error('Failed to export poster preview:', error)
    appStore.showError('??????')
  } finally {
    posterPreviewDownloading.value = false
  }
}

const posterPreviewScale = 0.72
const posterPreviewFrameStyle = {
  width: '360px',
  height: '520px',
  padding: '20px',
  background: 'linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%)',
  borderRadius: '20px',
  boxShadow: '0 20px 60px -15px rgba(0, 0, 0, 0.6), inset 0 1px 0 rgba(255, 255, 255, 0.1)',
  boxSizing: 'border-box',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  overflow: 'hidden'
} as const

const posterPreviewScaledPosterStyle = {
  width: '420px',
  height: '620px',
  transform: `scale(${posterPreviewScale})`,
  transformOrigin: 'center center',
  borderRadius: '28px',
  overflow: 'hidden',
  boxShadow: '0 25px 80px -20px rgba(0, 0, 0, 0.8)',
  flexShrink: '0'
} as const

const posterStyle = {
  position: 'relative',
  overflow: 'hidden',
  width: '420px',
  height: '620px',
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
  marginTop: '2px'
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
  marginTop: '-6px'
} as const
</script>

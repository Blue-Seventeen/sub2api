# README-CUSTOM

## 管理端在线更新源

- 管理端在线更新检查必须继续跟踪官方 `Wei-Shaw/sub2api` 的 release。
- 这个检查只作为官方上游发布时间信号，方便运维确认官方 Sub2API 何时发布新版本。
- 部署脚本可以默认使用本 fork 进行构建和部署，但不得让 `SUB2API_REPO` 等部署仓库配置影响管理端在线更新源。
- `backend/internal/service/update_service.go` 必须保持 `defaultUpdateRepo = "Wei-Shaw/sub2api"`。

> 用途：记录本 fork 的**本地定制功能点**、**高风险文件**、**上游同步保护规则**。
> 任何 AI / 人类在执行 `sync upstream`、合并 upstream tag、处理冲突、重跑代码生成之前，必须先阅读本文件。
> 本文件是当前唯一的自定义能力保护文档。

## 0. 上游同步硬规则

1. **默认保护本地能力**：遇到冲突时，不允许为了“快速同步 upstream”直接全量 `theirs` 覆盖本 fork 的兼容、计费、推广、使用记录、自动运维、代理池、备份等核心改动。
2. **先分析再合并**：必须先列出 upstream 改动点、本地定制点、冲突文件、行为风险，再决定吸收/裁剪/保留。
3. **Affiliate 禁止并入**：upstream 的 Affiliate / 邀请返利模块对本 fork 属于冗余功能，默认禁止重新引入。保留本 fork 自研的 Promotion / 推广中心。
4. **使用记录页属于核心资产**：`/admin/usage`、`/usage`、`/key-usage` 及其统计、筛选、成本、模型映射、首 token、耗时字段不得被 upstream 简化或覆盖。
5. **兼容链路优先保真**：Claude Code / Codex / Cherry Studio 与 GPT / Claude / GLM / Kimi 的兼容改动，不得被单纯平台同步覆盖。
6. **不可判定就停下询问**：若无法确定某个 upstream 改动是否会破坏本地能力，必须停止并向维护者确认。

## 1. 审计基线

| 项目 | 当前约定 |
|---|---|
| 当前主线 | `dev` |
| 当前 upstream 基线 | 已同步到 `v0.1.134`，`backend/cmd/server/VERSION` 已对齐 `0.1.134` |
| 早期 fork 保护基线 | `2b72deb8fd45dc3a526bda2299b16df8d471107c` |
| 部署策略 | `dev` 是真实可部署主线；`sub2api-custom-localtest` 仅用于本地测试 |
| 架构原则 | 保留 Sub2API 的 Account / Group / Channel / 调度 / sticky / failover / billing，渐进吸收协议优先兼容内核 |

## 2. 本 fork 必须保留的定制能力总表

| 分类 | 本地能力 | 保护原因 | 重点文件 / 页面 |
|---|---|---|---|
| 多上游兼容 | GLM / DeepSeek / 豆包 / Qwen / Kimi 等兼容平台扩展 | 这是“任意客户端 × 任意上游”的基础 | `backend/internal/service/compatible_*`, `backend/internal/pkg/apicompat/*` |
| Claude Code × Kimi | Moonshot/Kimi native-first、relay fallback、chat fallback、tool restore、tokenizer usage 修复 | 解决 Claude Code 中 Kimi 工具调用/usage/stream 不稳定 | `compatible_gateway_service.go`, `compatible_platform_moonshot.go`, `compatible_claude_kimi_tool_restore.go`, `moonshot_tokenizer.go` |
| Kimi 官方通道 | Moonshot 官方兼容平台、默认官方 base URL、模型/平台展示补齐 | 保留 Kimi 官方线路，不被 generic compatible 平台覆盖 | `compatible_platform_moonshot.go`, `compatible_gateway_service.go`, `PlatformTypeBadge.vue`, `platformColors.ts` |
| Claude Code × GPT | `/v1/messages` 到 OpenAI 链路诊断、benchmark、OpenAI passthrough instructions 修复 | 用于定位 GPT 在 Claude Code 下的兼容性与速度问题 | `openai_gateway_handler.go`, `openai_gateway_messages.go`, `openai_gateway_service.go` |
| GLM 计费 | GLM token usage fallback，避免 token=0 | 防止计费异常和免费跑 | `compatible_usage_estimate.go`, `billing_service.go` |
| Cherry Studio 图片 | GPT-images / New-API upstream 图片响应归一 | 保证 Cherry Studio 生图链路稳定 | `openai_images.go`, `apicompat/*`, image normalizer 相关逻辑 |
| Codex 兼容 | Responses / WS / tool id / previous_response_id / model mapping 保护 | 支撑 Codex 客户端接入非 GPT 上游的后续演进 | `gateway_handler_responses.go`, `openai_gateway_service.go`, `apicompat/*` |
| 计费倍率 | 分组倍率、用户分组倍率、统一倍率、渠道级定价优先 | 影响真实收入和配额扣减 | `billing_service.go`, `pricing_service.go`, `api_key_service.go`, group/admin 相关 handler |
| 使用记录 | `/admin/usage`、用户使用记录、Key 使用记录的增强展示与统计 | 这是排障、审计、成本核算核心页面 | 见第 3 节 |
| 推广中心 | 自研 Promotion / 推广中心 / 推广后台 / 返佣统计 | 替代 upstream Affiliate，不可被覆盖 | `backend/internal/service/*promotion*`, `frontend/src/views/**/Promotion*.vue` |
| 自动运维 | 账号自动刷新、测试、恢复、删除、规则筛选 | 维护账号池稳定性 | `account_auto_ops*`, `proxy_auto_probe*` |
| 代理池 | 代理检测、成功队列、账号选择最优代理 | 提升上游请求成功率 | `proxy_*`, `account_proxy*`, `frontend` 代理管理页 |
| 订阅管理 | 兑换时刻滚动窗口、自定义小时限额、开始时间列、秒级时间展示、`starts_at` 排序与列设置持久化 | 订阅额度语义必须跟随兑换/续费生效时刻，且不能因长时间字段影响表格布局或中转链路 | `backend/internal/service/subscription_service.go`, `backend/internal/service/user_subscription.go`, `backend/internal/repository/user_subscription_repo.go`, `backend/migrations/145_subscription_windows_anchor_to_starts_at.sql`, `backend/migrations/146_subscription_custom_hour_limit.sql`, `frontend/src/views/admin/SubscriptionsView.vue`, `frontend/src/views/admin/GroupsView.vue`, `frontend/src/views/user/SubscriptionsView.vue`, `frontend/src/views/user/PaymentView.vue` |
| 设置增强 | 站点 Logo、自定义菜单、外链新页面打开、邀请码注册 HTML 提示 | 属于运营配置能力 | `setting_service.go`, `SettingsView.vue`, `AppSidebar.vue` |
| 分组平台搜索 | `/admin/groups` 创建分组平台选择增加与账号表单一致的模糊搜索 | 纯前端交互增强，不改变分组保存、调度、计费或平台语义 | `frontend/src/views/admin/GroupsView.vue` |
| 多机部署 | 定时备份本机开关 | 多机共库时由每台服务器本地文件决定是否执行定时备份，默认关闭 | `backup_service.go`, `backup_service_schedule_local_test.go` |
| OpenAI Fast/Flex Policy | `service_tier` 策略、默认空规则/pass、低价 tier 自动标准化、最终有效 tier 计费 | 吸收 upstream 能力但保持本 fork 不允许低价模式的运营约束 | `setting_service.go`, `openai_gateway_service.go`, `openai_fast_policy_test.go`, `SettingsView.vue` |
| Anthropic 缓存 TTL 注入 | 管理端可选开启 Anthropic OAuth/SetupToken 请求已有 ephemeral cache_control 强制 1h，默认关闭，usage 默认按 5m 回写 | 吸收 upstream 成本优化能力，同时避免默认改变现有请求与计费语义 | `setting_service.go`, `gateway_service.go`, `gateway_body_order_test.go`, `SettingsView.vue` |
| 请求体读失败观测 | 对 `unexpected EOF`、`context canceled`、`connection reset`、timeout、未知读失败分桶记录 | 不改变客户端错误文本，同时让 ops 能定位入口链路问题 | `request_body_read_error.go`, `ops_error_logger.go`, `ops_repo.go`, `OpsErrorDetailModal.vue` |
| Vertex Service Account | Gemini / Anthropic 通过 Vertex service account 认证，支持代理 token exchange 与模型预检 fallback | 扩展企业账号接入能力，保留现有 API key / proxy / fallback 行为 | `vertex_service_account.go`, `gemini_messages_compat_service.go`, `CreateAccountModal.vue`, `EditAccountModal.vue` |
| 热路径性能保护 | usage logging 队列、upstream HTTP client cache、API key rate-limit reset、usage 导出 COUNT 优化 | 降低高峰请求尾延迟和大表查询压力 | `usage_record_worker_pool.go`, `http_upstream.go`, `billing_cache_service.go`, `UsageView.vue` |
| 安全/隐私细化 | 调试 prompt 日志脱敏、新窗口 opener 隔离、compatible body limit 错误处理、备份 retention 删除失败保护 | 减少生产误配置泄露、tabnabbing、截断响应和 S3 孤儿对象风险 | `gateway_service.go`, `providerConfig.ts`, `compatible_gateway_service.go`, `backup_service.go` |
| 测试稳定性 | config 测试隔离、wire 生成检查 | 保证 dev 可部署 | `backend/internal/config/*`, `backend/cmd/server/wire_gen_test.go` |

## 3. 使用记录页面专项保护清单

> 这一块是本 fork 明确优化过的重点功能，后续同步 upstream 时必须重点保护。

### 3.1 页面范围

| 页面 | 路由 / 文件 | 说明 |
|---|---|---|
| 管理后台使用记录 | `/admin/usage` / `frontend/src/views/admin/UsageView.vue` | 管理员全局审计、成本、模型、端点、账号、用户维度分析 |
| 用户侧使用记录 | `/usage` / `frontend/src/views/user/UsageView.vue` | 用户查看自己的请求、消耗、模型、耗时等 |
| API Key 使用页 | `/key-usage` / `frontend/src/views/KeyUsageView.vue` | API Key 维度的用量查看 |
| 使用记录组件 | `frontend/src/components/admin/usage/*` | 筛选、表格、统计卡片、导出、清理弹窗 |
| 使用记录工具 | `frontend/src/utils/usage*.ts` | 请求类型、倍率、服务层级、价格展示、加载队列等辅助逻辑 |
| 使用记录 API | `frontend/src/api/usage.ts`, `frontend/src/api/admin/usage.ts` | 前端请求封装 |

### 3.2 必须保留的字段与能力

- 模型链路字段：
  - `requested_model`
  - `upstream_model`
  - `model_mapping_chain`
  - `billing_model`
  - `billing_model_source`
- 计费与成本字段：
  - `input_cost`
  - `output_cost`
  - `cache_read_cost`
  - `cache_creation_cost`
  - `image_output_cost`
  - `total_cost`
  - `actual_cost`
  - `real_actual_cost`
  - `account_stats_cost`
  - `rate_multiplier`
  - `account_rate_multiplier`
  - `unified_rate_multiplier`
  - `billing_type`
  - `billing_mode`
  - `billing_tier`
- 性能与排障字段：
  - `first_token_ms`
  - `duration_ms`
  - `reasoning_effort`
  - `inbound_endpoint`
  - `upstream_endpoint`
  - `request_type`
  - `user_agent`
  - `account_id`
  - `group_id`
  - `api_key_id`
- 展示/交互能力：
  - 成本 tooltip / 明细展示
  - 用户、API Key、账号、分组、请求类型、计费类型筛选
  - requested / upstream / mapping 模型分布切换
  - inbound / upstream / path 端点分布切换
  - token / actual cost 指标切换
  - CSV 导出字段完整保留
  - 管理端和用户端字段权限隔离，用户侧不得泄露账号内部成本倍率

### 3.3 后端保护点

- `backend/ent/schema/usage_log.go`
- `backend/internal/repository/usage_log_repo.go`
- `backend/internal/repository/usage_log_repo_request_type_test.go`
- `backend/internal/pkg/usagestats/usage_log_types.go`
- `backend/internal/handler/admin/dashboard_handler.go`
- `backend/internal/handler/user_handler.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/openai_gateway_service.go`
- `backend/internal/service/compatible_gateway_service.go`

同步 upstream 后，必须确认 usage log 写入与查询仍覆盖：模型映射、计费、成本、端点、首 token、耗时、reasoning、request type。

### 3.4 前端保护点

- `frontend/src/views/admin/UsageView.vue`
- `frontend/src/views/user/UsageView.vue`
- `frontend/src/views/KeyUsageView.vue`
- `frontend/src/components/admin/usage/UsageTable.vue`
- `frontend/src/components/admin/usage/UsageFilters.vue`
- `frontend/src/components/admin/usage/UsageStatsCards.vue`
- `frontend/src/components/admin/usage/UsageExportProgress.vue`
- `frontend/src/components/admin/usage/UsageCleanupDialog.vue`
- `frontend/src/utils/usagePricing.ts`
- `frontend/src/utils/usageRequestType.ts`
- `frontend/src/utils/usageRate.ts`
- `frontend/src/utils/usageServiceTier.ts`
- `frontend/src/utils/usageLoadQueue.ts`
- `frontend/src/i18n/locales/zh.ts`
- `frontend/src/i18n/locales/en.ts`

## 4. 兼容网关 / AI 客户端链路保护

### 4.1 功能点

- 扩展兼容平台支持：`GLM / DeepSeek / 豆包 / Qwen / Kimi`
- Anthropic Messages、OpenAI Responses、OpenAI Chat Completions、Images 之间的兼容转换
- Claude Code 检测、版本门禁、SSE 语义保护
- Kimi / Moonshot：native messages 优先，失败后 relay，再 fallback 到 chat
- Kimi tokenizer 本地估算输入 token，避免使用记录 `input_tokens=0`
- 兼容路径记录：`client_profile`、`compatibility_route`、`fallback_chain`、`upstream_transport`
- 流式首 token、总耗时、late usage 收尾统计
- 瞬时 `502/503/504/520~525` 同端点重试

### 4.2 重点文件

- `backend/internal/handler/compatibility_helper.go`
- `backend/internal/handler/compatible_gateway_handler.go`
- `backend/internal/service/compatibility_contract.go`
- `backend/internal/service/compatible_gateway_service.go`
- `backend/internal/service/compatible_platform_moonshot.go`
- `backend/internal/service/compatible_platform_patches.go`
- `backend/internal/service/compatible_claude_kimi_tool_restore.go`
- `backend/internal/service/compatible_usage_estimate.go`
- `backend/internal/service/moonshot_tokenizer.go`
- `backend/internal/service/tokenizer_assets/kimi_k2.tiktoken.model`
- `backend/internal/pkg/apicompat/*`

## 5. 推广中心与 Affiliate 裁剪规则

### 5.1 保留 Promotion

本 fork 已有自己的推广中心：

- 用户侧 `/promotion`
- 管理后台 `/admin/promotion`
- 推广链接 / 邀请码 / 团队贡献 / 佣金统计
- 明暗色主题适配
- 推广返佣只针对激活后用户
- 用户侧推广团队贡献按佣金统计

重点保护：

- `backend/internal/service/*promotion*`
- `backend/internal/repository/promotion_*`
- `backend/internal/handler/promotion_handler.go`
- `backend/internal/handler/admin/promotion_handler.go`
- `backend/internal/server/routes/promotion.go`
- `frontend/src/views/**/Promotion*.vue`
- `frontend/src/components/layout/AppSidebar.vue`

### 5.2 禁止重新吸收 Affiliate

upstream 的 Affiliate / 邀请返利模块属于冗余功能，后续同步 upstream 时默认排除：

- `backend/internal/service/affiliate_service.go`
- `backend/internal/repository/affiliate_repo.go`
- `/aff`
- `/aff/transfer`
- `affiliate_rebate_rate`
- `backend/migrations/*affiliate*`
- `frontend/src/views/user/AffiliateView.vue`
- `frontend/src/router/index.ts` 中的 `/affiliate`
- `frontend/src/components/layout/AppSidebar.vue` 中的 `nav.affiliate`

如果 upstream 后续修改 Affiliate，除非维护者明确要求，否则不并入。

## 6. 设置、支付、OAuth、运维能力保护

### 6.1 设置增强

- `/admin/settings` 站点 Logo
- 系统设置“在新页面打开”开关
- 自定义菜单开关
- 邀请码注册开启后，自定义 HTML 报错提示
- S3 备份配置
- 定时备份配置

重点文件：

- `backend/internal/service/setting_service.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/setting_handler.go`
- `backend/internal/handler/dto/settings.go`
- `frontend/src/views/admin/SettingsView.vue`
- `frontend/src/components/layout/AppSidebar.vue`

### 6.2 支付 / OAuth / 登录

- WeChat / OIDC / LinuxDo 等第三方登录兼容
- 支付恢复 / 回跳 / provider snapshot / visible method source
- Stripe 与易支付同时启用时按钮展示修复
- auth identity foundation / pending oauth / bind / unbind 流程

重点文件：

- `backend/internal/service/auth_service.go`
- `backend/internal/service/payment_fulfillment.go`
- `backend/internal/handler/payment_webhook_handler.go`
- `backend/internal/payment/provider/*`

### 6.3 自动运维 / 代理 / 通道监控

- 账号自动运维：刷新令牌、测试连接、恢复状态、删除账号
- 代理池自动检测、成功队列、账号选择最优代理
- `/admin/proxies` 代理管理增强：真实 proxy stats、实时出口账号数、自动检测增量刷新、延迟/实时使用排序持久化、Quick Add/BatchCreate/ImportData 五元组去重语义
- Clash 订阅托管代理：订阅保存在中央数据库，节点本地运行 mihomo runtime；托管代理仍通过 `proxy_id` 绑定账号，删除保护、计费、usage log、中转统计语义不变
- 自动代理粘性保持：仅对“自动选择最优代理”的账号生效，优先复用上次命中的可用代理；Redis / 观测写入失败必须 best-effort 降级，不得阻塞中转链路
- Available Channels 聚合视图
- Channel Monitor / Request Template / Rollup / 用户侧状态页

重点文件：

- `backend/internal/service/account_auto_ops*`
- `backend/internal/service/account_refresh_service.go`
- `backend/internal/service/proxy_*`
- `backend/internal/service/managed_proxy*`
- `backend/internal/service/proxy_active_usage.go`
- `backend/internal/service/proxy_stats*`
- `backend/internal/repository/proxy_stats_repo.go`
- `backend/internal/repository/proxy_sticky_store.go`
- `backend/internal/repository/proxy_subscription_repo.go`
- `backend/migrations/135_add_proxy_request_stats.sql`
- `backend/migrations/136_add_managed_proxy_subscriptions.sql`
- `backend/migrations/137_add_proxy_subscription_nodes.sql`
- `frontend/src/views/admin/ProxiesView.vue`
- `frontend/src/api/admin/proxies.ts`
- `frontend/src/utils/proxyBatchInput.ts`
- `frontend/src/components/admin/proxy/ImportDataModal.vue`
- `backend/internal/service/subscription_service.go`
- `backend/internal/service/user_subscription.go`
- `backend/internal/repository/user_subscription_repo.go`
- `backend/migrations/145_subscription_windows_anchor_to_starts_at.sql`
- `backend/migrations/146_subscription_custom_hour_limit.sql`
- `frontend/src/views/admin/SubscriptionsView.vue`
- `frontend/src/views/admin/GroupsView.vue`
- `frontend/src/views/user/SubscriptionsView.vue`
- `frontend/src/views/user/PaymentView.vue`
- `frontend/src/components/payment/SubscriptionPlanCard.vue`
- `frontend/src/views/KeyUsageView.vue`
- `backend/internal/service/channel_monitor_*`
- `backend/internal/service/channel_available.go`
- `backend/internal/repository/channel_monitor_*`
- `frontend/src/views/admin/ChannelMonitorView.vue`
- `frontend/src/views/user/AvailableChannelsView.vue`
- `frontend/src/views/user/ChannelStatusView.vue`

## 7. 备份与双机部署保护

- 多机器共用同一 PostgreSQL / Redis 时，`backup_schedule` 仍在数据库 settings 中保存 cron、保留天数、保留份数等共享参数。
- 定时备份启用状态不再走共享数据库，也不再走 Redis 锁；每台服务器只读取自己的本地文件 `backup_schedule.local.json`。
- 本地文件默认路径：`DATA_DIR/backup_schedule.local.json`；未设置 `DATA_DIR` 时优先使用 `/app/data/backup_schedule.local.json`，否则使用当前目录。
- 本地文件不存在或解析失败时默认视为未启用，避免新节点上线后自动重复跑备份。
- 升级注意：旧数据库里即使已有 `enabled=true`，升级后也必须在目标备份节点重新启用本机开关，否则定时备份不会自动执行。
- 只影响定时备份，不影响手动备份。

重点文件：

- `backend/internal/service/backup_service.go`
- `backend/internal/service/backup_service_schedule_local_test.go`
- `backend/internal/service/wire.go`
- `backend/cmd/server/wire_gen.go`

## 8. v0.1.120 同步新增保护点

### 8.1 OpenAI Fast/Flex Policy

- 默认配置必须保持 disabled / 空规则 / pass，不允许 upstream 默认 filter priority 改变现有请求行为。
- `normalizeOpenAIServiceTier` 允许 `priority`、`auto`、`default`、`scale` 等标准值；未知 tier 继续剥离。
- 本 fork 不允许命中低价模式：用户请求低价 `flex` 时必须自动调整为标准模式，不能向上游透传低价 tier。
- 策略过滤后必须使用“最终有效 `service_tier`”计费，避免上游按 standard/default 执行但账单仍按 priority 或其他 tier 计费。
- HTTP / WS / passthrough 路由需要保持兼容；filter / block 只允许在管理员显式配置规则后生效。

重点文件：

- `backend/internal/service/setting_service.go`
- `backend/internal/service/openai_gateway_service.go`
- `backend/internal/service/openai_fast_policy_test.go`
- `backend/internal/service/openai_passthrough_normalization_test.go`
- `backend/internal/service/openai_ws_v2/*`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/dto/settings.go`
- `frontend/src/views/admin/SettingsView.vue`

### 8.2 Request Body 读取失败内部观测

- 客户端响应必须保持兼容：非超限读取失败仍返回 `400 invalid_request_error` 与 `Failed to read request body`。
- `http.MaxBytesError` / body 超限仍保持现有 `413` 逻辑，不改 SDK 可见行为。
- 内部稳定分类包括：`request_body_unexpected_eof`、`request_body_context_canceled`、`request_body_connection_reset`、`request_body_timeout`、`request_body_read_error`。
- 应用日志和 ops 只记录分类、原始 error、path、method、content_length、transfer_encoding、user_agent、client_request_id，不记录请求体内容。
- `OpsErrorLoggerMiddleware` 需要把这类错误写入 `network_error_type`，并标记为 `network / client_request / client`。

重点文件：

- `backend/internal/handler/request_body_read_error.go`
- `backend/internal/handler/request_body_read_error_test.go`
- `backend/internal/pkg/httputil/body.go`
- `backend/internal/pkg/httputil/body_test.go`
- `backend/internal/handler/ops_error_logger.go`
- `backend/internal/repository/ops_repo.go`
- `backend/internal/repository/ops_repo_network_error_type_test.go`
- `frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue`

### 8.3 Vertex Service Account

- Gemini / Anthropic 兼容链路支持通过 Vertex service account 获取访问令牌。
- service account token exchange 必须继承账号代理配置，避免云端访问策略和普通上游请求不一致。
- Gemini `/v1beta/models` 预检在 Vertex service account 账号下应使用兼容 fallback，不能因为 native endpoint 差异误判账号不可用。
- 新增 UI 文案和表单字段必须同时覆盖创建、编辑、批量编辑和中英文 locale。

重点文件：

- `backend/internal/service/vertex_service_account.go`
- `backend/internal/service/vertex_service_account_test.go`
- `backend/internal/service/gateway_anthropic_vertex_service_account_test.go`
- `backend/internal/service/gemini_messages_compat_service.go`
- `frontend/src/components/account/CreateAccountModal.vue`
- `frontend/src/components/account/EditAccountModal.vue`
- `frontend/src/components/account/BulkEditAccountModal.vue`
- `frontend/src/components/common/PlatformTypeBadge.vue`
- `frontend/src/types/index.ts`
- `frontend/src/i18n/locales/zh.ts`
- `frontend/src/i18n/locales/en.ts`

### 8.4 热路径与安全回归保护

- usage logging 队列满时不得回退为请求 goroutine 内同步执行重任务；overflow fallback 应走 bounded async 或显式降级，避免高峰尾延迟被放大。
- upstream HTTP client cache 淘汰不能长时间持有全局写锁并同步关闭大量连接。
- API key rate-limit 窗口过期时需要避免同 key 高并发重复启动 reset goroutine。
- 管理后台 usage 导出默认避免每页精确 `COUNT(*)`，精确总数应按需启用。
- Kimi / Moonshot 官方兼容通道需要保留 native-first 与官方默认 base URL，已有账号显式 `base_url` 不得被覆盖。
- compatible gateway 读取 upstream body 时必须处理 body limit 读错，不能返回空或截断 body。
- 调试日志不得在生产误开时输出完整 prompt body；支付和备份下载新窗口必须隔离 `window.opener`。
- 备份 retention 删除 S3 对象失败时不得丢失元数据，避免后续无法清理孤儿对象。
- 备份定时启用状态是本机文件开关，默认关闭；多节点部署只能在选定节点启用，否则会重复备份。

重点文件：

- `backend/internal/service/usage_record_worker_pool.go`
- `backend/internal/repository/http_upstream.go`
- `backend/internal/service/billing_cache_service.go`
- `backend/internal/service/compatible_gateway_service.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/service/backup_service.go`
- `frontend/src/views/admin/UsageView.vue`
- `frontend/src/views/admin/__tests__/UsageView.spec.ts`
- `frontend/src/views/user/PaymentView.vue`
- `frontend/src/components/payment/providerConfig.ts`
- `frontend/src/components/payment/StripePaymentInline.vue`
- `frontend/src/components/admin/account/AccountBulkActionsBar.vue`
- `frontend/src/views/admin/BackupView.vue`

### 8.5 验证重点

同步 `v0.1.120` 后至少确认：

- Fast/Flex policy 默认不改变现有请求，低价 `flex` 会被标准化，账单使用最终有效 tier。
- Request body 读失败只增强内部日志和 ops，不改变客户端 JSON 结构和错误文本。
- Vertex service account 账号创建、编辑、批量编辑、代理 token exchange、Gemini 模型预检可用。
- Compatible / Kimi 官方通道保持 native-first、relay fallback、chat fallback 与官方默认 base URL 行为。
- usage logging、HTTP client cache、rate-limit reset、usage 导出 COUNT 优化不改变正常计费和查询结果。
- compatible body limit、debug 日志脱敏、opener 隔离、backup retention 删除失败保护均有专项回归。
- 升级后在唯一备份节点重新开启本机定时备份开关，多节点不要同时开启。

建议测试命令：

```powershell
cd backend
go test ./internal/handler ./internal/repository ./internal/pkg/httputil ./internal/service ./internal/service/openai_ws_v2 ./cmd/server -count=1

cd ../frontend
npm run typecheck
npm run test:run -- accountsLocale providerConfig
```

## 9. v0.1.121 同步新增保护点

### 9.1 Anthropic Cache TTL 1h Injection

- 新增 `/admin/settings` 网关转发开关 `enable_anthropic_cache_ttl_1h_injection`，默认 `false`，不改变升级后的现有请求行为。
- 开启后仅作用于 Anthropic OAuth / SetupToken 账号，并且只改写请求体中已经存在的 `ephemeral` cache_control，不新增缓存断点。
- 账号级 cache TTL 计费覆盖优先于全局开关；没有账号级覆盖时，全局 1h 注入产生的 response usage 默认回写到 5m 计费，避免账单被错误放大到 1h。
- 网关转发设置继续使用进程内 60s 缓存与更新失效机制，不在每个请求上直接读设置表。

重点文件：

- `backend/internal/service/gateway_service.go`
- `backend/internal/service/setting_service.go`
- `backend/internal/service/gateway_body_order_test.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/dto/settings.go`
- `frontend/src/views/admin/SettingsView.vue`
- `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

### 9.2 Sticky Session Scheduling

- scheduler snapshot 的 slim metadata 必须保留账号分组成员关系，不能因为裁剪嵌套 group 导致 sticky session 误判账号不在分组。
- sticky 命中后仍需保留平台、模型、quota、窗口成本、RPM、schedulable 状态检查。
- 当 sticky 原账号暂时不可用并走 fallback 账号成功时，不应覆盖原 sticky 绑定，避免短暂故障把会话永久漂移到 fallback 账号。

重点文件：

- `backend/internal/repository/scheduler_cache.go`
- `backend/internal/repository/scheduler_cache_unit_test.go`
- `backend/internal/repository/scheduler_cache_integration_test.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/handler/gateway_handler.go`

### 9.3 OpenAI Responses WS Previous Response Inference

- store=false WebSocket 多轮工具续链中，`function_call_output` 缺少 `previous_response_id` 时可以回填上一轮响应 ID。
- 如果当前 payload 已包含完整 tool call / function call 上下文，不能强行推断 previous response，避免覆盖客户端自包含 replay 语义。
- 只有 `item_reference` 的场景仍需要推断上一轮响应锚点，否则上游无法解析被引用的 function_call。

重点文件：

- `backend/internal/service/openai_ws_forwarder.go`
- `backend/internal/service/openai_ws_forwarder_ingress_test.go`
- `backend/internal/service/openai_ws_forwarder_ingress_session_test.go`

### 9.4 表格分页大小策略

- 本 fork 保留“系统表格默认值优先”的策略，不吸收 upstream 恢复 `localStorage` 分页大小持久化的改动。
- 旧浏览器本地 `table-page-size` 不得覆盖管理员配置的 `table_default_page_size`，避免每台客户端残留状态破坏统一表格展示。
- 页面仍通过 `normalizeTablePageSize` 归一分页大小，非法值不能突破系统允许范围。

重点文件：

- `frontend/src/components/common/Pagination.vue`
- `frontend/src/composables/usePersistedPageSize.ts`
- `frontend/src/composables/useTableLoader.ts`

### 9.5 同步范围裁剪说明

- 本次没有直接 merge `v0.1.121` tag；当前 fork 与 upstream tag 历史非线性，直接 merge 会重新引入 Affiliate 等本地明确排除能力。
- 未吸收 upstream `.gitignore` 中忽略 `AGENTS.md` 的改动，继续保留本 fork 对本地协作文档的跟踪能力。
- upstream tag `v0.1.121` 中 `backend/cmd/server/VERSION` 仍为 `0.1.120`，本 fork 同步到相同文件内容，不额外强行改为 `0.1.121`。

### 9.6 验证重点

- Anthropic TTL 注入默认关闭；开启后只作用于 Anthropic OAuth / SetupToken，且只修改已有 ephemeral cache_control。
- sticky session fallback 成功后不能污染原绑定；scheduler snapshot slim/full metadata 都应保留分组成员关系。
- OpenAI Responses WS 的 `item_reference` 续链、完整 tool context replay、缺失 `call_id` 三类场景都应维持各自语义。
- 表格分页大小必须以管理员配置默认值为准，旧 localStorage 值不能覆盖系统默认配置。
- Affiliate 仍不得重新进入路由、菜单、migration、service、repository。

### 9.7 New-API 风格接口增量开关与新平台 adapter

- 账号行为开关统一存放在 `extra.newapi_style_interface_enabled`，不进入 `credentials`，避免被当作密钥或敏感配置处理。
- `/admin/accounts` 创建和编辑账号均展示“启动 New-API 风格接口”开关；Anthropic、OpenAI、Gemini、Antigravity 以及 GLM、DeepSeek、Ali、Moonshot、VolcEngine 默认关闭，继续走已验证的 sub2api 原链路。
- 新增 New-API-only 平台 `perplexity`、`mistral`、`siliconflow`、`xai`、`openrouter`、`suno`、`kling`、`midjourney` 默认并强制启用该开关，因为它们没有旧链路。
- 后端已统一落地 `UseNewAPIStyleInterface(account, group)` 作为行为开关入口；OpenAI、Anthropic/Gemini/Antigravity 原生 handler、compatible gateway 和 New-API-only endpoint 均在选中账号后按有效开关分流。
- Anthropic、OpenAI、Gemini、Antigravity 默认仍保持原生链路；只有账号级或分组级有效开关开启后，`messages`、`responses`、`chat/completions`、`images` 等已接入端点才进入 New-API 风格转发。
- compatible usage fallback 已扩展到所有 compatible 平台；成功请求若上游缺失 usage，会按请求/响应做本地 token 估算，避免新接入平台再次出现 0 token / 0 cost 静默计费问题。
- 若 compatible 平台已有可计费用量但渠道定价未命中导致成本为 0，后端会写 Warn 日志提示补齐定价，允许记录 usage log 但不伪装成正常收费。

重点文件：
- `backend/internal/service/compatible_platforms.go`
- `backend/internal/service/compatible_platform_newapi_generic.go`
- `backend/internal/service/newapi_style_gateway_service.go`
- `backend/internal/handler/newapi_style_gateway_handler.go`
- `backend/internal/service/gateway_service.go`
- `backend/internal/server/routes/gateway.go`
- `frontend/src/components/account/CreateAccountModal.vue`
- `frontend/src/components/account/EditAccountModal.vue`

### 9.8 New-API 风格路由二阶段落地保护

- `UseNewAPIStyleInterface(account, group)` 是唯一分流判断入口；账号级 `extra.newapi_style_interface_enabled`、分组级 `newapi_style_interface_enabled` 和 New-API-only 平台强制规则都必须通过该 helper 生效。
- 对已有平台未开启有效开关时，Anthropic / OpenAI / Gemini / Antigravity / GLM / DeepSeek / Ali / Moonshot / VolcEngine 继续走现有 native/custom/compatible 链路。
- 已挂载 New-API-only 入口：`/v1/audio/*`、`/v1/embeddings`、`/v1/rerank`、`/v1/videos*`、`/v1/video/generations*`、`/suno/*`、`/kling/*`、`/mj/*`；这些入口只允许已启用 New-API 风格且 adapter 支持的账号承接。
- New-API 风格转发必须继续复用 sub2api 的 API key、group、account 调度、concurrency、failover、ops、usage worker 和 billing 链路，不允许新增旁路直连。
- 计费防 0 已新增 `request_count`、`task_count`、`usage_estimated`、`billable_unit_type` 字段和 migration；New-API 成功响应必须至少落 token、image、request 或 task 之一，缺失 upstream usage 时要估算或按 request/task 计量。
- 当前任务型接口先落地同步转发和 task 计费 guardrail；`suno`、`kling`、`midjourney` 可创建为 New-API-only 账号并分别承接 `/suno/*`、`/kling/*`、`/mj/*`。如果后续实现 New-API 异步任务表、轮询 worker、预扣费、失败退款/冲正，必须单独新增 migration、repository、service、worker 测试，并继续记录在本文件。
- 验证基线：`go test ./internal/handler ./internal/service ./internal/repository ./internal/server ./internal/pkg/httputil -count=1` 必须通过；涉及前端账号开关时还需跑 `npm run build` 或等价 typecheck/build。

### 9.9 Group 级 New-API 风格开关

- `/admin/groups` 增加分组级 `newapi_style_interface_enabled`，默认 `false`。
- 分组开关为 `true` 时，该分组通过通用网关选中的账号优先使用 New-API style adapter，避免逐账号批量开启。
- 分组开关为 `false` 时，不覆盖账号级 `extra.newapi_style_interface_enabled`，以保证既有账号级启用行为不被破坏。
- New-API-only 平台仍由平台规则强制启用；Antigravity 专属 `/antigravity/v1/*`、`/antigravity/v1beta/*` 路由保持原义，不由该开关重定向。
- 保护文件：`backend/ent/schema/group.go`、`backend/migrations/132_add_group_newapi_style_interface_enabled.sql`、`backend/internal/service/compatible_platforms.go`、`backend/internal/service/newapi_style_gateway_service.go`、`frontend/src/views/admin/GroupsView.vue`。

### 9.10 New-API reference channel catalog guardrail
- The New-API reference project has exactly 37 `relay/channel` directories. This fork records all 37 in `backend/internal/service/newapi_channel_catalog.go` with one of `enabled_preset`, `existing_custom`, `dedicated_required`, `task_worker_required`, or `candidate_unverified`.
- Runtime enablement is intentionally narrower than the catalog. `enabled_preset` and `existing_custom` entries are the only safe categories to rely on today; signed providers, OAuth/project-scoped providers, media/task providers, and unverified OpenAI-like providers must not be advertised as fully supported until dedicated adapters, usage extraction, billing tests, and task workers exist.
- Create/update account writes normalize `extra.newapi_style_interface_enabled`: New-API-only platforms are persisted as enabled, unsupported platforms drop stale keys, and existing native/custom platforms keep only explicit true. Bulk account updates that touch this key normalize per account instead of blindly merging one JSON value across mixed platforms.
- New-API style upstream request errors and HTTP error responses must append `OpsUpstreamErrorEvent` entries as well as setting the summary ops error, so failover and ops detail views do not lose per-attempt context.
- Group-level New-API style enablement must be included in zero-cost compatible billing warnings; account-level checks alone are insufficient after `/admin/groups` gained the switch.
- Current catalog statuses: enabled preset = `ali`, `deepseek`, `mistral`, `moonshot`, `openrouter`, `perplexity`, `siliconflow`, `volcengine`, `xai`, `zhipu`; existing custom = `aws`, `claude`, `codex`, `gemini`, `openai`, `vertex`; task worker required = `jimeng`, `minimax`, `replicate`, `suno`, `kling`, `midjourney`, `task`; dedicated required = `baidu`, `baidu_v2`, `cloudflare`, `coze`, `dify`, `palm`, `tencent`, `xunfei`, `zhipu_4v`; candidate unverified = `ai360`, `cohere`, `jina`, `lingyiwanwu`, `mokaai`, `ollama`, `submodel`, `xinference`.
Protect files:
- `backend/internal/service/newapi_channel_catalog.go`
- `backend/internal/service/newapi_channel_catalog_test.go`
- `backend/internal/service/compatible_platforms.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/service/newapi_style_gateway_service.go`

### 9.11 Upstream v0.1.123 selective absorption
- 本次同步选择性吸收 upstream `v0.1.122..v0.1.123` 中的 OpenAI 兼容与稳定性修复：compact 批量编辑字段、APIKey 账号 raw Chat Completions fallback、Responses 探测、图片请求稳定性、流式 usage drain、零 usage 日志记录、WS passthrough `reasoning_effort` / User-Agent usage metadata、未知模型不再静默 fallback 到默认模型。
- Chat Completions 冲突处理规则：继续保留本 fork 的 New-API style 分流；关闭 New-API 开关的原生 OpenAI fallback 调用必须传空 fallback model，避免重新引入 upstream 已修复的未知模型默认兜底。
- raw Chat Completions usage billing 必须按最终上游计费模型记录，不能被原始请求模型覆盖；零 usage OpenAI 响应仍应写 usage log，便于审计异常上游行为。
- OpenAI WS passthrough usage metadata 继续使用 policy-mutated payload 提取 `service_tier` 和 `reasoning_effort`，保证 Fast/Flex policy 过滤后计费与实际上游处理一致。
- upstream affiliate / referral 相关 commit、migration、路由、菜单和前端页面继续跳过；本 fork 使用现有 Promotion / rebate 系统，不允许在同步中重新引入 affiliate 账本体系。
- 上游 main 的 `backend/cmd/server/VERSION` 已确认为 `0.1.123`；本 fork 在 `5b71157f` 中将本地 `VERSION` 对齐为 `0.1.123`。后续同步冲突时必须同时核对 upstream tag 与 main 的 `VERSION`，不得把旧的 `0.1.122` 重新写回当前 dev。
- 同步前暂存的 `/admin/groups` 创建分组平台模糊搜索 UI 已恢复，后续 upstream sync 不应覆盖该交互。
Protect files:
- `backend/internal/handler/openai_chat_completions.go`
- `backend/internal/service/openai_gateway_chat_completions.go`
- `backend/internal/service/openai_gateway_chat_completions_raw.go`
- `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
- `frontend/src/components/account/BulkEditAccountModal.vue`
- `frontend/src/views/admin/GroupsView.vue`

### 9.12 v0.1.123 版本对齐与分组平台搜索

- v0.1.123 同步时 `backend/cmd/server/VERSION` 曾对齐 `0.1.123`；后续版本同步以最新 upstream main 的 `VERSION` 为准。
- `/admin/groups` 创建分组模块的平台选择保留搜索输入，使用 `createPlatformSearchQuery` / `filteredCreatePlatformOptions` 对平台名称、平台标识、描述和模型提示做模糊过滤，交互需与 `/admin/accounts` 添加账号的平台搜索保持一致。
- 该搜索是 UI-only 优化，不改变 `platform` 字段保存值、不改变 New-API 风格开关、不改变分组倍率/限额/模型路由、不影响后端调度和计费。
- 后续 upstream sync 若触碰 `frontend/src/views/admin/GroupsView.vue`，必须确认创建分组与编辑分组仍能正常选择平台，且搜索为空时仍展示完整平台列表。
- 当前 dev 源码已通过 `git archive` 形式分别发布到主节点和子节点 quick-deploy 脚本流程；部署脚本不属于源码功能，但后续验证版本时应以容器健康状态、`/health` 和启动版本日志三者共同确认。

### 9.13 Upstream v0.1.124 selective absorption

- `backend/cmd/server/VERSION` 当前应保持 `0.1.124`；本次使用 upstream `main` 的 `f3577bc6` 对齐版本号，因为 tag `v0.1.124` 自身仍停留在 `VERSION=0.1.123`。
- 已吸收 OpenAI / Codex 低风险兼容补丁：WS `function_call_output` 请求不再错误删除 `previous_response_id`，避免工具结果回传被上游视为断链。
- 已吸收 Codex image generation bridge 的默认关闭策略：`gateway.codex_image_generation_bridge_enabled` 默认 `false`，只有全局配置、Channel feature override 或 OpenAI 账号 `extra.codex_image_generation_bridge` 显式开启时，才会为 Codex `/v1/responses` 自动注入 `image_generation` 工具和桥接指令；显式携带 image_generation 的请求仍保留原有归一化链路。
- 已吸收 ops cleanup 设置生效修复：`OpsCleanupService` 会从 `ops_advanced_settings.data_retention` 读取启用状态、cron 和保留天数，管理员更新高级设置后会热重载清理 cron；仍复用当前 fork 的 ops 表、Channel Monitor 维护、Redis/DB leader lock 和 heartbeat，不替换现有运维统计体系。
- 已吸收通用前端选择器的低风险搜索增强：`Select` 和 `GroupSelector` 支持 `searchable="auto"`，选项较多时自动展示搜索输入；这是 UI-only 优化，不改变保存字段、调度、计费或权限。
- 继续跳过 upstream Affiliate / redeem rebate / markdown page、GitHub/Google email OAuth、risk-control content moderation、大范围 OpenAI image generation controls 和 OpenAI messages compatibility 重构等高影响改动；这些改动会触碰本 fork 已有 Promotion、认证、页面、网关策略或安全审计语义，后续如需吸收必须单独评估。
- 保护文件：`backend/internal/service/openai_gateway_service.go`、`backend/internal/service/openai_ws_forwarder.go`、`backend/internal/service/ops_cleanup_service.go`、`backend/internal/service/ops_settings.go`、`backend/internal/service/codex_image_generation_bridge.go`、`frontend/src/components/common/Select.vue`、`frontend/src/components/common/GroupSelector.vue`。

### 9.14 Upstream v0.1.124 high-impact features selected integration

> This section supersedes the earlier v0.1.124 note that temporarily skipped these high-impact upstream features. They were later approved for guarded integration.

- GitHub / Google email OAuth has been integrated as an additive login capability. It is default-off through `github_oauth_enabled=false` and `google_oauth_enabled=false`; login/register buttons are only shown when the corresponding provider is enabled and configured. OAuth secrets remain write-only in admin settings responses.
- Risk-Control content moderation has been integrated as an additive gateway audit capability. It is default-off through `risk_control_enabled=false`, and the gateway hot path calls `IsRiskControlEnabled` before building moderation input or reading/logging request bodies for risk checks.
- Markdown Pages has been integrated as an additive custom-menu renderer. It is default-off through `markdown_pages_enabled=false`; only custom menu URLs in `md:<slug>` form fetch `data/pages/<slug>.md`, and the frontend renders markdown only after DOMPurify sanitization. Existing external URL, iframe, and new-tab custom menu behavior is unchanged.
- Affiliate / redeem rebate remains excluded. The fork's existing Promotion system is still the only referral/rebate system and must not be replaced by upstream Affiliate logic during future syncs.
- These features are protected as default-off extensions: enabling them must be an explicit admin action, and disabled state must not change existing login, gateway, billing, usage, Promotion, New-API routing, payment, or custom menu behavior.

Protect files:
- `backend/internal/handler/auth_email_oauth.go`
- `backend/internal/handler/content_moderation_helper.go`
- `backend/internal/handler/page_handler.go`
- `backend/internal/service/auth_email_oauth_auto.go`
- `backend/internal/service/auth_oauth_email_flow.go`
- `backend/internal/service/content_moderation.go`
- `backend/internal/service/content_moderation_input.go`
- `backend/internal/service/setting_service.go`
- `backend/internal/server/routes/auth.go`
- `backend/internal/server/routes/admin.go`
- `backend/internal/server/router.go`
- `frontend/src/components/auth/EmailOAuthButtons.vue`
- `frontend/src/views/admin/RiskControlView.vue`
- `frontend/src/views/admin/SettingsView.vue`
- `frontend/src/views/auth/OAuthCallbackView.vue`
- `frontend/src/views/user/CustomPageView.vue`
- `frontend/src/api/pages.ts`
- `frontend/src/api/admin/riskControl.ts`

Validation baseline:
- `go test ./internal/service ./internal/handler ./internal/server ./internal/repository ./cmd/server -count=1`
- `pnpm run build`

### 9.15 管理员账号测试类型选择

- `/admin/accounts/:id/test` 支持管理员显式选择测试类型：`auto`、`text`、`image`、`asr`、`tts`、`video`、`task`、`embedding`、`rerank`；前端不按平台/模型名隐藏显式类型，管理员选什么就按对应 probe 发起测试，由上游返回真实能力错误。
- `auto` 或未传 `test_type` 时继续沿用旧自动判断逻辑：OpenAI `gpt-image-*` 和 Gemini 图片模型仍走图片测试，其余平台保持原文本/平台测试行为。
- 显式类型只影响管理员主动触发的账号测试 SSE 链路，不进入用户网关、中转、usage、billing、ops 或 failover 主链路。
- ASR/TTS 已接入 OpenAI 与 GLM/Zhipu API Key 测试路径；ASR 使用内置 `backend/internal/service/testdata/asr_probe_zh.mp3`，预期短中文内容为“你好”；GLM/Zhipu 使用 `/api/paas/v4/audio/transcriptions` 与 `/api/paas/v4/audio/speech`，TTS 音色由管理员在测试弹窗输入。
- 所有显式测试类型都必须由管理员选择模型；前端不得提交空 `model_id`，后端也不得为显式测试配置默认模型。若管理员选择的模型不支持当前测试类型，应按该模型真实请求上游并返回上游能力错误。
- TTS 音色测试成功后按登录 token 本机作用域与平台保存到浏览器 localStorage，下次同账号同平台测试可直接选择；该保存仅影响管理员测试弹窗，不写数据库、不影响真实中转请求。
- Video 使用内置 `backend/internal/service/testdata/video_probe_zh.mp4` 发起 Chat Completions 视频理解请求，测试画面与声音理解，不再走视频生成任务；Embedding 走 OpenAI API Key 测试路径；Rerank 走 SiliconFlow 测试路径；Task 走 Suno/Kling/Midjourney 测试路径。平台不支持时应透传或返回明确 SSE error，不静默 fallback 到 `auto`。
- 不支持的平台、模型或未知 `test_type` 必须通过 SSE error 返回 unsupported，不允许盲目请求上游，也不能静默 fallback 到 `auto`。
- Task 测试最多轮询 60 秒；如果已拿到 task id 但仍在处理中，返回“提交成功但仍在处理中”的成功事件，避免长任务被误判为账号不可用。
- 该能力可能产生上游测试成本，但不记录用户用量、不扣费、不改变管理员以外的任何链路表现。

Protect files:
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/service/account_test_service.go`
- `backend/internal/service/account_test_probe_types.go`
- `backend/internal/service/account_test_newapi_probes.go`
- `backend/internal/service/account_test_probe_types_test.go`
- `backend/internal/service/testdata/asr_probe_zh.mp3`
- `backend/internal/service/testdata/video_probe_zh.mp4`
- `frontend/src/components/admin/account/AccountTestModal.vue`
- `frontend/src/components/admin/account/__tests__/AccountTestModal.spec.ts`
- `frontend/src/components/account/AccountTestModal.vue`
- `frontend/src/components/account/__tests__/AccountTestModal.spec.ts`
- `frontend/src/i18n/locales/en.ts`
- `frontend/src/i18n/locales/zh.ts`

### 9.16 Custom menu sidebar hit area

- Custom menu entries in the sidebar must keep the same full-row clickable area as built-in menu entries.
- External custom menu entries render as `<button class="sidebar-link">` when `open_in_new_tab=true`; `.sidebar-link` must include `w-full` so the hit area does not shrink to the menu label length.
- This is a frontend-only layout rule. It must not change custom menu visibility, URL routing, new-tab behavior, iframe/markdown rendering, gateway forwarding, billing, usage logging, failover, or any upstream relay path.
- Regression coverage lives in `frontend/src/components/layout/__tests__/AppSidebar.spec.ts` and should assert that `.sidebar-link` keeps full-width behavior.

Protect files:
- `frontend/src/style.css`
- `frontend/src/components/layout/AppSidebar.vue`
- `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`

Validation baseline:
- `cd frontend && npm run test:run -- AppSidebar`
- `cd frontend && npm run build`

## 10. localtest 环境说明

- `sub2api-custom-localtest` 是测试环境，可覆盖、重建容器、清理数据。
- `sub2api-custom-src/dev` 是真实可部署主线，不能提交临时打包目录、迁移中间文件、benchmark 临时输出。
- localtest 的 `deploy/.env`、`deploy/data`、`deploy/redis_data`、`deploy/postgres_data` 默认不应被 dev 覆盖。
- PostgreSQL 本地测试环境优先使用 named volume，避免 Windows bind mount 导致权限问题。

## 11. 上游同步后的最小验收清单

### 11.1 后端测试

至少执行：

```powershell
cd backend
go test ./internal/config ./internal/service ./cmd/server -count=1
go test ./internal/handler -run "TestCompatible|TestGateway" -count=1
go test ./internal/server/middleware -run "TestAPIKeyAuth|TestApiKeyAuthWithSubscriptionGoogle" -count=1
```

如同步触及 usage / gateway / promotion / backup，还应补跑对应专项测试。

### 11.2 前端测试

至少执行：

```powershell
cd frontend
pnpm run build
```

如同步触及使用记录页面，还应人工检查：

- `/admin/usage` 表格字段是否完整
- 筛选器是否可用
- 成本 tooltip / 账号成本 / 实际扣费是否正常
- first token / duration 是否显示
- requested model / upstream model / endpoint 分布图是否正常
- CSV 导出字段是否完整

### 11.3 人工链路验收

- Claude Code -> Sub2API -> Kimi
- Claude Code -> Sub2API -> GLM
- Claude Code -> Sub2API -> GPT
- Cherry Studio -> GPT-images
- Codex -> GPT / 非 GPT 上游基本链路
- `/promotion` 与 `/admin/promotion`
- `/admin/settings` 邀请码注册提示、站点 Logo、自定义菜单
- `/admin/usage` 与用户侧 `/usage`
- 定时备份配置与本机启用开关行为

## 11. 后续新增定制功能时的记录要求

以后新增 fork 能力时，必须同步补充本文件：

- 功能点
- 保护原因
- 关键文件
- 验证方式
- 是否允许 upstream 覆盖

如果只改代码不改本文件，下一次 upstream 同步时很容易被误删。


## 12. 远端推送约定

本项目当前维护两个自有远端，后续执行“推送项目 / 发布代码 / 同步远端”时，默认同时推送 `main` 与 `dev` 到两个平台：

- GitHub：`origin` -> `https://github.com/Blue-Seventeen/sub2api.git`
- GitCode：`gitcode` -> `https://gitcode.com/Blue17/sub2api.git`

推荐命令：

```powershell
git push origin main dev
git push gitcode main dev
```

注意：`upstream` 仅用于同步官方项目，不允许把本 fork 的定制提交 push 到 `upstream`。

## 13. 自定义全局货币符号显示

- `/admin/settings` 的 API 端点地址下新增 `display_currency_symbol` 展示设置，默认值为 `$`，管理员可配置为 `¥`、`RMB` 等最多 8 个 Unicode 字符的可见符号。
- `货币符号` 下方新增 `货币符号仅用于本机` 开关，默认开启；开启时写入本机 `display_currency_symbol.local.json`，关闭时写入 PostgreSQL settings 并在共库节点共享。
- 本机文件路径优先级为 `DATA_DIR/display_currency_symbol.local.json`、`/app/data/display_currency_symbol.local.json`、当前目录 `display_currency_symbol.local.json`；文件结构固定为 `{ "local_only": true, "symbol": "¥" }`。
- 升级兼容：如果本机文件不存在但数据库已有 `display_currency_symbol`，首次读取会用数据库值初始化当前节点本机符号并尽量落盘，避免升级后展示符号突变。
- 本机配置文件写入在支持 POSIX 权限的文件系统上会尽量 `chmod 0600`；若 Docker Desktop / Windows bind mount 等文件系统不支持 `chmod`，只记录 warn，不阻断保存，避免 `/admin/settings` 返回 `internal error`。
- 该能力只影响前端可见金额展示，不改变余额、充值、退款、订阅、计费、用量、导出 CSV 和 API 返回的任何数值字段，也不做汇率换算。
- 后端不新增数据库 migration；public settings 和 SSR 注入只暴露当前节点有效 `display_currency_symbol`，不暴露本机/共享开关状态。
- 前端金额展示必须统一使用 `formatCurrencyAmount`、`formatCostAmount`、`formatCompactCurrencyAmount`、`formatCurrency` 或 `formatScaled`，避免重新写死 `$` 或 `¥`。
- upstream sync 若触碰设置页、public settings、本机配置文件、金额展示组件、支付/推广/用量页面或 `frontend/src/utils/format.ts`，必须确认本展示符号不会被覆盖回硬编码美元符号或被错误改成全局数据库强制共享。

Protect files:
- `backend/internal/service/local_config_file.go`
- `backend/internal/service/display_currency_symbol_local.go`
- `backend/internal/service/setting_service.go`
- `backend/internal/service/settings_view.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/setting_handler.go`
- `frontend/src/stores/app.ts`
- `frontend/src/utils/format.ts`
- `frontend/src/utils/pricing.ts`
- `frontend/src/views/admin/SettingsView.vue`

## 14. v0.1.130 升级兼容说明

- 当前 `codex/sync-v0.1.130` 已包含 upstream `v0.1.130` tag；后续从 `dev` / `0.1.125` 升级时，以该分支为 v0.1.130 兼容基线。
- upstream Affiliate 仍不吸收；本 fork 继续以 Promotion 作为唯一推广返佣体系，不允许重新引入 `/affiliate`、admin affiliate 页面、Affiliate repository/service 或 affiliate migrations。
- 已按 upstream 删除 Ops retry/replay storage 与管理入口；升级后管理员不能再从 Ops 错误日志直接重放原请求，只保留脱敏后的 ops error log 与统计分析。
- `/admin/proxies` 继续以本 fork 自定义体系为准：proxy stats、实时出口账号数、自动检测增量刷新、自动代理粘性、Clash/mihomo 托管订阅和订阅节点拆分都必须保留。
- 分布式代理订阅运行态仍是节点本地状态：订阅和 proxy 绑定存在中央数据库；mihomo 进程、本地端口、runtime 状态、latency/quality、auto-probe 最优选择和 sticky proxy 由每个节点本地维护；集群级实时使用统计依赖共享 Redis。
- 图片计费语义为按实际生成图片数计费，OpenAI / Gateway 路径中 `RequestCount` 使用 `ImageCount` 属于正确行为，不得改回“每个请求一次”。
- v0.1.130 兼容修复要求保持中转热路径轻量：OpenAI ChatCompletions SSE 必须先写 `text/event-stream` header；usage 成功统计不能只依赖 `actual_cost > 0`；sticky 调试日志不得使用默认 `Info`；OpenAI 抢槽失败后不得同步 fresh-load Redis 兜底。
- 发布前至少验证：`go test ./internal/config ./internal/service ./internal/repository ./internal/handler ./internal/handler/admin ./internal/server ./cmd/server -count=1`、`go test ./internal/pkg/apicompat ./internal/pkg/openai_compat ./internal/service -run "Test.*(OpenAI|Compatible|Gateway|Image|Proxy|Sticky|Usage|Billing)" -count=1`、`npm run typecheck`、`npm run build`、`git diff --check`。

## 15. v0.1.131 升级兼容说明

- 当前 `codex/sync-v0.1.131` 以 `codex/sync-v0.1.130` 为基线合入 upstream `v0.1.131`，不应回退 v0.1.130 已保留的 Promotion、CompatibleGateway、usage fallback、统一倍率、AccountAutoOps、备份、本机货币符号和 `/admin/proxies` 自定义体系。
- 订阅管理继续以本 fork 为准；v0.1.131 合并不得回退兑换时刻滚动窗口、自定义小时限额、`starts_at` 排序、开始时间列、秒级时间展示、列设置持久化或相关迁移。
- 已吸收 upstream 用户按平台余额限额能力：新增 `user_platform_quotas` ent schema、repository/service/handler、管理端用户平台限额弹窗、用户侧平台配额展示和默认平台限额设置；迁移编号在本 fork 中使用 `144_user_platform_quotas.sql`，避免与本地 142/143 兼容迁移冲突。
- 用户按平台余额限额只约束余额标准模式下的上游平台消费窗口（日/周/月），不得替代现有订阅额度、分组倍率、渠道定价、统一倍率或真实余额/显示余额语义；平台限额失败也不得改变 usage log 已有字段含义。
- 已吸收 upstream 风控阈值扩展：`/admin/risk-control` 可配置 Moderations 分类阈值；风控仍受现有 `risk_control_enabled` 开关控制，默认关闭时不得读取/记录请求体或影响中转热路径。
- 已吸收 upstream OpenAI Responses 流式失败事件与 HTTP/2 超时/代理 fallback 修复；这些只用于增强错误透传、网络兼容和上游代理可用性，不得改变 Codex/Claude Code/Kimi/GLM/NewAPI 的模型映射、usage 估算、计费或 failover 语义。
- upstream Affiliate 仍不吸收；本 fork 只保留必要 OAuth 兼容 shim，不允许引入 upstream Affiliate 页面、路由、转账 API、repository/service 或 migration。推广返佣继续以自研 Promotion 为唯一权威体系。
- `/admin/proxies` 仍以本 fork 为准；v0.1.131 合并不得改写代理 CRUD、托管订阅拆分、mihomo runtime、stats、active usage、sticky、auto-probe 或 gateway 代理解析链路。
- 已同步 upstream 部署配置与赞助商资源更新时，必须保留本 fork 的 managed proxy/mihomo、图片并发、Docker tag 和本地部署配置；资源类图片可随 upstream 更新，但不能引入新的业务入口覆盖现有菜单。
- 发布前至少验证：`go test ./internal/config ./internal/service ./internal/repository ./internal/handler ./internal/handler/admin ./internal/server ./cmd/server -count=1`、`go test ./internal/pkg/apicompat ./internal/pkg/openai_compat ./internal/service -run "Test.*(OpenAI|Compatible|Gateway|Image|Proxy|Sticky|Usage|Billing)" -count=1`、`go test ./internal/service -run "TestProxyAutoProbe|TestProxyActiveUsage|TestProxyStats|TestProxySticky|TestManagedProxy" -count=1`、`npm run typecheck`、`npm run build`、`git diff --check`。

## 16. v0.1.132 升级兼容说明

- 当前 `codex/sync-v0.1.132` 以 `codex/sync-v0.1.131` 为基线合入 upstream `v0.1.132`，`v0.1.132` tag 已是当前分支祖先；后续不得回退 v0.1.131 已保留的 Promotion、CompatibleGateway、usage fallback、统一倍率、AccountAutoOps、订阅管理和 `/admin/proxies` 自定义体系。
- 已吸收并增强 upstream 分组自定义 `/v1/models` 模型列表：新增 `models_list_config` 字段、管理端配置入口和 API key auth snapshot 版本刷新；启用后同时作为该分组的请求模型白名单，支持大小写不敏感匹配与后缀 `*` 通配。限制只校验用户请求模型名，不校验映射后的上游模型名，不改变实际账号调度、模型映射、计费倍率或 compatible fallback 语义。已启用并保存过的自定义模型列表重新打开时必须只展示已保存白名单条目，管理员删除的模型不得由默认候选模型自动补回；默认候选只用于新建分组、未启用配置或管理员手动新增/编辑时参考。
- 已吸收 upstream 账号池同账号重试状态码配置；未配置时必须继续回退默认 `[401,403,429]`，不得让该配置覆盖现有 failover、sticky、auto-probe 或代理粘性选择规则。
- 已吸收 upstream OpenAI WS rate-limit failover、API Key Responses SSE fallback、Chat Responses usage billing 保留、模型 404 按账号+模型冷却、Antigravity `message_start.input_tokens`、Bedrock `context_management` 清理和 Ops local business-limit 分类；这些属于错误处理、计费准确性和观测分类优化，不得引入成功热路径同步 DB/Redis 等待。
- 已吸收 upstream 长上下文 `cache_read` / `cache_creation` 倍率计费修复；这是计费准确性修复，不得改回未乘倍率，也不得覆盖本 fork 的 `QuotaPlatform`、`ClientProfile`、`CompatibilityRoute`、`FallbackChain`、`ChannelUsageFields` usage 写入字段。
- upstream Affiliate 仍不吸收；本 fork 继续以自研 Promotion 作为唯一推广返佣体系，只允许保留必要兼容 shim，不允许恢复 upstream Affiliate 页面、路由、转账 API、repository/service 或 migration。
- `/admin/proxies` 仍以本 fork 为准；v0.1.132 合并不得改写代理 CRUD、Clash/mihomo 托管订阅、订阅节点拆分、proxy stats、active usage、sticky、auto-probe、自动检测增量刷新或 gateway 代理解析链路。
- 订阅管理仍以本 fork 为准；v0.1.132 合并不得回退兑换时刻滚动窗口、自定义小时限额、`starts_at` 排序、开始时间列、秒级时间展示、列设置持久化或相关迁移。
- `backend/cmd/server/VERSION` 在本 fork 中主动对齐为 `0.1.132`；后续同步时如 upstream tag 内 VERSION 落后于 tag 号，应由维护者确认是否继续按本 fork 发布版本号手动对齐。
- 发布前至少验证：`go test ./internal/service -run "Test.*(OpenAI|WS|Pool|ModelNotFound|Billing|Proxy|Sticky|Usage|RateLimit|Compatible|Subscription|Group)" -count=1`、`go test ./internal/handler ./internal/handler/admin ./internal/server -run "Test.*(OpenAI|Gateway|Proxy|Subscription|Account|Group|Ops|Models)" -count=1`、`go test ./internal/repository ./internal/pkg/apicompat ./cmd/server -count=1`、`npm run typecheck`、`npm run build`、`git diff --check`。

## 17. v0.1.133 升级兼容说明

- 当前 `codex/sync-v0.1.133` 以 `codex/sync-v0.1.132` 为基线合入 upstream `v0.1.133`，`backend/cmd/server/VERSION` 对齐为 `0.1.133`；后续不得回退 v0.1.132 已保留的 Promotion、CompatibleGateway、usage fallback、统一倍率、AccountAutoOps、订阅管理和 `/admin/proxies` 自定义体系。
- 当前 `codex/sync-v0.1.134` 已合入 upstream `v0.1.134`，`backend/cmd/server/VERSION` 对齐为 `0.1.134`；后续不得回退 v0.1.134 已吸收的失败请求追踪、图像 token 计费、用户平台配额 flusher、Responses/Chat bridge、OpenAI/Codex 兼容增强、leader lock 与请求体性能优化，同时必须保留本 fork 的 CompatibleGateway、NewAPI-style、mandatory usage/billing、Promotion、Kimi/Qwen/GLM/DeepSeek 自定义链路。
- 本 fork 新增 Qwen/DashScope ASR 官方路径别名：`POST /compatible-mode/v1/chat/completions` 仅允许 Ali/Qwen 分组使用，进入后复用现有 CompatibleGateway/NewAPI-style 主链路、模型白名单、账号调度、mandatory usage/billing 和订阅扣费，不新增旁路计费逻辑。该路径用于让用户以 DashScope 官方 OpenAI-compatible 路径调用 `qwen3-asr*`，同时保留原 `/v1/chat/completions` 调用方式；`input_audio.data` 必须使用 URL 或 `data:audio/mpeg;base64,<base64>` Data URL，裸 Base64 在上游前返回明确 `400 invalid_request_error`，Ali NewAPI-style 分支也必须执行同样校验并补齐 `X-DashScope-SSE: enable`。嵌入前端绕过只允许精确该路径，不得扩大为整个 `/compatible-mode/` 前缀。
- 本 fork 新增 Qwen/DashScope TTS 官方路径别名：`POST /api/v1/services/aigc/multimodal-generation/generation` 仅允许 Ali/Qwen 分组使用，用户请求体保持 DashScope 官方格式，例如 `{"model":"qwen3-tts-flash","input":{"text":"你好","voice":"Cherry","language_type":"Chinese"}}`；进入后必须复用现有 NewAPI-style 鉴权、分组模型白名单、账号调度、并发控制、active usage、mandatory usage/billing 和订阅扣费，不得新增旁路计费逻辑。该路径默认补齐 `X-DashScope-SSE: enable`，如客户端显式传入则按客户端值透传；响应保持官方 SSE/JSON 格式透传，不做 OpenAI `/v1/audio/speech` 格式转换。后续 upstream sync 不得把该路径合并回通用 `/audio` 路由或扩大为所有 compatible 平台的官方路径代理。
- 本 fork 新增 Qwen/DashScope 图片生成支持：`/admin/accounts` 显式 `image` 测试支持 Ali/Qwen `qwen-image` probe；用户侧 `POST /v1/images/generations` 和根路径 `POST /images/generations` 对 Ali/Qwen 分组开放 OpenAI 风格入参，内部转换到 DashScope `POST /api/v1/services/aigc/multimodal-generation/generation`，响应规范化为 OpenAI images `data[].url` 形状，并复用现有 NewAPI-style 鉴权、分组模型白名单、账号调度、并发控制、active usage、mandatory usage/billing、订阅扣费和图片计费语义。`POST /api/v1/services/aigc/multimodal-generation/generation` 同时作为 Qwen 官方多模态路径别名：`qwen-image*` 按图片计费并透传 DashScope 请求/响应，`qwen3-tts*` 保持原 TTS 请求计费；不得扩大到非 Ali/Qwen 平台，也不得开放未验证的 `/v1/images/edits` 非 OpenAI 路径。
- 已吸收 upstream OpenAI/Codex 兼容增强：OpenAI embeddings gateway、endpoint capability 路由限制、Claude Code Codex 插件全局放行开关、WS terminal event first-token 修复、Responses/Chat usage 字段透传、concurrency acquire 错误分类和 request context 透传；合并时必须保留本 fork 的 mandatory usage/billing 防漏扣兜底。
- 已吸收 upstream 账号 quota 自动暂停能力：支持按 5h/7d 用量阈值自动暂停账号调度，并在配置更新后刷新调度热路径缓存；该能力不得替代现有账号状态、AccountAutoOps、sticky proxy、auto-probe 或 proxy stats 语义。
- 已吸收 upstream 内容审计运行态增强：blocked keywords、pre-block/runtime status、hash block 记录与队列观测需与本 fork 风控开关并存；默认关闭时不得读取/记录请求体或影响中转热路径。
- 已吸收 upstream Antigravity/Anthropic/Gemini 兼容修复、Claude Opus 4.8 模型映射和模型价格元数据更新；这些仅用于协议适配、usage 准确性和价格表更新，不得覆盖本 fork 的统一倍率、GroupRates、channel pricing 优先级或图片按生成数计费语义。
- `/admin/proxies` 仍以本 fork 为准；v0.1.133 合并不得改写 Clash/mihomo 托管订阅、订阅节点拆分、proxy stats、active usage、sticky、auto-probe、增量刷新或 gateway 代理解析链路。
- `/admin/proxies` 分布式部署语义：代理配置、托管订阅、订阅节点列表、启停状态和账号绑定关系继续共享 DB；latency/quality、auto-probe 最优选择、sticky proxy、managed mihomo runtime 实例目录和 runtime 健康状态必须按当前节点本地化。节点身份解析顺序为 `NODE_ID` -> 本机公网 IP -> hostname -> 随机 fallback；公网多节点部署时允许不显式设置 `NODE_ID`，系统会优先用本机公网 IP 生成类似 `ip-203.0.113.10` 的节点身份。多节点共用同一 DB/Redis 时必须保证每个正在运行的节点身份唯一且稳定；如果多节点共用同一个公网出口/NAT、无法探测公网 IP、或需要自定义名称，则必须在 `deploy/.env` 显式设置不同 `NODE_ID`（例如 `sub2api-node-01`、`sub2api-node-02`），`docker-compose.yml` 已透传该变量。Redis 新 key 使用 `proxy:latency:{node_id}:{proxy_id}` 与 `proxy_sticky_account:{node_id}:{account_id}`，旧全局 key 只允许兼容 fallback，新写入不得再污染全局 key。DB `last_error` 仅保留订阅刷新/配置类共享错误，本地 mihomo 启动/进程错误只进入当前节点 runtime status。
- 订阅管理仍以本 fork 为准；v0.1.133 合并不得回退兑换时刻滚动窗口、自定义小时限额、`starts_at` 排序、开始时间列、秒级时间展示、列设置持久化或相关迁移。
- upstream Affiliate 仍不吸收；本 fork 继续以自研 Promotion 作为唯一推广返佣体系。
- 发布前至少验证：`go test ./internal/service -run "Test.*(OpenAI|WS|Pool|ModelNotFound|Billing|Proxy|Sticky|Usage|RateLimit|Compatible|Subscription|Group|ContentModeration)" -count=1`、`go test ./internal/handler ./internal/handler/admin ./internal/server -run "Test.*(OpenAI|Gateway|Proxy|Subscription|Account|Group|Ops|Models|Concurrency)" -count=1`、`go test ./internal/repository ./internal/pkg/apicompat ./cmd/server -count=1`、`npm run typecheck`、`npm run build`、`git diff --check`。

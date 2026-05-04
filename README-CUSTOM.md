# README-CUSTOM

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
| 当前 upstream 基线 | 已同步到 `v0.1.121` |
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
| 设置增强 | 站点 Logo、自定义菜单、外链新页面打开、邀请码注册 HTML 提示 | 属于运营配置能力 | `setting_service.go`, `SettingsView.vue`, `AppSidebar.vue` |
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
- Available Channels 聚合视图
- Channel Monitor / Request Template / Rollup / 用户侧状态页

重点文件：

- `backend/internal/service/account_auto_ops*`
- `backend/internal/service/account_refresh_service.go`
- `backend/internal/service/proxy_*`
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

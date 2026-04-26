# README-CUSTOM

> 用途：记录本 fork 的**本地定制功能点**、**高风险文件**、**上游同步保护规则**。  
> 任何 AI / 人类在执行 `sync upstream`、合并 upstream tag、处理冲突、重跑代码生成之前，必须先阅读本文件。  
> 原仓库曾存在拼写错误文件 `README-UCSTOM.md`，现在保留为兼容入口，主文档以本文件为准。

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
| 当前 upstream 基线 | 已同步到 `v0.1.118` |
| 早期 fork 保护基线 | `2b72deb8fd45dc3a526bda2299b16df8d471107c` |
| 部署策略 | `dev` 是真实可部署主线；`sub2api-custom-localtest` 仅用于本地测试 |
| 架构原则 | 保留 Sub2API 的 Account / Group / Channel / 调度 / sticky / failover / billing，渐进吸收协议优先兼容内核 |

## 2. 本 fork 必须保留的定制能力总表

| 分类 | 本地能力 | 保护原因 | 重点文件 / 页面 |
|---|---|---|---|
| 多上游兼容 | GLM / DeepSeek / 豆包 / Qwen / Kimi 等兼容平台扩展 | 这是“任意客户端 × 任意上游”的基础 | `backend/internal/service/compatible_*`, `backend/internal/pkg/apicompat/*` |
| Claude Code × Kimi | Moonshot/Kimi native-first、relay fallback、chat fallback、tool restore、tokenizer usage 修复 | 解决 Claude Code 中 Kimi 工具调用/usage/stream 不稳定 | `compatible_gateway_service.go`, `compatible_platform_moonshot.go`, `compatible_claude_kimi_tool_restore.go`, `moonshot_tokenizer.go` |
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
| 双机部署 | 定时备份 Redis 分布式锁 | 双机共库时避免重复提交 S3 备份 | `backup_service.go`, `backup_service_lock_test.go` |
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

- 多机器共用同一 PostgreSQL / Redis 时，`backup_schedule` 虽然仍存在数据库 settings 中，但实际执行前必须通过 Redis 分布式锁抢占。
- 锁 key：`sub2api:backup:scheduler:lock`
- 当前锁 TTL：`35m`
- 只影响定时备份，不影响手动备份。

重点文件：

- `backend/internal/service/backup_service.go`
- `backend/internal/service/backup_service_lock_test.go`
- `backend/internal/service/wire.go`
- `backend/cmd/server/wire_gen.go`

## 8. localtest 环境说明

- `sub2api-custom-localtest` 是测试环境，可覆盖、重建容器、清理数据。
- `sub2api-custom-src/dev` 是真实可部署主线，不能提交临时打包目录、迁移中间文件、benchmark 临时输出。
- localtest 的 `deploy/.env`、`deploy/data`、`deploy/redis_data`、`deploy/postgres_data` 默认不应被 dev 覆盖。
- PostgreSQL 本地测试环境优先使用 named volume，避免 Windows bind mount 导致权限问题。

## 9. 上游同步后的最小验收清单

### 9.1 后端测试

至少执行：

```powershell
cd backend
go test ./internal/config ./internal/service ./cmd/server -count=1
go test ./internal/handler -run "TestCompatible|TestGateway" -count=1
go test ./internal/server/middleware -run "TestAPIKeyAuth|TestApiKeyAuthWithSubscriptionGoogle" -count=1
```

如同步触及 usage / gateway / promotion / backup，还应补跑对应专项测试。

### 9.2 前端测试

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

### 9.3 人工链路验收

- Claude Code -> Sub2API -> Kimi
- Claude Code -> Sub2API -> GLM
- Claude Code -> Sub2API -> GPT
- Cherry Studio -> GPT-images
- Codex -> GPT / 非 GPT 上游基本链路
- `/promotion` 与 `/admin/promotion`
- `/admin/settings` 邀请码注册提示、站点 Logo、自定义菜单
- `/admin/usage` 与用户侧 `/usage`
- 定时备份配置与 Redis 锁行为

## 10. 后续新增定制功能时的记录要求

以后新增 fork 能力时，必须同步补充本文件：

- 功能点
- 保护原因
- 关键文件
- 验证方式
- 是否允许 upstream 覆盖

如果只改代码不改本文件，下一次 upstream 同步时很容易被误删。

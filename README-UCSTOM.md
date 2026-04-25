# README-UCSTOM

> 说明：文件名按当前仓库约定保留为 `README-UCSTOM.md`。  
> 用途：记录本 fork 的**本地定制功能点**、**高风险文件**、**上游同步检查项**，避免以后同步 upstream 时把这些能力冲掉。

## 1. 审计基线

- fork 对比基线提交：`2b72deb8fd45dc3a526bda2299b16df8d471107c`
  - 提交说明：`v0.115_feature01_fix01: 修复 GLM Token 无法计算导致计费为 0 的 Bug`
- 当前审计对象：
  - `dev`
  - 已同步 upstream `v0.1.117`
  - 已合入本地 Kimi / GLM / Claude Code 兼容增强

## 2. 本仓库必须保留的定制能力

下面这些能力默认视为 **fork 主干能力**，以后同步 upstream 时应优先保护。

### 2.1 兼容网关 / Claude Code / Kimi / GLM 兼容链路

#### 功能点

- 扩展兼容平台支持：`GLM / DeepSeek / 豆包 / Qwen / Kimi`
- Anthropic / Responses / Chat Completions 之间的兼容转换
- GLM 兼容链路 usage fallback，避免 `token=0` 导致计费异常
- Kimi / Moonshot 的 Claude Code 兼容增强：
  - `messages` 原生优先，失败后自动 fallback 到 `chat/completions`
  - fallback 后仍保留 structured `tool_use / tool_result`
  - `thinking / reasoning_effort` 兼容修补
  - Kimi tokenizer 本地估算输入 token，避免使用记录里 `input_tokens=0`
  - 流式首 token 时间、总耗时、usage 收尾统计
  - 瞬时 `502/503/504/520~525` 同端点重试

#### 重点文件

- `backend/internal/service/compatible_gateway_service.go`
- `backend/internal/service/compatible_gateway_service_relay_fallback_test.go`
- `backend/internal/service/compatible_platform_moonshot.go`
- `backend/internal/service/compatible_platform_moonshot_test.go`
- `backend/internal/service/compatible_platform_patches.go`
- `backend/internal/service/compatible_usage_estimate.go`
- `backend/internal/service/moonshot_tokenizer.go`
- `backend/internal/service/tokenizer_assets/kimi_k2.tiktoken.model`
- `backend/internal/handler/compatible_gateway_handler.go`
- `backend/internal/pkg/apicompat/*`

### 2.2 计费 / 倍率 / 模型映射 / 使用记录

#### 功能点

- 渠道级定价优先于默认定价
- `BillingModelSource / RequestedModel / UpstreamModel` 链路保护
- 分组倍率 / 用户分组倍率 / 统一倍率
- Compatible Gateway usage fallback 计费补偿
- OpenAI / Compatible 两条链路统一的成本结算逻辑
- 图片计费、OpenAI 图片 API 同步接入
- 使用记录保留：
  - `duration_ms`
  - `first_token_ms`
  - `reasoning_effort`
  - `model_mapping_chain`
  - `billing_mode`

#### 重点文件

- `backend/internal/service/gateway_service.go`
- `backend/internal/service/gateway_record_usage_test.go`
- `backend/internal/service/openai_gateway_service.go`
- `backend/internal/service/openai_gateway_record_usage_test.go`
- `backend/internal/service/billing_service.go`
- `backend/internal/service/pricing_service.go`
- `backend/internal/service/api_key_service.go`

### 2.3 推广系统 / 站点设置 / 自定义菜单

#### 功能点

- 推广中心 / 推广后台入口保留
- 推广返佣只针对激活后用户
- 用户侧推广团队贡献按佣金统计
- `/admin/promotion` 海报 Logo
- `/admin/settings` 站点 Logo
- 系统设置“在新页面打开”开关
- 自定义菜单开关保留

#### 重点文件 / 页面

- `frontend/src/components/layout/AppSidebar.vue`
- `frontend/src/views/**/Promotion*.vue`
- `backend/internal/service/setting_service.go`
- `backend/internal/handler/admin/setting_handler.go`
- `backend/internal/handler/setting_handler.go`
- 推广相关 `backend/internal/service/*promotion*`
- 推广相关 `backend/internal/handler/*promotion*`

### 2.4 支付 / OAuth / WeChat / OIDC 升级兼容

#### 功能点

- WeChat / OIDC / LinuxDo 等第三方登录兼容
- 支付恢复 / 回跳 / provider snapshot / visible method source
- 旧数据迁移兼容
- auth identity foundation / pending oauth / bind / unbind 流程

#### 重点文件

- `backend/internal/service/setting_service.go`
- `backend/internal/service/auth_service.go`
- `backend/internal/service/payment_fulfillment.go`
- `backend/internal/handler/payment_webhook_handler.go`
- `backend/internal/payment/provider/*`

### 2.5 WebSearch / 通知 / 可用渠道 / 通道监控

#### 功能点

- Anthropic API Key 账号的 web search emulation
- 余额 / 配额提醒
- Available Channels 聚合视图
- Channel Monitor / Request Template / Rollup / 用户侧状态页

> 这部分不是早期 fork 的最初能力，但现在已经进入当前分支主干，也应视为本仓库需要保护的功能。

#### 重点文件

- `backend/internal/service/channel_monitor_*`
- `backend/internal/service/channel_available.go`
- `backend/internal/repository/channel_monitor_*`
- `frontend/src/views/admin/ChannelMonitorView.vue`
- `frontend/src/views/user/AvailableChannelsView.vue`
- `frontend/src/views/user/ChannelStatusView.vue`
- `frontend/src/components/layout/AppSidebar.vue`

## 3. 本次从 `2b72deb8` 到当前版本的变化分类

### 3.1 明确属于“新增功能 / 新模块”的部分

- Channel Monitor 全链路
- Available Channels 全链路
- RPM / 用户级 RPM / 分组级 RPM 扩展
- OpenAI 图片响应桥接增强
- 若干支付 / OAuth / WeChat / OIDC 升级兼容补丁
- `v0.1.116 / v0.1.117` 同步带来的 ent / migration / handler / frontend 增量

### 3.2 明确属于“本地兼容优化 / 修 Bug”的部分

- GLM token 计费为 0 修复
- GLM relay fallback compatibility
- Kimi / Moonshot tokenizer 计数
- Kimi Claude Code 兼容 fallback
- Compatible Gateway 流式 usage / 首 token / duration 修复
- Compatible transient 5xx retry
- 图片显示 Bug / Cherry Studio 兼容

## 4. 上游同步时的高风险检查点

以后同步 upstream 时，下面这些点**不能直接全量取 theirs / ours**：

1. `backend/internal/service/compatible_gateway_service.go`
2. `backend/internal/service/compatible_platform_moonshot*.go`
3. `backend/internal/service/compatible_platform_patches.go`
4. `backend/internal/service/gateway_service.go`
5. `backend/internal/service/openai_gateway_service.go`
6. `backend/internal/service/api_key_service.go`
7. `backend/internal/service/setting_service.go`
8. `backend/internal/pkg/apicompat/*`
9. `frontend/src/components/layout/AppSidebar.vue`
10. 推广相关前后端文件

## 5. 上游同步后的最小验收清单

### 5.1 后端测试

至少执行：

```powershell
go test ./internal/service -run "TestCompatibleGatewayService|TestGatewayServiceRecordUsage|TestEstimateMoonshotCompatibleInputTokens" -count=1
go test ./internal/handler -run "TestCompatible|TestGateway" -count=1
go test ./internal/server/middleware -run "TestAPIKeyAuth|TestApiKeyAuthWithSubscriptionGoogle" -count=1
go test ./...
```

### 5.2 功能验收

至少人工检查：

- Claude Code -> Sub2api -> Kimi
- Claude Code -> Sub2api -> GLM
- Kimi / GLM 使用记录是否仍写入：
  - `input_tokens`
  - `output_tokens`
  - `first_token_ms`
  - `duration_ms`
- 渠道级定价是否仍生效
- `/promotion` 与 `/admin/promotion` 是否正常
- `/admin/settings` 站点 Logo 是否正常
- 侧边栏是否仍保留：
  - 推广入口
  - 推广后台入口
  - 可用渠道
  - 通道监控

## 6. localtest 环境特别说明（不属于源码主仓功能，但容易被同步覆盖）

- `sub2api-custom-localtest/deploy/docker-compose.dev.yml`
  - PostgreSQL 本地测试环境应优先使用 **named volume**
  - 不应直接改成 Windows bind mount `./postgres_data:/var/lib/postgresql/data`
  - 否则 PostgreSQL 容器容易出现：
    - `chmod ... Operation not permitted`

---

如果后续再新增 fork 能力，记得同步更新本文件，并把：

- 功能点
- 关键文件
- 验证方式

一起补进来，避免以后同步上游时丢失。

## 7. ????????????????????

### 7.1 ???? / Affiliate ???????? fork ??

> ????????????? / Affiliate????????????????????????**????**?
> ???? upstream ???????? **????** ??????????????

#### ??

- ???????????????????`/promotion`?`/admin/promotion`
- ??????????????????????????????????
- ?????
  - ???????
  - ????????
  - ??/??????
  - ????????

#### ???? upstream ?????

??????????????**?????**?

- ????
  - `affiliate`
  - `invite rebate`
  - `affiliate_rebate_rate`
  - `user_affiliates`
  - `user_affiliate_ledger`
- ???? / ???
  - `backend/internal/service/affiliate_service.go`
  - `backend/internal/repository/affiliate_repo.go`
  - `backend/internal/server/routes/user.go` ? `/aff`?`/aff/transfer`
  - `backend/internal/handler/user_handler.go` ? `GetAffiliate`?`TransferAffiliateQuota`
  - `backend/internal/handler/admin/setting_handler.go` / `dto/settings.go` ? `affiliate_rebate_rate`
  - `backend/internal/service/payment_fulfillment.go` ? affiliate rebate ????
  - `backend/migrations/130_add_user_affiliates.sql`
  - `backend/migrations/131_affiliate_rebate_hardening.sql`
- ???? / ???
  - `frontend/src/views/user/AffiliateView.vue`
  - `frontend/src/router/index.ts` ? `/affiliate`
  - `frontend/src/components/layout/AppSidebar.vue` ? `nav.affiliate`
  - `frontend/src/views/admin/SettingsView.vue` ? `affiliate_rebate_rate` ?? UI
  - `frontend/src/api/user.ts` ? affiliate ?? API

#### ??????

- ?? upstream ??????? Affiliate?**?????**
- ???????????**???? / ??**??????????????
- ?? future merge ?????
  - `promotion` ???????? fork
  - `affiliate` ???????? upstream ??
- ?????????????????????????????????????????????

#### ??????????

- ?????????`/promotion`
- ????????`/admin/promotion`
- ???? / ???????????????????????????????????????


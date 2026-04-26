# Hack3rX Sub2API Custom

> 一个面向 **多客户端 × 多上游模型** 的统一 AI 中转网关。
> 当前项目不是官方原版 Sub2API，而是在 Sub2API 基础上进行深度二次开发，并参考 New-API 的协议兼容思想持续演进而来。

## 项目定位

本项目的目标是把不同 AI 客户端与不同上游模型之间的协议差异收敛到网关层：

```text
Claude Code / Codex / Cherry Studio / OpenAI SDK / Anthropic SDK
                              ↓
                    Hack3rX Sub2API Custom
                              ↓
        GPT / Claude / GLM / Kimi / DeepSeek / Qwen / 豆包 / 其他兼容上游
```

也就是说，客户端只需要使用自己熟悉的协议接入本平台，实际执行模型可以由平台根据账号、分组、模型映射、能力策略、调度和兼容转换选择。

## 基于哪些项目优化而来

本项目主要基于以下开源项目和本地实践经验二次开发：

| 来源 | 作用 |
|---|---|
| [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) | 作为主框架基础，保留 Account / Group / Channel / 调度 / 粘性会话 / failover / billing / 管理后台等核心运行时 |
| [Calcium-Ion/new-api](https://github.com/Calcium-Ion/new-api) | 作为协议优先、能力抽象、平台扩展思路的参考，用于改进统一 AI 网关设计 |
| Claude Code / Codex / Cherry Studio 实际使用链路 | 作为重点客户端兼容目标，围绕真实问题修复 Kimi、GLM、GPT-images、Responses 等链路 |

> 致谢以上项目与生态。当前仓库是自用增强版 fork，不代表原项目官方立场。

## 核心增强能力

### 1. 多客户端兼容

重点面向下列客户端做兼容优化：

- Claude Code
- Codex CLI
- Cherry Studio
- OpenAI SDK / Chat Completions 客户端
- Anthropic Messages 客户端

已重点处理的协议入口：

- `/v1/messages`
- `/v1/responses`
- `/v1/chat/completions`
- `/v1/images/generations`
- `/v1/images/edits`

### 2. 多上游模型与平台

在官方 Sub2API 能力基础上，扩展和优化了更多兼容上游：

- GPT / OpenAI 系列
- Claude / Anthropic 系列
- GLM / 智谱系列
- Kimi / Moonshot 系列
- DeepSeek
- Qwen
- 豆包
- 其他 OpenAI-compatible 或 Anthropic-compatible 上游

### 3. Claude Code × Kimi / GLM 兼容优化

针对实际使用中发现的 Kimi、GLM 在 Claude Code 中的不稳定问题，增加了多层兼容处理：

- Kimi / Moonshot native messages 优先
- relay fallback / chat fallback
- 工具调用降级文本恢复兜底
- Kimi tokenizer 本地估算输入 token
- GLM usage fallback，避免 token 为 0 导致计费异常
- SSE、late usage、首 token、总耗时等记录修复
- 兼容链路可观测字段：`client_profile`、`compatibility_route`、`fallback_chain`、`upstream_transport`

### 4. Codex / Responses 链路增强

- 保护 `previous_response_id`
- 保留 tool call id
- 处理 Codex Responses payload normalization
- 兼容 Codex Spark 相关限制
- 修复 OpenAI Responses 流式输出前 failover 时机
- 记录请求模型、上游模型、模型映射链路

### 5. Cherry Studio 与图片链路

- 修复 Cherry Studio 经中转调用 GPT-images 时的响应 shape 问题
- 对上游 OpenAI / New-API 兼容图片返回进行归一
- 保护 `data[].url` / `data[].b64_json`
- 兼容 image generation / image edits
- 图片计费与使用记录同步接入

### 6. 使用记录与成本审计增强

本项目重点优化了使用记录页面与后端统计字段，便于排障、审计和成本核算：

- 管理后台 `/admin/usage`
- 用户侧 `/usage`
- API Key 维度 `/key-usage`
- 支持展示 / 筛选：
  - requested model
  - upstream model
  - model mapping chain
  - billing model / billing mode / billing tier
  - first token latency
  - request duration
  - reasoning effort
  - inbound / upstream endpoint
  - account / group / user / API key
  - 标准计费、实际扣费、账号成本、倍率
- CSV 导出字段增强
- 成本 tooltip 与明细展示优化

### 7. 计费、倍率与模型映射

- 渠道级定价优先于默认定价
- 分组倍率
- 用户分组倍率
- 统一倍率
- 支持 0 倍率免费策略
- 兼容图片、token、按次等多种计费模式
- 保留 requested model / upstream model / billing model 链路

### 8. 推广中心

本项目保留自研 Promotion / 推广中心体系，不使用 upstream 新增的 Affiliate 邀请返利模块：

- 用户侧推广中心 `/promotion`
- 管理后台推广配置 `/admin/promotion`
- 推广链接 / 邀请码
- 团队贡献统计
- 佣金统计
- 推广返佣只针对激活后用户
- 明暗色主题适配

### 9. 账号自动运维与代理池

- 账号自动运维：
  - 刷新令牌
  - 测试连接
  - 恢复状态
  - 删除异常账号
  - 按规则筛选运维对象
- 代理池：
  - 自动检测代理可用性
  - 成功队列
  - 账号选择最优代理
  - 放宽 OpenAI 代理成功队列判定规则

### 10. 系统设置与运营能力

- 站点 Logo
- 自定义菜单
- 外链新页面打开
- 邀请码注册 HTML 提示
- S3 兼容存储备份配置
- 定时备份配置
- 双机部署定时备份 Redis 分布式锁，避免多实例重复备份
- 可用渠道视图
- 通道监控与用户侧状态页

## 当前架构原则

本项目不会把 Sub2API 直接改造成 New-API 的 channel-center 模型，而是采用：

```text
Sub2API 运行时主框架
  + Account / Group / Channel / 调度 / billing
  + capability registry / compatibility context
  + protocol bridge / adapter 思路
  + 针对 Claude Code、Codex、Cherry Studio 的深链路优化
```

核心原则：

1. 保留 Sub2API 的账号、分组、渠道、调度、计费、粘性会话和 failover。
2. 吸收 New-API 的协议优先、能力抽象和 adapter 思路。
3. 优先解决真实客户端链路中的兼容问题。
4. 不支持的能力返回确定性错误，不做静默伪装。
5. 所有 fallback 和兼容路径尽量可观测。

## 关键文档

| 文档 | 说明 |
|---|---|
| [`README-CUSTOM.md`](./README-CUSTOM.md) | 本 fork 的自定义能力、上游同步保护规则、高风险文件清单 |
| [`docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md`](./docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md) | 后续统一兼容内核架构设计 |
| [`docs/PAYMENT_CN.md`](./docs/PAYMENT_CN.md) | 支付配置说明 |
| [`docs/promotion_e2e_checklist.md`](./docs/promotion_e2e_checklist.md) | 推广中心验收清单 |

## 技术栈

| 模块 | 技术 |
|---|---|
| Backend | Go, Gin, Ent |
| Frontend | Vue 3, Vite, TailwindCSS |
| Database | PostgreSQL |
| Cache / Lock | Redis |
| Deploy | Docker / Docker Compose / Nginx |
| Backup | S3-compatible object storage |

## 本地开发与测试

后端测试：

```powershell
cd backend
go test ./internal/config ./internal/service ./cmd/server -count=1
```

前端构建：

```powershell
cd frontend
pnpm install
pnpm run build
```

本地测试环境建议使用 `sub2api-custom-localtest`，不要把 localtest 的 `.env`、数据库目录、Redis 数据目录提交到 dev 主仓库。

## 上游同步注意事项

同步 upstream 之前必须先阅读：

```text
README-CUSTOM.md
```

尤其注意：

- 不要重新引入 upstream Affiliate / 邀请返利模块。
- 不要覆盖自研 Promotion / 推广中心。
- 不要覆盖使用记录页面增强。
- 不要覆盖 Kimi / GLM / Claude Code / Codex / Cherry Studio 兼容链路。
- 不要覆盖计费倍率、模型映射、使用记录成本字段。
- 不要提交临时迁移目录、benchmark 临时文件、localtest 数据。

## 免责声明

本项目是基于开源项目进行的二次开发版本，仅用于自有部署和研究。
使用者应自行确认上游服务条款、账号合规性、数据安全与部署安全。

# Hack3rX Sub2API Custom

> 面向 **多客户端 × 多上游模型** 的统一 AI 中转网关。
> 当前仓库是基于 Sub2API 深度二次开发的自定义版本，并参考 New-API 的协议优先、能力抽象和适配器思路持续优化。

## 项目说明

本项目保留 Sub2API 的账号、分组、渠道、调度、粘性会话、故障转移、计费和后台管理能力，同时围绕真实客户端使用场景做了大量增强：

- Claude Code
- Codex CLI
- Cherry Studio
- OpenAI SDK / Chat Completions 客户端
- Anthropic Messages 客户端

目标是让客户端按自己熟悉的协议接入本平台，再由平台统一选择和桥接上游：

- GPT / OpenAI
- Claude / Anthropic
- GLM / 智谱
- Kimi / Moonshot
- DeepSeek
- Qwen
- 豆包
- 其他 OpenAI-compatible / Anthropic-compatible 上游

## 基于哪些项目优化而来

| 来源 | 说明 |
|---|---|
| [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) | 主框架基础，保留 Account / Group / Channel / 调度 / billing 等核心运行时 |
| [Calcium-Ion/new-api](https://github.com/Calcium-Ion/new-api) | 协议优先、能力抽象、平台适配器设计参考 |
| 实际客户端链路 | 围绕 Claude Code、Codex、Cherry Studio 的真实兼容问题做专项优化 |

## 主要增强能力

- 多客户端协议兼容：`/v1/messages`、`/v1/responses`、`/v1/chat/completions`、`/v1/images/*`
- Claude Code × Kimi / GLM 兼容增强
- Codex Responses / tool call / previous response 链路保护
- Cherry Studio 图片生成与编辑响应归一
- GLM / Kimi usage fallback 与 tokenizer 估算
- 使用记录与成本审计增强
- 计费倍率、统一倍率、模型映射链路保护
- 自研 Promotion / 推广中心
- 账号自动运维与代理池
- 可用渠道、通道监控与用户侧状态页
- S3 备份与多实例定时备份 Redis 锁
- 上游同步保护文档与兼容内核架构文档

## 重要文档

| 文档 | 说明 |
|---|---|
| [`README.md`](./README.md) | 项目总览 |
| [`README-CUSTOM.md`](./README-CUSTOM.md) | 本 fork 自定义能力、同步保护规则、高风险文件清单 |
| [`docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md`](./docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md) | 后续统一兼容内核架构设计 |
| [`docs/PAYMENT_CN.md`](./docs/PAYMENT_CN.md) | 支付配置说明 |
| [`docs/promotion_e2e_checklist.md`](./docs/promotion_e2e_checklist.md) | 推广中心验收清单 |

## 同步 upstream 前必读

同步官方 Sub2API 或其他上游改动前，必须先阅读：

```text
README-CUSTOM.md
```

尤其注意：

- 不要重新引入 upstream Affiliate / 邀请返利模块。
- 不要覆盖自研 Promotion / 推广中心。
- 不要覆盖使用记录页面增强。
- 不要覆盖 Kimi / GLM / Claude Code / Codex / Cherry Studio 兼容链路。
- 不要覆盖计费倍率、模型映射、使用记录成本字段。

## 免责声明

本仓库是基于开源项目二次开发的自定义版本，仅用于自有部署和研究。使用者应自行确认上游服务条款、账号合规性、数据安全与部署安全。

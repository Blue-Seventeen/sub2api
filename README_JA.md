# Hack3rX Sub2API Custom

> **Multiple clients × multiple upstream AI providers** を目標にした AI API gateway です。
> このリポジトリは公式 Sub2API そのものではなく、Sub2API をベースに深く二次開発した custom fork です。New-API の protocol-first / adapter-first の設計思想も参考にしています。

## Overview

This project keeps the Sub2API runtime model:

- Account / Group / Channel
- scheduling and sticky sessions
- failover
- billing and usage logs
- admin dashboard

At the same time, it adds compatibility work for real client flows:

- Claude Code
- Codex CLI
- Cherry Studio
- OpenAI SDK / Chat Completions clients
- Anthropic Messages clients

Supported or optimized upstream families include:

- GPT / OpenAI
- Claude / Anthropic
- GLM / Zhipu
- Kimi / Moonshot
- DeepSeek
- Qwen
- Doubao
- other OpenAI-compatible or Anthropic-compatible providers

## Based on

| Source | Role |
|---|---|
| [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) | Main runtime framework and admin system |
| [Calcium-Ion/new-api](https://github.com/Calcium-Ion/new-api) | Reference for protocol-first capability modeling and adapter design |
| Real Claude Code / Codex / Cherry Studio workflows | Practical compatibility targets |

## Major custom features

- Compatibility for `/v1/messages`, `/v1/responses`, `/v1/chat/completions`, and `/v1/images/*`
- Claude Code × Kimi / GLM compatibility fixes
- Codex Responses, tool call id, and previous response handling
- Cherry Studio image generation/edit response normalization
- GLM / Kimi usage fallback and tokenizer estimation
- Enhanced usage logs, cost audit, model mapping, first-token latency, and request duration
- Custom billing multipliers and unified rate support
- Custom Promotion center instead of upstream Affiliate
- Account auto-ops and proxy pool
- Channel monitor and available-channel views
- S3-compatible backup and Redis lock for multi-node scheduled backups

## Important documents

| Document | Description |
|---|---|
| [`README.md`](./README.md) | Main project overview |
| [`README_CN.md`](./README_CN.md) | Chinese overview |
| [`README-CUSTOM.md`](./README-CUSTOM.md) | Custom fork protection rules and high-risk files |
| [`docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md`](./docs/ARCHITECTURE_COMPATIBILITY_KERNEL_CN.md) | Compatibility-kernel architecture notes |

## Upstream sync warning

Before syncing upstream changes, read:

```text
README-CUSTOM.md
```

Do not overwrite the custom compatibility layer, usage-log enhancements, Promotion center, billing logic, or backup lock behavior without explicit confirmation.

## Disclaimer

This is a custom fork for self-hosted use and research. Users are responsible for complying with upstream service terms, account policies, data security, and deployment security requirements.

# Hack3rX Sub2API Custom 开发指南

> 本文档用于说明当前 custom fork 的开发、测试、同步上游和隐私处理约定。
> 如需了解本 fork 的功能保护边界，请优先阅读 [`README-CUSTOM.md`](./README-CUSTOM.md)。

## 1. 项目定位

当前仓库是基于 `Wei-Shaw/sub2api` 深度二次开发的自定义版本，并参考 `Calcium-Ion/new-api` 的协议优先与适配器设计思想。

开发时必须保护以下本地能力：

- Claude Code / Codex / Cherry Studio 兼容链路
- GPT / Claude / GLM / Kimi 等上游兼容
- 使用记录、成本审计、模型映射、首 token、耗时等字段
- 自研 Promotion / 推广中心
- 计费倍率、统一倍率、渠道定价
- 自动运维、代理池、通道监控
- S3 备份与 Redis 定时备份锁

## 2. 技术栈

| 模块 | 技术 |
|---|---|
| Backend | Go, Gin, Ent |
| Frontend | Vue 3, Vite, TailwindCSS |
| Database | PostgreSQL |
| Cache / Lock | Redis |
| Deploy | Docker / Docker Compose / Nginx |

## 3. 本地开发约定

- 推荐使用 `sub2api-custom-localtest` 作为本地测试部署目录。
- `sub2api-custom-src/dev` 是真实可部署主线，不应提交本地测试数据、构建产物、迁移临时包。
- 不要提交：
  - `.env`
  - `config.yaml`
  - 数据库目录
  - Redis 数据目录
  - Clash / proxy 真实配置
  - benchmark 临时结果
  - 服务器 IP、密码、Access Key、Secret Key

## 4. 常用测试

后端核心测试：

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

如修改兼容链路，应增加或补跑对应专项测试：

- Claude Code -> GPT
- Claude Code -> Kimi
- Claude Code -> GLM
- Codex -> Responses
- Cherry Studio -> Images

如修改使用记录，应检查：

- `/admin/usage`
- `/usage`
- `/key-usage`
- CSV 导出字段
- 成本 tooltip
- requested model / upstream model / model mapping chain
- first token / duration / reasoning effort

## 5. 上游同步流程

同步 upstream 前必须：

1. 阅读 `README-CUSTOM.md`。
2. 列出 upstream 变更点。
3. 列出本地自定义冲突点。
4. 明确哪些吸收、哪些裁剪、哪些保留。
5. 严禁重新引入 upstream Affiliate / 邀请返利模块，除非维护者明确要求。

同步后至少验证：

```powershell
cd backend
go test ./internal/config ./internal/service ./cmd/server -count=1
```

```powershell
cd frontend
pnpm run build
```

## 6. 隐私与密钥处理

提交前应检查：

- 是否包含真实服务器 IP、SSH 密码、root 密码。
- 是否包含 S3 / OSS / R2 Access Key 或 Secret Key。
- 是否包含 `.env`、`config.yaml`、Clash 配置、数据库 dump。
- 是否包含真实域名测试样例；测试中应优先使用 `example.test`、`example.com`。
- 是否包含构建产物或外部网站打包文件。

推荐搜索：

```powershell
git grep -n "password\\|secret\\|access_key\\|api_key\\|token\\|BEGIN .*PRIVATE KEY"
```

命中后需要人工区分“示例/测试占位符”和“真实敏感信息”。

## 7. 提交规范

建议使用简洁的 conventional commit 风格：

- `fix(backup): guard scheduled backups with redis lock`
- `docs(custom): document protected fork customizations`
- `feat(compat): add ...`
- `chore(cleanup): remove generated artifacts`

提交前保持：

```powershell
git status --short
```

只包含本次明确要提交的源码和文档。

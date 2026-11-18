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

## 三、CI/CD 流水线

### GitHub Actions Workflows

| Workflow | 触发条件 | 检查内容 |
|----------|----------|----------|
| **backend-ci.yml** | push, pull_request | 单元测试 + 集成测试 + golangci-lint v2.13 |
| **security-scan.yml** | push, pull_request, 每周一 | govulncheck + gosec + pnpm audit |
| **release.yml** | tag `v*` | 构建发布（PR 不触发） |

### CI 要求

- Go 版本必须是 **1.27.4**：三个 workflow 都用 `go-version-file: backend/go.mod` 取版本，随后硬断言 `go version | grep -q 'go1.27.4'`。升级 Go 时要同时改 `backend/go.mod`、`backend-ci.yml`（两处）、`release.yml`、`security-scan.yml` 里的这句断言，**以及三个 Dockerfile 里的 Go 构建镜像**（`Dockerfile` / `deploy/Dockerfile` 的 `ARG GOLANG_IMAGE`、`backend/Dockerfile` 的 `FROM golang:`）。前者漏了 CI 会在版本校验步骤直接失败；**后者漏了 CI 不会报，而是等到有人用这些 Dockerfile 构建时才失败**（`go.mod requires go >= X (running Y; GOTOOLCHAIN=local)`）。
- 前端使用 `pnpm install --frozen-lockfile`，必须提交 `pnpm-lock.yaml`

### 本地测试命令

```bash
# 后端单元测试
cd backend && go test -tags=unit ./...

# 后端集成测试
cd backend && go test -tags=integration ./...

# 代码质量检查
cd backend && golangci-lint run ./...

# 前端依赖安装（必须用 pnpm）
cd frontend && pnpm install
```

## 四、常见坑点 & 解决方案

### 坑 1：pnpm-lock.yaml 必须同步提交

**问题**：`package.json` 新增依赖后，CI 的 `pnpm install --frozen-lockfile` 失败。

**原因**：上游 CI 使用 pnpm，lock 文件不同步会报错。

**解决**：
```bash
cd frontend
pnpm install  # 更新 pnpm-lock.yaml
git add pnpm-lock.yaml
git commit -m "chore: update pnpm-lock.yaml"
```

---

### 坑 2：npm 和 pnpm 的 node_modules 冲突

**问题**：之前用 npm 装过 `node_modules`，pnpm install 报 `EPERM` 错误。

**解决**：
```bash
cd frontend
rm -rf node_modules  # 或 PowerShell: Remove-Item -Recurse -Force node_modules
pnpm install
```

---

### 坑 3：PowerShell 中 bcrypt hash 的 `$` 被转义

**问题**：bcrypt hash 格式如 `$2a$10$xxx...`，PowerShell 把 `$2a` 当变量解析，导致数据丢失。

**解决**：将 SQL 写入文件，用 `psql -f` 执行：
```bash
# 错误示范（PowerShell 会吃掉 $）
psql -c "INSERT INTO users ... VALUES ('$2a$10$...')"

# 正确做法
echo "INSERT INTO users ... VALUES ('\$2a\$10\$...')" > temp.sql
psql -U sub2api -h 127.0.0.1 -d sub2api -f temp.sql
```

---

### 坑 4：psql 不支持中文路径

**问题**：`psql -f "D:\中文路径\file.sql"` 报错找不到文件。

**解决**：复制到纯英文路径再执行：
```bash
cp "D:\中文路径\file.sql" "C:\temp.sql"
psql -f "C:\temp.sql"
```

---

### 坑 5：PostgreSQL 密码重置流程

**场景**：忘记 PostgreSQL 密码。

**步骤**：
1. 修改 `C:\Program Files\PostgreSQL\16\data\pg_hba.conf`
   ```
   # 将 scram-sha-256 改为 trust
   host    all    all    127.0.0.1/32    trust
   ```
2. 重启 PostgreSQL 服务
   ```powershell
   Restart-Service postgresql-x64-16
   ```
3. 无密码登录并重置
   ```bash
   psql -U postgres -h 127.0.0.1
   ALTER USER sub2api WITH PASSWORD 'sub2api';
   ALTER USER postgres WITH PASSWORD 'postgres';
   ```
4. 改回 `scram-sha-256` 并重启

---

### 坑 6：Go interface 新增方法后 test stub 必须补全

**问题**：给 interface 新增方法后，编译报错 `does not implement interface (missing method XXX)`。

**原因**：所有测试文件中实现该 interface 的 stub/mock 都必须补上新方法。

**解决**：
```bash
# 搜索所有实现该 interface 的 struct
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

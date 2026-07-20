# Hack3rX Sub2API Custom 部署文件

本目录保存 Docker Compose、Apple container、systemd、示例配置和部署辅助脚本。

> 当前项目是自定义 fork，不建议直接使用官方 Sub2API 镜像覆盖部署。生产环境应优先使用本仓库 `dev` 分支构建出的自定义镜像或发布包。

## 推荐部署方式

| 方式 | 适用场景 |
|---|---|
| Docker Compose | 推荐；适合新机器、迁移、测试和生产部署 |
| 本地构建镜像 | 推荐；确保包含本 fork 的兼容、推广、使用记录和备份锁改动 |
| Apple container | 可选；适合 Apple silicon + macOS 26 的本地或自管部署 |
| 二进制 + systemd | 可选；适合已有运维体系 |

## 主要文件

| 文件 | 说明 |
|---|---|
| `docker-compose.yml` | Docker Compose 示例，使用 named volume |
| `docker-compose.local.yml` | 本地目录挂载示例，便于迁移和调试 |
| `docker-deploy.sh` | Docker 部署辅助脚本 |
| `apple-container.sh` | Apple `container` 生命周期脚本 |
| `APPLE_CONTAINER.md` | Apple `container` 部署与运维说明 |
| `.env.example` | 环境变量示例，不能填入真实密钥后提交 |
| `config.example.yaml` | 配置文件示例 |
| `DOCKER.md` | Docker 部署说明 |
| `install.sh` | 二进制安装脚本 |
| `sub2api.service` | systemd 服务示例 |

## Apple Container

Apple silicon 且运行 macOS 26 的机器可使用 Apple `container` 1.1.0 或更高版本。脚本会按顺序启动 PostgreSQL、Redis 和 Sub2API，并执行就绪检查，但不提供持续重启守护；生产部署仍推荐 Docker Compose。

```bash
./apple-container.sh init
./apple-container.sh up
./apple-container.sh status
./apple-container.sh logs app -f
```

使用前必须在 `.env` 中将 `APPLE_CONTAINER_SUB2API_IMAGE` 指向本 fork 构建的镜像，不能直接替换为官方镜像，否则自定义功能会缺失。完整限制见 [APPLE_CONTAINER.md](./APPLE_CONTAINER.md)。

## 环境变量补充

- `NODE_ID`：多实例部署时建议显式设置且每个节点唯一，避免节点本地代理延迟、粘性会话和备份调度状态互相干扰。
- `SERVER_TRUSTED_PROXIES`：反向代理/CDN/LB 场景下用于真实客户端 IP 解析；只填写你控制的代理地址或 CIDR，不要使用 `0.0.0.0/0`。
- `UPDATE_GITHUB_TOKEN`：仅用于 GitHub Release API 更新检查；Release asset 下载仍匿名，不读取 `GITHUB_TOKEN` 或 `GH_TOKEN`。

## 安全注意事项

- 不要提交 `.env`、`config.yaml`、数据库 dump、Redis dump。
- 生产数据库和 Redis 密码必须使用强密码。
- PostgreSQL / Redis 如需公网监听，必须配合防火墙白名单。
- Nginx 建议只暴露 80 / 443，Sub2API 容器端口应仅本机可访问。
- 多实例共用数据库时，定时备份不再依赖 Redis 锁；只有本机 `DATA_DIR/backup_schedule.local.json` 中启用的节点会执行，默认不启用。

## 版本升级注意事项

- 常规小版本升级只替换 `sub2api` 应用容器，不重建 PostgreSQL/Redis 容器，也不要动数据卷。
- 如果新版本包含应用内 migration，必须让新应用容器启动并完成迁移后再切流；多实例部署时先保留一个新应用节点执行迁移，健康检查通过后再滚动升级剩余节点。
- v0.1.145 自定义多时段高峰倍率包含 `159_add_group_peak_rate_windows.sql`，该 migration 只给 `groups` 增加 additive JSONB 字段并从旧单段高峰字段回填第一段。旧容器回退会忽略 `peak_rate_windows`，多窗口只按 legacy 第一段生效。
- 多时段高峰倍率同时覆盖标准余额分组和订阅分组，token、per_request、image、duration、character 计费都会叠加命中的高峰倍率。回退期间如果编辑高峰配置，再升级时新容器读路径会优先使用 legacy 第一段以保持兼容；只有在新版本再次保存分组后，`peak_rate_windows` 才会被持久同步。
- v0.1.153 新增 `174_add_usage_logs_api_key_latest_ip_index_notx.sql`，仅并发创建 usage 查询索引；旧容器会忽略该索引，可继续使用同一数据库。

## 迁移注意事项

如果从旧机器迁移：

1. 先在新机器启动空环境。
2. 演练恢复历史备份。
3. 正式迁移前冻结旧机写流量。
4. 迁移 PostgreSQL / Redis / data。
5. 旧机可临时改连新机数据库，确认稳定后再切 DNS。

具体迁移脚本和机器私有配置不应进入本仓库。

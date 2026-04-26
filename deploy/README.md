# Hack3rX Sub2API Custom 部署文件

本目录保存 Docker Compose、systemd、示例配置和部署辅助脚本。

> 当前项目是自定义 fork，不建议直接使用官方 Sub2API 镜像覆盖部署。生产环境应优先使用本仓库 `dev` 分支构建出的自定义镜像或发布包。

## 推荐部署方式

| 方式 | 适用场景 |
|---|---|
| Docker Compose | 推荐；适合新机器、迁移、测试和生产部署 |
| 本地构建镜像 | 推荐；确保包含本 fork 的兼容、推广、使用记录和备份锁改动 |
| 二进制 + systemd | 可选；适合已有运维体系 |

## 主要文件

| 文件 | 说明 |
|---|---|
| `docker-compose.yml` | Docker Compose 示例，使用 named volume |
| `docker-compose.local.yml` | 本地目录挂载示例，便于迁移和调试 |
| `docker-deploy.sh` | Docker 部署辅助脚本 |
| `.env.example` | 环境变量示例，不能填入真实密钥后提交 |
| `config.example.yaml` | 配置文件示例 |
| `DOCKER.md` | Docker 部署说明 |
| `install.sh` | 二进制安装脚本 |
| `sub2api.service` | systemd 服务示例 |

## 安全注意事项

- 不要提交 `.env`、`config.yaml`、数据库 dump、Redis dump。
- 生产数据库和 Redis 密码必须使用强密码。
- PostgreSQL / Redis 如需公网监听，必须配合防火墙白名单。
- Nginx 建议只暴露 80 / 443，Sub2API 容器端口应仅本机可访问。
- 多实例共用数据库时，定时备份依赖 Redis 锁避免重复执行。

## 迁移注意事项

如果从旧机器迁移：

1. 先在新机器启动空环境。
2. 演练恢复历史备份。
3. 正式迁移前冻结旧机写流量。
4. 迁移 PostgreSQL / Redis / data。
5. 旧机可临时改连新机数据库，确认稳定后再切 DNS。

具体迁移脚本和机器私有配置不应进入本仓库。

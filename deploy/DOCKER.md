# Docker 部署说明

> 当前仓库是 Hack3rX Sub2API Custom，自定义能力较多。
> 生产环境不要直接替换成官方 `weishaw/sub2api` 镜像，否则会丢失本 fork 的兼容、推广、使用记录、自动运维和备份锁改动。

## 本地构建镜像

在仓库根目录执行：

```bash
docker build \
  --build-arg VERSION=0.1.172 \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  -t sub2api-custom:v0.1.172 .
```

或者使用你自己的版本号：

```bash
docker build \
  --build-arg VERSION=0.1.172 \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  -t sub2api-custom:v0.1.172 .
```

## Docker Compose 示例

```yaml
services:
  sub2api:
    image: sub2api-custom:dev
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      AUTO_SETUP: "true"
      DATABASE_HOST: postgres
      DATABASE_PORT: "5432"
      DATABASE_USER: sub2api
      DATABASE_PASSWORD: ${POSTGRES_PASSWORD}
      DATABASE_DBNAME: sub2api
      DATABASE_SSLMODE: disable
      REDIS_HOST: redis
      REDIS_PORT: "6379"
      REDIS_PASSWORD: ${REDIS_PASSWORD}
      REDIS_DB: "0"
      SERVER_MODE: release
      TZ: Asia/Shanghai
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:18-alpine
    restart: unless-stopped
    entrypoint:
      - sh
      - -c
      - |
        set -e
        if [ ! -s "$$PGDATA/PG_VERSION" ] && [ -s "$$PGDATA/pgdata/PG_VERSION" ]; then
          echo "Migrating PostgreSQL data directory from $$PGDATA/pgdata to $$PGDATA"
          find "$$PGDATA/pgdata" -mindepth 1 -maxdepth 1 -exec mv {} "$$PGDATA"/ \;
          rmdir "$$PGDATA/pgdata"
        fi
        exec docker-entrypoint.sh "$@"
      - docker-entrypoint-sh
    command: ["postgres"]
    environment:
      PGDATA: /var/lib/postgresql/data
      POSTGRES_USER: sub2api
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:8-alpine
    restart: unless-stopped
    command: ["redis-server", "--requirepass", "${REDIS_PASSWORD}"]
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Nginx 建议

- 对外只开放 `80/tcp` 与 `443/tcp`。
- Sub2API 的 `8080` 建议绑定 `127.0.0.1`，不要直接公网暴露。
- SSE / 长连接链路需要关闭代理缓冲并设置较长超时。

示例：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

## 多机器部署

多机器共用同一 PostgreSQL / Redis 时：

- 数据库与 Redis 必须使用强密码。
- 数据端口如需公网监听，必须配置防火墙白名单。
- 定时备份由每台应用节点自己的 `DATA_DIR/backup_schedule.local.json` 控制，默认不启用；只在需要执行备份的节点上打开。
- DNS 切换期间可以让新旧应用同时连接同一套数据层，避免数据分叉。

## 版本升级注意事项

- 常规升级只替换 `sub2api` 应用容器，不重建 PostgreSQL/Redis 容器，也不要动数据卷。
- 新版本如包含应用内 migration，必须确认新应用容器启动后 migration 执行成功，再切流或继续滚动升级。
- v0.1.145 自定义多时段高峰倍率包含 `159_add_group_peak_rate_windows.sql`；旧容器回退只识别 `peak_start` / `peak_end` / `peak_rate_multiplier`，多窗口会退化为第一段。
- 多时段高峰倍率同时覆盖标准余额分组和订阅分组，token、per_request、image、duration、character 计费都会叠加命中的高峰倍率。回退期间如果编辑高峰配置，再升级时新容器读路径会优先使用 legacy 第一段以保持兼容；只有在新版本再次保存分组后，`peak_rate_windows` 才会被持久同步。

## 禁止提交的内容

- `.env`
- `config.yaml`
- PostgreSQL / Redis 数据目录
- 真实证书、密钥、Access Key
- Clash / proxy 真实配置
- 迁移临时包和数据库 dump

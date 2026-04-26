# Docker 部署说明

> 当前仓库是 Hack3rX Sub2API Custom，自定义能力较多。
> 生产环境不要直接替换成官方 `weishaw/sub2api` 镜像，否则会丢失本 fork 的兼容、推广、使用记录、自动运维和备份锁改动。

## 本地构建镜像

在仓库根目录执行：

```bash
docker build -t sub2api-custom:dev .
```

或者使用你自己的版本号：

```bash
docker build -t sub2api-custom:0.1.118-custom .
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
      DATABASE_URL: postgres://sub2api:${POSTGRES_PASSWORD}@postgres:5432/sub2api?sslmode=disable
      REDIS_URL: redis://:${REDIS_PASSWORD}@redis:6379/0
      SERVER_MODE: release
      TZ: Asia/Shanghai
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: sub2api
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
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
- 定时备份由 Redis 锁保护，避免多个应用节点同时提交备份。
- DNS 切换期间可以让新旧应用同时连接同一套数据层，避免数据分叉。

## 禁止提交的内容

- `.env`
- `config.yaml`
- PostgreSQL / Redis 数据目录
- 真实证书、密钥、Access Key
- Clash / proxy 真实配置
- 迁移临时包和数据库 dump

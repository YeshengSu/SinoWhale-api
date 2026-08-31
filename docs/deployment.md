# SinoWhale-api 部署指南

> **目标读者**：SRE、运维、负责上线的开发者
> **关联文档**：[打包与部署完整 SOP（PACKAGE_FLOW.md）](../PACKAGE_FLOW.md) · [本地开发指南](./development.md)

---

## 1. 部署模式：离线 bundle

本项目采用「**本地 Windows 打包 Docker 镜像 → 上传服务器 → 离线启动**」模式，**不依赖镜像仓库**：

```text
本地 Windows                              服务器 14.103.22.215
──────────────────────────                ──────────────────────────────
① git tag vX.Y.Z + 写入 VERSION 文件
② scripts\build-swapi-images.ps1
   构建镜像 + 打包 deploy-bundle
   → dist\swapi-deploy-bundle-vX.Y.Z.tar.gz
     （镜像 tar + compose + env 模板
      + install.sh + verify-services.sh）
③ scp / upload-swapi-chunked.ps1  ──────→ ④ tar -xzf 解压到 /opt/swapi-deploy-bundle-vX.Y.Z/
                                           ⑤ 编辑 .env.production（首次填密钥）
                                           ⑥ bash install.sh
                                             （docker load → compose up -d → 健康检查）
                                           → http://localhost:3088
                                           → https://api.sinxwhalex.com（Nginx 反代）
```

要点：

- **服务器无需 git / Go / Bun**：依赖安装与代码构建全部在 Dockerfile 多阶段构建内完成
- `install.sh` 由构建脚本自动生成并注入版本号，服务器上一键执行
- **版本四处一致**（缺一即部署事故）：`git tag` = `VERSION` 文件 = `.env.production` 的 `DEPLOY_VERSION` = `scripts/versions/<版本>.md`

> 逐步命令、热更新、回滚、测试热更新的完整细节见 [PACKAGE_FLOW.md](../PACKAGE_FLOW.md)，本文档不重复。

---

## 2. 三份 Compose 文件的用途

| 文件 | 用途 | 镜像来源 | 能否用于生产 |
|------|------|---------|:---:|
| [docker-compose.yml](../docker-compose.yml) | 上游通用部署示例 | `calciumion/new-api:latest`（官方社区镜像，**不含本项目定制代码**） | ❌ |
| [docker-compose.dev.yml](../docker-compose.dev.yml) | 本地开发联调（方案 C） | `build: .` 本地代码构建 | ❌（注释明确禁止） |
| **[docker-compose.deploy.yml](../docker-compose.deploy.yml)** | **生产部署** | `sinowhalex/swapi:${DEPLOY_VERSION}`（本地构建，经 bundle 离线加载） | ✅ |

**生产部署一律使用 `docker-compose.deploy.yml`**，特性：密码/密钥全部环境变量注入（零硬编码）、全链路 healthcheck、按健康状态编排启动顺序、日志轮转（50MB × 5）、Redis 数据持久化、内置 SWX Header 与短信配置项。

---

## 3. 环境变量清单

环境变量文件为**服务器上 bundle 目录内的 `.env.production`**（模板：[.env.production.example](../.env.production.example)），通过 `docker compose --env-file` 注入，容器内不存在该文件。

| 变量 | 必填 | 说明 | 默认 |
|------|:---:|------|------|
| `DEPLOY_VERSION` | ✅ | 镜像 tag（`install.sh` 自动注入，一般无需手改） | - |
| `POSTGRES_PASSWORD` | ✅ | PostgreSQL 密码（用户 `swapi`，库名 `new-api`） | - |
| `REDIS_PASSWORD` | ✅ | Redis 密码 | - |
| `SESSION_SECRET` | ✅ | Session 密钥，重启不掉线；`openssl rand -hex 32` 生成 | - |
| `TZ` | - | 时区 | `Asia/Shanghai` |
| `ERROR_LOG_ENABLED` | - | 错误日志开关 | `true` |
| `BATCH_UPDATE_ENABLED` | - | 批量更新开关 | `true` |
| `NODE_NAME` | - | 节点名（审计日志标识，多节点需不同） | `swapi-node-1` |
| `SWX_HEADER_ENABLED` | - | SWX 自定义 Header 透传总开关 | `true` |
| `SWX_HEADER_STRICT` | - | 严格模式：非法 Header 返回 400 | `false` |
| `SWX_HEADER_LOG_QUERY_ROLE` | - | 可按 SWX 用户过滤日志的角色 | `admin` |
| `SMS_PROVIDER` | - | 短信服务商（短信验证码登录，v0.2.0 起通用可用） | `aliyun` |
| `SMS_ACCESS_KEY` / `SMS_SECRET_KEY` | - | 短信凭证（启用短信验证码才需要） | - |
| `SMS_SIGN_NAME` / `SMS_TEMPLATE_CODE` | - | 短信签名/模板 | - |

通用运行时变量（流式超时、请求体上限等）见根目录 `.env.example`。

**新增环境变量的五处同步**（代码引用 / 模板声明 / compose 透传 / 服务器实值 / 文档说明）见 [PACKAGE_FLOW.md 第十二章](../PACKAGE_FLOW.md)。

---

## 4. 数据持久化

| 项 | 位置 | 热更新时 |
|---|---|---|
| PostgreSQL 数据 | Docker 卷 `swapi-pg-data` | 跨版本自动复用 |
| Redis 数据 | Docker 卷 `swapi-redis-data` | 跨版本自动复用 |
| 应用数据 | 宿主机挂载 `./data` | **需手动复制到新版本目录** |
| 应用日志 | 宿主机挂载 `./logs` | **需手动复制到新版本目录** |

- `data/`、`logs/` 是相对 bundle 目录的 **bind mount**——切换版本目录后必须手动复制，否则数据/日志"消失"（实际留在旧目录）
- `down` 不会删除 Docker 卷；**彻底清空数据**需 `down -v`（危险，谨慎操作）

---

## 5. 升级与回滚

**升级**（详见 [PACKAGE_FLOW.md 第四章](../PACKAGE_FLOW.md)）：解压新 bundle → 停旧容器 → 复制旧版 `.env.production` + `data/` + `logs/` → `bash install.sh`。

**回滚**：进入旧版本 bundle 目录重跑 `bash install.sh`，秒级（镜像已 load）。

**跨大版本**：`bin/` 下存在对应 `migration_*.sql` 时，升级前需手动执行（先备份卷），常规升级靠启动时 GORM AutoMigrate 自动同步。

**快速验证（不走发版）**：构建 `:test` 镜像，用 `DEPLOY_VERSION=test` 环境变量覆盖重建容器，不动 `.env.production` 与数据卷，详见 [PACKAGE_FLOW.md 第十一章](../PACKAGE_FLOW.md)。

---

## 6. 日常运维命令速查

```bash
# 实时日志
docker logs -f --tail=100 swapi-new-api

# 重启应用（不动数据库/缓存）
docker restart swapi-new-api

# 健康检查（服务器本机 / 外部域名）
curl -s http://localhost:3088/api/status
curl -s https://api.sinxwhalex.com/api/status

# 进入 PostgreSQL / Redis 排查
docker exec -it swapi-postgres psql -U swapi -d new-api
docker exec -it swapi-redis redis-cli -a '<REDIS_PASSWORD>'

# 停止所有 SWAPI 容器（不影响 SWX）
docker compose -p swapi --env-file .env.production -f docker-compose.deploy.yml down
```

> 所有 `docker compose` 命令必须带 `--env-file .env.production`，否则因缺 `DEPLOY_VERSION` 报错。

---

## 7. 与 SinoWhaleX 的共存

SWAPI 与 SWX 部署在**同一台服务器**（`14.103.22.215`），通过命名完全隔离：

| 资源 | SWX | SWAPI |
|------|-----|-------|
| compose 项目 / 容器名 | `sinowhalex` / `sinowhalex-*` | `swapi` / `swapi-*` |
| 主机端口 | 3000-3008 | **3088** |
| bundle 目录 | `/opt/deploy-bundle-*` | `/opt/swapi-deploy-bundle-*`（**禁止简写**，防互相覆盖） |
| 域名 | sinxwhalex.com | api.sinxwhalex.com（共用 Nginx，配置源在 SWX 仓库） |

请求链路：`Nginx (443, proxy_buffering off 支持 SSE) → 127.0.0.1:3088 → swapi-new-api 容器 → swapi-postgres / swapi-redis`。

协作红线：Nginx 变更必须在 SWX 仓库修改提交后同步服务器；发版各自独立；**禁止在服务器上直接改代码**。

---

## 8. 安全清单（上线前逐项确认）

- [ ] `.env.production` 中 `POSTGRES_PASSWORD` / `REDIS_PASSWORD` 已改为强密码（勿用示例值）
- [ ] `SESSION_SECRET` 已重新生成（勿复用任何提交在仓库里的值）
- [ ] `.env.production` 密钥文件备份到 `/opt/.env.swapi.production` 等安全位置（清理旧版本前必备）
- [ ] 3088 端口不直接暴露公网，由 Nginx 反代并配置 HTTPS
- [ ] 防火墙仅放行反代端口，PostgreSQL/Redis 未映射宿主机端口（deploy.yml 默认未映射，勿自行添加）
- [ ] 面向公众提供生成式 AI 服务时，完成所在地备案、内容安全、实名、日志留存等合规义务

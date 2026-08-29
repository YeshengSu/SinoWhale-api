# SWAPI 打包与部署流程

> 本文档参照 SinoWhaleX 的 `PACKAGE_FLOW.md` 标准编写。SWAPI 与 SWX 部署在**同一台服务器**（`14.103.22.215`），均采用「本地 Windows 打包 Docker 镜像 → 上传服务器 → docker load 启动」的离线部署模式。两套流程命令风格一致，但**镜像、compose project、端口、目录命名完全隔离**（见第十三章）。

## 架构概览

```
┌── 本地 Windows ──────────────────────────────────────────────────┐
│                                                                    │
│   ① git tag vX.Y.Z         打版本标签（并同步写入 VERSION 文件）  │
│   ② build-swapi-images.ps1 构建 1 个镜像 → 打包 deploy-bundle    │
│   ③ scp / upload-swapi-chunked.ps1  上传 bundle 到服务器         │
│                                                                    │
└───────────────────────────────┬────────────────────────────────────┘
                                │
                                ▼
┌── 服务器 14.103.22.215 ──────────────────────────────────────────┐
│                                                                    │
│   ④ tar -xzf               解压 bundle 到 /opt/swapi-deploy-bundle-vX.Y.Z │
│   ⑤ nano .env.production   填写数据库/Redis/Session 密码          │
│   ⑥ bash install.sh        加载镜像 → 启动 → 验证                 │
│                                                                    │
│   ┌──────────────────────────────────────────┐                    │
│   │  swapi-new-api 容器（Go 单体，:3088）   │                    │
│   │  内置 web/default + web/classic 双前端  │                    │
│   └──────────────────────────────────────────┘                    │
│   + swapi-postgres (postgres:15)                                  │
│   + swapi-redis (redis:7-alpine)                                  │
│                                                                    │
│   Nginx（SWX 统一管理）: api.sinxwhalex.com → 127.0.0.1:3088     │
└────────────────────────────────────────────────────────────────────┘
```

### 与 SWX 部署架构对比

| 项目         | SWX                                | SWAPI                          |
| ------------ | ---------------------------------- | ------------------------------ |
| 镜像数       | 2（unified-backend + frontend）    | **1（swapi 单镜像）**          |
| 技术栈       | Node.js 微服务 ×8（PM2）+ Next.js  | **Go 单体 + React 双前端**     |
| compose 项目 | `sinowhalex`                       | **`swapi`**                    |
| 对外端口     | 3000（前端）/ 3001-3008（后端）    | **3088**                       |
| 服务器目录   | `/opt/deploy-bundle-vX.Y.Z/`       | **`/opt/swapi-deploy-bundle-vX.Y.Z/`** |
| 数据库       | 独立 postgres/mongo/redis/minio    | **独立 swapi-postgres + swapi-redis** |
| 域名         | sinxwhalex.com                     | **api.sinxwhalex.com**         |

> ⚠️ **命名隔离红线**：SWAPI 的 bundle 目录/压缩包统一使用 `swapi-deploy-bundle-` 前缀，**禁止**简写为 `deploy-bundle-`，否则会与 SWX 在 `/opt` 下的同名目录互相覆盖（历史上两者都用过 `deploy-bundle-vX.Y.Z` 命名）。

---

## 一、文件清单

| 文件                              | 运行位置        | 作用                                              |
| --------------------------------- | --------------- | ------------------------------------------------- |
| `Dockerfile`                      | 本地            | 多阶段构建：bun 编译双前端 → Go 编译 → 运行镜像   |
| `Dockerfile.dev`                  | 本地            | 本地开发调试用                                    |
| `docker-compose.deploy.yml`       | 服务器          | 启动容器用（`image:` + `${DEPLOY_VERSION}`）      |
| `docker-compose.dev.yml`          | 本地            | 本地开发用                                        |
| `scripts/build-swapi-images.ps1`  | 本地 PowerShell | 构建镜像 → 打标签 → 导出 tar → 打包 deploy-bundle |
| `scripts/upload-swapi-chunked.ps1`| 本地 PowerShell | 分块上传大 bundle（避免 SCP Broken pipe）        |
| `scripts/install.sh`              | 服务器 Bash     | 一键安装脚本模板（实际由构建脚本注入版本号生成）  |
| `scripts/verify-services.sh`      | 服务器 Bash     | 服务健康检查验证                                  |
| `.env.production.example`         | 模板            | 生产环境变量模板（复制为 `.env.production`）      |
| `VERSION`                         | 本地            | 版本号文件（构建时注入二进制与前端，见 3.1 注意） |
| `scripts/versions/`               | 本地            | 版本记录文件（每次构建自动生成 `<版本>.md`，随发布提交，见 3.1） |
| `bin/migration_*.sql`             | 服务器手动执行  | 跨大版本数据库迁移脚本（见 3.4）                 |

> 部署包（swapi-deploy-bundle）包含镜像 tar + docker-compose.deploy.yml + .env.production.example + install.sh + verify-services.sh，一个包解决所有问题，服务器不需要 git clone，也不需要 Go/Bun 环境。

---

## 二、环境准备（仅首次）

### 2.1 本地安装 Docker Desktop

从官网下载安装：https://www.docker.com/products/docker-desktop/

安装后必须配置国内镜像加速器，否则无法拉取基础镜像（`oven/bun`、`golang:1.26.1-alpine`、`debian:bookworm-slim` 等）。

打开 **Docker Desktop → Settings → Docker Engine**，替换为：

```json
{
  "registry-mirrors": [
    "https://docker.m.daocloud.io",
    "https://dockerhub.timeweb.cloud",
    "https://mirror.ccs.tencentyun.com"
  ]
}
```

点 **Apply & Restart**，等待 Docker Desktop 重启完成。验证：

```powershell
docker ps
```

### 2.2 服务器环境

服务器 Docker Engine 已随 SWX 部署安装完成（参见 SWX `PACKAGE_FLOW.md` 第二章），SWAPI **无需重复安装**，仅需确认：

```bash
ssh root@14.103.22.215 "docker ps && df -h"
```

### 2.3 依赖安装与代码构建说明（重要）

SWAPI 的**依赖安装与代码构建完全在 Docker 多阶段构建内完成**，本地打包只需要 Docker Desktop + Git：

| 构建阶段            | 基础镜像             | 内容                                            |
| ------------------- | -------------------- | ----------------------------------------------- |
| `builder`           | `oven/bun:1`         | `bun install` → 编译 `web/default` 前端         |
| `builder-classic`   | `oven/bun:1`         | `bun install --filter ./classic` → 编译 classic 前端 |
| `builder2`          | `golang:1.26.1-alpine` | `go mod download` → `go build`（注入 VERSION）|
| 运行镜像            | `debian:bookworm-slim` | 仅含 `new-api` 二进制 + 证书 + 时区           |

也就是说：**本地无需安装 Go / Bun 即可产出生产镜像**。仅在本地开发调试时才需要（Go 1.26+、Bun，配合 `make dev-api` / `make dev-web`，详见 `makefile`）。

> Go 代理已内置在 Dockerfile 中（`GOPROXY=https://goproxy.cn,direct`），国内网络可直接构建。

---

## 三、首次部署

### 3.1 本地操作

> **关于版本号：** 下文示例中的 `vX.Y.Z` 仅为占位符，**请勿原样复制**。`build-swapi-images.ps1` 会自动通过 `git tag --sort=-v:refname` 取仓库最新语义化标签并交互式提示确认；仓库还没有任何 Git 标签时，脚本会要求手动输入版本号。

```powershell
# 0. （可选）查看仓库现有的最新标签
git tag --sort=-v:refname | Select-Object -First 5

# 1. 打新版本标签（semver 规范，按需替换 vX.Y.Z）
git tag vX.Y.Z
git push origin vX.Y.Z

# 2. ★ 将版本号写入 VERSION 文件（必做，勿遗漏）
#    Dockerfile 通过 `cat VERSION` 把版本号注入 Go 二进制（common.Version）
#    和前端（VITE_REACT_APP_VERSION）。该文件为空会导致镜像内版本号为空，
#    后台「系统设置」显示的版本将不可追溯。
Set-Content -Path VERSION -Value "vX.Y.Z" -NoNewline
git add VERSION && git commit -m "chore: bump VERSION to vX.Y.Z" && git push

# 3. 构建镜像 + 打包 deploy-bundle（确保 Docker Desktop 已启动）
#    推荐方式：不传 -Version，由脚本自动识别最新 git tag 并要求回车确认
.\scripts\build-swapi-images.ps1
#    显式指定版本（CI 或确定版本号时使用）：
#    .\scripts\build-swapi-images.ps1 -Version vX.Y.Z
#
# → 输出: dist\swapi-deploy-bundle-<版本>.tar.gz
#   内含: swapi 镜像 tar + docker-compose.deploy.yml + .env 模板
#         + install.sh + verify-services.sh

#     ★ 版本文件（scripts/versions/）：
#       脚本会自动生成 scripts/versions/<版本>.md（参考 SWX deploy/versions/ 机制）：
#          - 版本号 / commit / 发布日期 / 上一版本
#          - 变更描述：优先取环境变量 RELEASE_LOG，否则自动汇总上一版本 tag 到 HEAD 的提交
#       示例：RELEASE_LOG="修复 xxx 问题" .\scripts\build-swapi-images.ps1
#       版本文件必须随本次发布一起提交 git：
#          git add scripts/versions/vX.Y.Z.md && git commit

# 4. 上传到服务器（用实际生成的文件名，不要照抄 vX.Y.Z）
#    小文件可直接 scp：
scp dist\swapi-deploy-bundle-vX.Y.Z.tar.gz root@14.103.22.215:/opt/
#    镜像较大（>200MB）时使用分块上传（50MB/块、断点续传、自动重试）：
.\scripts\upload-swapi-chunked.ps1 -Version vX.Y.Z
```

> **常见误用：** 早期流程使用 `v0.1.0` 等示例值，部分同事直接照抄导致版本号与实际仓库标签错位。请始终以 `git tag --sort=-v:refname` 的输出或脚本交互提示为准。如果 `build-swapi-images.ps1` 因仓库无标签而要求手动输入，请先回到第 1 步打标签，再重跑构建。

### 3.2 服务器部署（逐步）

```bash
ssh root@14.103.22.215
cd /opt

# 1. 解压 bundle（注意 swapi- 前缀，与 SWX 的 deploy-bundle-* 区分）
tar -xzf swapi-deploy-bundle-vX.Y.Z.tar.gz
cd swapi-deploy-bundle-vX.Y.Z

# 2. 配置生产环境变量（首次部署手动填写）
cp .env.production.example .env.production
nano .env.production    # 或 vi .env.production
```

**必须填写的字段**（完整说明见 `.env.production.example` 注释）：

```ini
# ── 部署版本（install.sh 会自动注入，可不改）──
DEPLOY_VERSION=vX.Y.Z

# ── 数据库 / 缓存密码（建议 openssl rand -base64 24 生成）──
POSTGRES_PASSWORD=强密码
REDIS_PASSWORD=强密码

# ── Session 密钥（固定值，防止重启后登录态失效；openssl rand -hex 32）──
SESSION_SECRET=64位随机字符串

# ── SinoWhaleX 集成（有默认值，按需调整）──
SWX_HEADER_ENABLED=true       # 提取 X-SWX-* Header 写入日志 other 字段
SWX_HEADER_STRICT=false       # true 时非法 Header 返回 400
SWX_HEADER_LOG_QUERY_ROLE=admin
```

> SWAPI 不需要 SWX 那样的 `setup-passwords.sh` 交互向导——密钥字段只有 3 个，直接编辑即可。

**可选字段**（短信验证码登录/绑定手机，v0.2.0 起通用可用）：

```ini
# 通用登录三方式之一：手机号 + 短信验证码（发码/绑定手机用）
SMS_PROVIDER=aliyun
SMS_ACCESS_KEY=
SMS_SECRET_KEY=
SMS_SIGN_NAME=
SMS_TEMPLATE_CODE=
```

> 修改任一字段后需 `docker compose up -d` 重建容器生效；遵循第十二章五处同步检查清单。

```bash
# 3. 一键安装（加载镜像 → 启动所有容器 → 健康检查）
bash install.sh
```

`install.sh` 自动执行：

1. `docker load` 加载 swapi 镜像
2. 检查 `.env.production`，不存在则从模板复制并提示先编辑
3. `docker compose -p swapi up -d` 启动（new-api + postgres + redis）
4. `verify-services.sh` 验证

### 3.3 首次初始化：Setup 向导（必做）

容器启动后，GORM `AutoMigrate` 会**自动建表**，无需手动执行 SQL。但超级管理员需要通过 Setup 向导创建：

```text
浏览器访问 https://api.sinxwhalex.com
→ 进入 Setup 向导 → 设置 root 管理员账号密码
→ 登录后台「系统设置」完成渠道/模型等业务配置
```

如需重置向导状态重新走一遍（仅本地/测试环境），参考 `makefile` 的 `reset-setup` 目标——**生产环境禁止执行**。

### 3.4 数据库迁移（仅跨大版本时）

- **常规升级**：靠启动时 `AutoMigrate` 自动同步表结构，无需人工干预。
- **跨大版本升级**：`bin/` 下存在对应迁移脚本时（如 `migration_v0.2-v0.3.sql`），需在升级前手动执行（本地 PowerShell，迁移脚本在本地仓库 `bin/` 下）：

```powershell
# 1. 上传迁移脚本
scp bin\migration_vX.X-vX.X.sql root@14.103.22.215:/tmp/

# 2. 停应用容器，避免升级期间写入，然后执行迁移
ssh root@14.103.22.215 "docker stop swapi-new-api && docker exec -i swapi-postgres psql -U swapi -d new-api < /tmp/migration_vX.X-vX.X.sql"

# 3. 启动新版本
ssh root@14.103.22.215 "cd /opt/swapi-deploy-bundle-vX.Y.Z && bash install.sh"
```

> 升级前请先确认 `bin/` 下是否有当前版本区间对应的脚本，并**备份卷**：
> `docker run --rm -v swapi_swapi-pg-data:/data -v /opt:/backup alpine tar czf /backup/swapi-pg-$(date +%F).tar.gz -C /data .`

### 3.5 域名与 Nginx（已随 SWX 配置，仅验证）

SWAPI 的域名 `api.sinxwhalex.com` 由**服务器上 SWX 统一管理的 Nginx** 反代到 `127.0.0.1:3088`，配置源文件为 SWX 仓库的 `deploy/nginx/sinowhalex.conf`。首次部署只需验证两件事：

1. **DNS 解析**：域名服务商处 `api` 主机记录 A → `14.103.22.215`。

```powershell
ping api.sinxwhalex.com   # 应返回 14.103.22.215
```

2. **Nginx 已包含 api server 块**（监听 443、反代 3088、`proxy_buffering off` 支持 SSE 流式）：

```bash
grep -A5 "api.sinxwhalex.com" /etc/nginx/conf.d/sinowhalex.conf
```

> ⚠️ 修改 Nginx 配置必须遵守 SWX 的「服务器修改标准流程」：在 SWX 仓库的 `deploy/nginx/sinowhalex.conf` 修改 → 提交 → 同步服务器 → `nginx -t && systemctl reload nginx`，**禁止只在服务器上改不回写仓库**。

---

## 四、热更新（后续版本发布）

```powershell
# ========== 本地 ==========

# 1. 打新标签 + 同步 VERSION 文件（见 3.1 第 2 步）
git tag v0.2.0
git push origin v0.2.0
Set-Content -Path VERSION -Value "v0.2.0" -NoNewline
git add VERSION && git commit -m "chore: bump VERSION to v0.2.0" && git push

# 2. 构建新版本 bundle
.\scripts\build-swapi-images.ps1 -Version v0.2.0

# 2.1 提交版本文件（构建自动生成于 scripts/versions/v0.2.0.md）
git add scripts/versions/v0.2.0.md
git commit -m "chore: release v0.2.0"

# 3. 上传（大文件用分块）
.\scripts\upload-swapi-chunked.ps1 -Version v0.2.0
```

```bash
# ========== 服务器 ==========

# 4. 解压并安装
cd /opt
tar -xzf swapi-deploy-bundle-v0.2.0.tar.gz
cd swapi-deploy-bundle-v0.2.0

# 5. ★ 复用旧版本的密码配置与数据（必做，三项都要）
#    .env.production  → 密钥配置
#    data/            → 应用数据目录（compose bind mount ./data）
#    logs/            → 应用日志目录（compose bind mount ./logs）
#    先停应用容器，避免复制期间旧容器仍在写入 data/logs
docker stop swapi-new-api
cp ../swapi-deploy-bundle-v0.1.0/.env.production .
cp -a ../swapi-deploy-bundle-v0.1.0/data .
cp -a ../swapi-deploy-bundle-v0.1.0/logs .

# 6. 一键部署
bash install.sh

# 7. 清理旧版本压缩包（可选）
rm -f /opt/swapi-deploy-bundle-v0.1.0.tar.gz
```

> **热更新原理：** `install.sh` 通过 `docker compose -p swapi up -d` 启动，所有版本共享同一个 project 名（`swapi`），因此 **named volumes**（`swapi-pg-data`、`swapi-redis-data`）跨版本自动复用，**数据不会丢失**。
> **但注意：** `new-api` 容器的 `./data` 和 `./logs` 是相对 bundle 目录的 **bind mount**，切换到新版本目录后必须手动复制（第 5 步），否则数据/日志会「消失」（实际留在旧目录）。

---

## 五、版本回滚

```bash
# 切换到上一版本的 bundle 目录，重新安装
cd /opt/swapi-deploy-bundle-v0.1.0 && bash install.sh
```

回滚原理：服务器上保留各版本的 `swapi-deploy-bundle-vX.X.X/` 目录，要回滚时进入旧版本目录执行 `install.sh` 即可（镜像已 `docker load` 过，秒级完成）。

> 如涉及 3.4 的手动 SQL 迁移，回滚前需评估 schema 兼容性，必要时恢复 `/opt/swapi-pg-*.tar.gz` 备份。

---

## 六、日常运维命令

```bash
# 查看容器状态
docker compose -p swapi --env-file /opt/swapi-deploy-bundle-vX.Y.Z/.env.production \
  -f /opt/swapi-deploy-bundle-vX.Y.Z/docker-compose.deploy.yml ps

# 查看应用实时日志
docker logs -f --tail=100 swapi-new-api

# 重启应用（不动数据库/缓存）
docker restart swapi-new-api

# 进入 PostgreSQL 排查数据
docker exec -it swapi-postgres psql -U swapi -d new-api

# 进入 Redis 排查缓存
docker exec -it swapi-redis redis-cli -a '<REDIS_PASSWORD>'

# 停止所有 SWAPI 容器（不影响 SWX）
docker compose -p swapi --env-file /opt/swapi-deploy-bundle-vX.Y.Z/.env.production \
  -f /opt/swapi-deploy-bundle-vX.Y.Z/docker-compose.deploy.yml down

# 验证所有服务
bash /opt/swapi-deploy-bundle-vX.Y.Z/scripts/verify-services.sh
```

> **注意：** 所有 `docker compose` 命令都必须带 `--env-file .env.production`，否则会因找不到 `DEPLOY_VERSION` 等变量而报错。SWAPI（project `swapi`）与 SWX（project `sinowhalex`）互不影响，`down`/`restart` 不会波及对方。

---

## 七、版本管理规范

### 7.1 版本号规则（semver）

```
v<MAJOR>.<MINOR>.<PATCH>

MAJOR  不兼容的 API/数据库变更   v1.0.0 → v2.0.0
MINOR  新增功能，向后兼容         v1.0.0 → v1.1.0
PATCH  Bug 修复，向后兼容         v1.0.0 → v1.0.1
```

### 7.2 版本四处一致

每次发版，以下四处必须指向同一版本号：

| 位置                | 说明                                            |
| ------------------- | ----------------------------------------------- |
| `git tag`           | `vX.Y.Z`，push 到远端                            |
| `VERSION` 文件      | 构建时注入二进制与前端，空值=版本号丢失          |
| `DEPLOY_VERSION`    | `.env.production` 中，`install.sh` 会自动注入    |
| `scripts/versions/` | 版本记录文件（构建自动生成 `<版本>.md`，随发布提交） |

```powershell
# 创建标签
git tag v0.2.0 -m "新增 xxx 功能"

# 查看所有标签
git tag -l

# 删除本地/远程标签
git tag -d v0.2.0
git push origin --delete v0.2.0
```

### 7.3 服务器版本目录

服务器上各版本独立存放，每次上传新 bundle 解压到 `/opt/swapi-deploy-bundle-vX.X.X/`：

```bash
ssh root@14.103.22.215
ls -d /opt/swapi-deploy-bundle-*/
```

当前运行的是哪个版本，取决于你在哪个目录执行了 `bash install.sh`。

### 7.4 清理旧版本

镜像已加载到 Docker 中，解压目录和压缩包都可以安全删除：

```bash
# 删除所有旧版本压缩包
rm -f /opt/swapi-deploy-bundle-*.tar.gz

# 删除旧版本解压目录（确认不再需要回滚后）
rm -rf /opt/swapi-deploy-bundle-v0.1.*
```

> **注意：** 删除前先确认 `.env.production` 已备份到安全位置（如 `/opt/.env.swapi.production`）。

---

## 八、故障排查

### Docker Desktop 无法拉取镜像

```
ERROR: failed to fetch anonymous token from registry.docker.io
```

原因：国内网络无法直连 Docker Hub。解决：按 2.1 节配置镜像加速器。Go 模块下载失败则检查 Dockerfile 中 `GOPROXY=https://goproxy.cn,direct` 是否保留。

### scp 无法连接服务器 / Broken pipe

```powershell
ssh root@14.103.22.215 "echo ok"   # 测试连通性
```

大文件（>200MB）单连接长传输超时 → 改用 `.\scripts\upload-swapi-chunked.ps1` 分块上传（支持断点续传，单块失败自动重试 5 次）。

### 服务器 docker load 失败

```bash
df -h                    # 检查磁盘空间
systemctl status docker  # 检查 Docker 是否运行
```

### 容器启动后 immediately 退出

```bash
docker logs --tail=100 swapi-new-api
```

常见原因：

1. `SQL_DSN` 密码错误 → 核对 `.env.production` 的 `POSTGRES_PASSWORD` 与 postgres 容器实际值
2. `SESSION_SECRET` 为空 → 检查是否漏填
3. postgres/redis 未就绪 → `install.sh` 已配置 `depends_on: service_healthy`，若仍失败查看对应容器日志

### /api/status 返回异常

```bash
# 容器内自检（绕过 Nginx）
docker exec swapi-new-api wget -q -O - http://localhost:3000/api/status

# 数据库连通性
docker exec swapi-postgres pg_isready -U swapi -d new-api
docker exec swapi-redis redis-cli -a '<REDIS_PASSWORD>' ping
```

容器内正常但 `https://api.sinxwhalex.com` 异常 → Nginx / DNS 问题，按 3.5 节排查。

### 与 SWX 相互影响排查

两套系统仅共享：服务器资源、Nginx、80/443 端口。SWAPI 异常时先确认影响面：

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}' | grep -E 'swapi|sinowhalex'
```

- SWAPI 容器全挂、SWX 正常 → 问题在 `swapi` project 内部
- 两者同时异常 → 怀疑服务器级问题（磁盘/内存/Docker daemon）

---

## 九、常用命令速查表

| 场景          | 命令                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------------------------- |
| 首次打包      | `git tag vX.Y.Z` + 写入 `VERSION` → `.\scripts\build-swapi-images.ps1`                                  |
| 上传 bundle   | `scp dist\swapi-deploy-bundle-vX.Y.Z.tar.gz root@14.103.22.215:/opt/` 或 `.\scripts\upload-swapi-chunked.ps1 -Version vX.Y.Z` |
| 首次部署      | `tar -xzf ... && cd swapi-deploy-bundle-vX.Y.Z` → 编辑 `.env.production` → `bash install.sh`            |
| 热更新部署    | 解压新版本 → 复用旧版 `.env.production` + `data/` + `logs/` → `bash install.sh`                         |
| 查看日志      | `docker logs -f --tail=100 swapi-new-api`                                                               |
| 重启应用      | `docker restart swapi-new-api`                                                                          |
| 健康检查      | `curl -s http://localhost:3088/api/status`（服务器）/ `https://api.sinxwhalex.com/api/status`（外部）    |
| 停止服务      | `docker compose -p swapi --env-file .env.production -f docker-compose.deploy.yml down`                  |
| 清理旧版本    | `rm -f /opt/swapi-deploy-bundle-*.tar.gz && rm -rf /opt/swapi-deploy-bundle-v旧版本/`                   |
| 查看本地镜像  | `docker images sinowhalex/swapi`                                                                        |
| 查看 bundle   | `dir dist\swapi-deploy-bundle-*.tar.gz`（本地）                                                         |

---

## 十、目录结构

```
本地 Windows:
  dist/
  ├── swapi-deploy-bundle-vX.Y.Z.tar.gz   # ★ 部署包（镜像 + 配置 + 脚本）
  └── swapi-deploy-bundle-vX.Y.Z/         # 解压后的部署目录（构建中间产物）
      ├── install.sh                      # 一键安装脚本（构建时生成）
      ├── docker-compose.deploy.yml
      ├── .env.production.example
      ├── swapi-vX.Y.Z.tar                # 镜像 tar（docker save 导出）
      └── scripts/
          └── verify-services.sh

服务器 14.103.22.215:
  /opt/
  ├── deploy-bundle-vX.Y.Z/               # SWX 的部署目录（勿动）
  ├── swapi-deploy-bundle-vX.Y.Z.tar.gz   # SWAPI 上传的部署包
  └── swapi-deploy-bundle-vX.Y.Z/         # SWAPI 运行时目录
      ├── install.sh
      ├── .env.production                 # ★ 密码配置（首次手动填写，后续复用）
      ├── data/                           # ★ 应用数据 bind mount（热更新需迁移）
      ├── logs/                           # ★ 应用日志 bind mount（热更新需迁移）
      └── ...
```

### 镜像清单

| 镜像名             | 内容                                                       |
| ------------------ | ---------------------------------------------------------- |
| `sinowhalex/swapi` | Go 后端 + web/default + web/classic 双前端（单镜像单进程） |

---

## 十一、测试热更新（不打正式 tag，快速验证）

> **适用场景：** 仅修改少量代码，需要在生产服务器上快速验证，不希望升级 `DEPLOY_VERSION`、不希望走完整发版流程。
>
> **与 SWX 的关键差异：** SWX 后端是 Node.js，可以 `npx tsc` 后 SCP 单个 `.js` 文件 + `pm2 reload`（30 秒生效）。**SWAPI 是 Go 静态编译单体，无法替换单文件，必须重建镜像**。
>
> **核心技巧：** 镜像 tag 由 `${DEPLOY_VERSION}` 插值而来，且 **shell 环境变量优先级高于 `--env-file`**（与 `install.sh` 注入版本的机制相同）。因此只需用 `DEPLOY_VERSION=test` 临时覆盖，让 compose 拉起 `:test` 镜像——**环境变量构造（`SQL_DSN`/`REDIS_CONN_STRING`）、数据卷、健康检查全部复用 compose 配置，无需手写 `docker run`**。

```powershell
# 1. 本地构建 :test 镜像（源码层缓存失效即可，无需 --no-cache 全量重建）
docker build -t sinowhalex/swapi:test .

# 2. 导出 tar，复制为分块脚本约定的文件名后复用其上传（50MB/块、断点续传）
docker save sinowhalex/swapi:test -o $env:TEMP\swapi-test.tar
Copy-Item $env:TEMP\swapi-test.tar "dist\swapi-deploy-bundle-test.tar.gz" -Force
.\scripts\upload-swapi-chunked.ps1 -Version test
# → 服务器得到 /opt/swapi-deploy-bundle-test.tar.gz（内容为镜像 tar，docker load 自动识别）

# 3. 服务器加载镜像，并用 :test 重建容器（不动 .env.production、不动数据卷）
ssh root@14.103.22.215 "docker load -i /opt/swapi-deploy-bundle-test.tar.gz"
ssh root@14.103.22.215 "cd /opt/swapi-deploy-bundle-vX.Y.Z && DEPLOY_VERSION=test docker compose -p swapi --env-file .env.production -f docker-compose.deploy.yml up -d new-api"

# 4. 验证
ssh root@14.103.22.215 "docker ps --filter name=swapi-new-api --format 'table {{.Image}}\t{{.Status}}' && curl -s http://localhost:3088/api/status"

# 5. 清理临时文件
ssh root@14.103.22.215 "rm -f /opt/swapi-deploy-bundle-test.tar.gz"
```

**回滚（恢复正式版本）：**

```bash
# .env.production 里的 DEPLOY_VERSION 仍是正式版本号，直接重新 up 即可
ssh root@14.103.22.215 "cd /opt/swapi-deploy-bundle-vX.Y.Z && bash install.sh"
# 或显式指定：
ssh root@14.103.22.215 "cd /opt/swapi-deploy-bundle-vX.Y.Z && DEPLOY_VERSION=vX.Y.Z docker compose -p swapi --env-file .env.production -f docker-compose.deploy.yml up -d new-api"
```

> 📌 **建议**：将测试改动放在临时分支 `git checkout -b temp/test-xxx`，测试通过后合并回主分支并走正式 bundle 流程（含 VERSION 更新 + git tag）。

### 测试 vs 正式部署对比

| 维度     | 测试热更新                                     | 正式部署                                       |
| -------- | ---------------------------------------------- | ---------------------------------------------- |
| 构建     | `docker build :test` → tar → scp（分钟级）     | `build-swapi-images.ps1` → bundle（10-15 分钟）|
| Git      | 临时分支（`temp/test-xxx`）或不 commit         | 必须 commit 主分支 + 打 tag + 更新 VERSION     |
| 版本号   | 不升 `DEPLOY_VERSION`                          | 必须升级 `DEPLOY_VERSION` + git tag            |
| 镜像 tag | `:test`                                        | `:vX.Y.Z`                                      |
| 影响范围 | 仅 `swapi-new-api` 容器，重启容器即恢复        | 写入 bundle 目录 + `.env.production`，永久生效 |
| 回滚     | `docker compose up -d` 按正式版本重建容器      | 进入旧版本 bundle 目录重新 `install.sh`        |

---

## 十二、环境变量管理规范

> **核心原则**：`.env.production` 含真实生产密钥不进 Git，但**字段结构必须在 `.env.production.example` 中可追溯**。任何新增字段必须保证「代码引用 / 模板声明 / 服务器实值 / 文档说明 / docker-compose 透传」五处同步，少一处即视为部署事故。

### 12.1 服务器 `.env.production` 实际位置

- 路径：`/opt/swapi-deploy-bundle-vX.Y.Z/.env.production`（版本号随 `DEPLOY_VERSION` 变化）
- 加载方式：`docker compose --env-file` 注入到 `docker-compose.deploy.yml` 的 `${VAR}` 占位符
- 容器内不存在该文件，变量通过 compose 的 `environment:` 段下发

### 12.2 标准新增流程

1. **代码侧添加读取**（`common/env.go` / 对应 config）
2. **更新 `.env.production.example`**（含注释和占位值）
3. **修改 `docker-compose.deploy.yml`** — 在 `new-api` 服务 `environment:` 段加 `NEW_VAR: ${NEW_VAR}`
4. **本地验证** — `make dev-api` 或 `docker compose -f docker-compose.dev.yml up -d`
5. **同批次 Git 提交** — 代码 + example + compose + 本文档同 commit
6. **服务器侧追加真实值 + 重启**：

```powershell
$ts = Get-Date -Format "yyyyMMdd_HHmmss"
# 备份
ssh root@14.103.22.215 "cp /opt/swapi-deploy-bundle-vX.Y.Z/.env.production /opt/swapi-deploy-bundle-vX.Y.Z/.env.production.bak.$ts"
# 追加新字段
ssh root@14.103.22.215 "echo 'NEW_VAR=真实生产值' >> /opt/swapi-deploy-bundle-vX.Y.Z/.env.production"
# 验证写入并重启
ssh root@14.103.22.215 "grep NEW_VAR /opt/swapi-deploy-bundle-vX.Y.Z/.env.production"
ssh root@14.103.22.215 "cd /opt/swapi-deploy-bundle-vX.Y.Z && docker compose -p swapi --env-file .env.production -f docker-compose.deploy.yml up -d"
```

### 12.3 密钥隔离硬性规定

- ✅ 下载到**项目目录之外**（如 `E:\MyProject\SinoWhaleX-secrets\swapi\`）
- ❌ **严禁**下载到 `e:\Codes\SinoWhale-api\` 内任何位置（避免 `git add .` 误提交、避免打进镜像构建上下文）
- ❌ **严禁**用 Windows 默认记事本编辑（CRLF + BOM 会破坏 docker-compose 解析），用 VSCode 并确认 LF + UTF-8 without BOM

### 12.4 字段同步检查清单

每次新增 / 修改 / 删除 env 字段，提交前必须全部勾选：

- [ ] **代码引用**：`process.env` / `os.Getenv` 已读取并校验（Go 侧为 `common/env.go`）
- [ ] **模板声明**：`.env.production.example` 已添加（含注释和占位值）
- [ ] **Compose 透传**：`docker-compose.deploy.yml` 的 `new-api.environment:` 已包含 `${NEW_VAR}`
- [ ] **服务器实值**：`/opt/swapi-deploy-bundle-vX.Y.Z/.env.production` 已写入且备份了旧文件
- [ ] **文档说明**：本文档或 `.env.production.example` 注释已记录用途
- [ ] **服务重启**：`docker compose up -d` 已执行，容器 healthy
- [ ] **Git 提交**：example + compose + 代码同 commit，且未含 `.env.production` 本身

---

## 十三、与 SinoWhaleX 的共存与集成

### 13.1 资源隔离总览

| 资源         | SWX                        | SWAPI                     | 冲突风险 |
| ------------ | -------------------------- | ------------------------- | -------- |
| compose 项目 | `sinowhalex`               | `swapi`                   | 无       |
| 容器名       | `sinowhalex-*`             | `swapi-*`                 | 无       |
| Docker 网络  | `sinowhalex_default`       | `swapi_swapi-network`     | 无       |
| 数据卷       | `sinowhalex_*` named vols  | `swapi_swapi-pg-data` 等  | 无       |
| 主机端口     | 3000-3008                  | 3088                      | 无       |
| bundle 目录  | `/opt/deploy-bundle-*`     | `/opt/swapi-deploy-bundle-*` | 已通过前缀规避 |
| Nginx        | sinxwhalex.com server 块   | api.sinxwhalex.com server 块 | 共用同一 nginx 进程与证书 |

### 13.2 请求链路

```
用户/SWX 前端
   │  X-SWX-* Header（方案 C：用户身份透传）
   ▼
Nginx (api.sinxwhalex.com, 443)
   │  proxy_pass http://127.0.0.1:3088（proxy_buffering off，支持 SSE 流式）
   ▼
swapi-new-api 容器
   │  SWX_HEADER_ENABLED=true 时提取 Header 写入日志 other 字段
   ▼
swapi-postgres / swapi-redis
```

`SWX_HEADER_*` 三个变量控制 Header 提取行为（开关 / 严格模式 / 日志查询角色），详细语义见 `.env.production.example` 注释与 `middleware/swx_header.go`。

### 13.3 协作红线

1. **Nginx 是共享资源**：任何 `api.sinxwhalex.com` 相关的 Nginx 变更，在 SWX 仓库 `deploy/nginx/sinowhalex.conf` 修改并提交，再同步服务器。
2. **端口申请**：SWAPI 后续需要新端口时，避开 SWX 已占用的 3000-3008，并在此文档登记。
3. **发版独立**：两项目版本号各自独立（SWAPI `vX.Y.Z` 与 SWX `vX.Y.Z` 无对应关系），互不要求同步发版。
4. **服务器标准流程一致**：所有改动先本地修改 → 验证 → commit → 构建产物 → 上传 → 重启 → 验证，**禁止在服务器上直接改代码**（同 SWX dev-conventions 第 3 条）。

# SinoWhale-api 本地启动指南（方案 C 联调）

> **适用场景**：本地没有 Go 环境，需要通过 Docker 构建本地镜像运行修改后的 SinoWhale-api，与 SinoWhaleX `ai-service` 进行方案 C 端到端联调。
>
> **目标读者**：SinoWhale-api 开发者、SWX 协同测试人员
>
> **预计耗时**：首次构建 3-5 分钟；后续重建 1 分钟左右

---

## 目录

- [1. 前置依赖检查](#1-前置依赖检查)
- [2. 端口规划](#2-端口规划)
- [3. 启动流程](#3-启动流程)
- [4. 验证启动成功](#4-验证启动成功)
- [5. 与 SWX 协同联调](#5-与-swx-协同联调)
- [6. 代码修改后重建](#6-代码修改后重建)
- [7. 停止与清理](#7-停止与清理)
- [8. 常见问题排查](#8-常见问题排查)

---

## 1. 前置依赖检查

### 1.1 必需软件

| 软件 | 版本 | 验证命令 |
|------|------|---------|
| Docker Desktop | ≥ 20.x | `docker --version` |
| PowerShell | 5.1+ | `$PSVersionTable.PSVersion` |

### 1.2 SWX 基础设施容器已运行

本指南**复用 SinoWhaleX 项目已经启动的 PostgreSQL / Redis 容器**，无需再开第二套数据库。先确认它们存在：

```powershell
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

期望看到（至少前两项）：

```
NAMES                   STATUS              PORTS
sinowhalex-postgres-1   Up X hours (healthy) 0.0.0.0:5432->5432/tcp
sinowhalex-redis-1      Up X hours (healthy) 0.0.0.0:6379->6379/tcp
```

> ⚠️ 如果没有运行：先到 `e:\MyProject\SinoWhaleX` 执行 `docker-compose -f docker-compose.dev.yml up -d` 启动基础设施。

### 1.3 Docker 网络已存在

```powershell
docker network ls | Select-String "sinowhalex"
```

期望输出：

```
sinowhalex_default
```

---

## 2. 端口规划

启动后实际占用：

| 端口 | 服务 | 用途 |
|------|------|------|
| 3088 | **SinoWhale-api Gateway** | 你即将启动的本地构建版本 |
| 3000 | SWX 前端 (Next.js) | 不冲突 |
| 3003 | SWX ai-service | 不冲突 |
| 5432 | PostgreSQL | SWX 已运行 |
| 6379 | Redis | SWX 已运行 |

> ⚠️ **如果你之前用过官方镜像 `docker-compose up -d`**：先 `docker-compose down` 释放 3088 端口。

---

## 3. 启动流程

### Step 1: 构建本地镜像（首次必做）

```powershell
cd e:\Code\SinoWhale-api
docker build -t new-api-plan-c .
```

⏳ **首次构建预计 3-5 分钟**：
- 拉取 Go 1.26 / Bun 镜像（≈ 1.5GB）
- 编译前端 `web/default` 和 `web/classic`
- 编译 Go 二进制 `new-api`

构建成功后验证：

```powershell
docker images | Select-String "new-api-plan-c"
```

期望输出：

```
new-api-plan-c   latest   <image-id>   X seconds ago   ~150MB
```

### Step 2: 准备 PostgreSQL 数据库

如果之前未使用过 new-api，PostgreSQL 中没有 `new-api` 库，需要先创建：

```powershell
docker exec -it sinowhalex-postgres-1 psql -U admin -d postgres -c "CREATE DATABASE \"new-api\";"
```

> 注：SWX 的 postgres 默认用户是 `admin` / 密码 `dev_password`。如果你按 SinoWhale-api 官方 docker-compose 部署的 postgres（用户 `root` / 密码 `123456`），改用相应账号。

### Step 3: 启动容器

**推荐方式：加入 SWX 的 Docker 网络**（容器互通，连接更稳）

```powershell
docker run -d `
  --name new-api-plan-c `
  --network sinowhalex_default `
  -p 3088:3000 `
  -v ${PWD}/data:/data `
  -v ${PWD}/logs:/app/logs `
  -e SQL_DSN="postgresql://admin:dev_password@sinowhalex-postgres-1:5432/new-api" `
  -e REDIS_CONN_STRING="redis://sinowhalex-redis-1:6379" `
  -e SWX_HEADER_ENABLED="true" `
  -e SWX_HEADER_STRICT="false" `
  -e SWX_HEADER_LOG_QUERY_ROLE="admin" `
  -e TZ="Asia/Shanghai" `
  -e ERROR_LOG_ENABLED="true" `
  new-api-plan-c
```

**备选方式：用 host.docker.internal 连本机服务**

```powershell
docker run -d `
  --name new-api-plan-c `
  -p 3088:3000 `
  -v ${PWD}/data:/data `
  -v ${PWD}/logs:/app/logs `
  -e SQL_DSN="postgresql://admin:dev_password@host.docker.internal:5432/new-api" `
  -e REDIS_CONN_STRING="redis://host.docker.internal:6379" `
  -e SWX_HEADER_ENABLED="true" `
  -e SWX_HEADER_STRICT="false" `
  -e SWX_HEADER_LOG_QUERY_ROLE="admin" `
  -e TZ="Asia/Shanghai" `
  new-api-plan-c
```

### Step 4: 查看启动日志

```powershell
docker logs new-api-plan-c -f --tail 50
```

期望看到（关键字 `Server started`）：

```
[INFO] 2026/06/22 - 14:00:01 | route registered
[INFO] 2026/06/22 - 14:00:02 | server started, listening on port 3000
```

按 `Ctrl+C` 退出日志（不会停止容器）。

---

## 4. 验证启动成功

### 4.1 健康检查

```powershell
curl http://localhost:3088/api/status
```

期望返回：

```json
{"success": true, "data": {...}}
```

### 4.2 访问管理后台

浏览器打开：

```
http://localhost:3088
```

默认管理员账号：`root` / `123456`（首次登录后**必须改密码**）

### 4.3 完成一次性后台配置（联调前必做）

| # | 操作 | 路径 |
|---|------|------|
| ① | 创建用户 `sinowhalex_service` | 用户管理 → 添加用户 |
| ② | 创建分组 `sinowhalex` | 设置 → 分组管理 |
| ③ | 创建一个 Channel（可用 OpenAI Mock 测试） | 渠道 → 添加渠道 |
| ④ | 为 `sinowhalex_service` 创建 Token | 令牌 → 添加令牌（**复制保存 sk-xxx**） |

---

## 5. 与 SWX 协同联调

### 5.1 在 SWX 侧配置环境变量

编辑 `e:\MyProject\SinoWhaleX\.env`：

```env
AI_API_BASE_URL=http://localhost:3088
AI_API_TOKEN=sk-xxxxxxxxxxx        # 上一步复制的 Token
AI_INJECT_SWX_HEADERS=true
AI_USD_TO_CREDITS=100
POINT_SERVICE_INTERNAL_URL=http://localhost:3001/internal
POINT_SERVICE_API_KEY=<your-internal-key>
```

### 5.2 手动 curl 冒烟测试

```powershell
curl -X POST http://localhost:3088/v1/chat/completions `
  -H "Authorization: Bearer sk-xxxxxxxxxxx" `
  -H "X-SWX-User-Id: user_test001" `
  -H "X-SWX-Trace-Id: swx-test-001" `
  -H "X-SWX-Biz-Type: text" `
  -H "X-SWX-Request-Id: req-001" `
  -H "Content-Type: application/json" `
  -d '{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}'
```

### 5.3 验证日志落库

浏览器打开 `http://localhost:3088/log`，找到刚才的请求记录，展开 `Other` 字段应能看到：

```json
{
  "swx_user_id": "user_test001",
  "swx_trace_id": "swx-test-001",
  "swx_biz_type": "text",
  "swx_request_id": "req-001",
  ...
}
```

或用 API 按维度查询（需先以管理员身份登录获取 cookie）：

```powershell
curl "http://localhost:3088/api/log/?swx_user_id=user_test001" `
  -H "Cookie: session=..." `
  -H "New-Api-User: 1"
```

### 5.4 SWX ai-service 协同启动

```powershell
# 新开终端
cd e:\MyProject\SinoWhaleX\services\ai-service
pnpm run dev
```

成功后调用 `POST http://localhost:3003/api/ai/text/generate` 即可触发整条链路。

---

## 6. 代码修改后重建

每次修改 `e:\Code\SinoWhale-api` 下的 Go 代码后：

```powershell
# 停止并删除旧容器
docker stop new-api-plan-c
docker rm new-api-plan-c

# 重新构建（有 layer 缓存，通常 1 分钟以内）
cd e:\Code\SinoWhale-api
docker build -t new-api-plan-c .

# 重新启动（复用 Step 3 的命令）
docker run -d `
  --name new-api-plan-c `
  --network sinowhalex_default `
  -p 3088:3000 `
  -v ${PWD}/data:/data `
  -v ${PWD}/logs:/app/logs `
  -e SQL_DSN="postgresql://admin:dev_password@sinowhalex-postgres-1:5432/new-api" `
  -e REDIS_CONN_STRING="redis://sinowhalex-redis-1:6379" `
  -e SWX_HEADER_ENABLED="true" `
  -e SWX_HEADER_STRICT="false" `
  -e TZ="Asia/Shanghai" `
  new-api-plan-c
```

> 💡 **加速建议**：把上面三条命令保存为 `restart.ps1`，每次一键重启。

---

## 7. 停止与清理

### 仅停止

```powershell
docker stop new-api-plan-c
```

### 删除容器（保留数据卷）

```powershell
docker stop new-api-plan-c
docker rm new-api-plan-c
```

### 完全清理（含数据卷和镜像）

```powershell
docker stop new-api-plan-c
docker rm new-api-plan-c
docker rmi new-api-plan-c
Remove-Item -Recurse -Force e:\Code\SinoWhale-api\data
Remove-Item -Recurse -Force e:\Code\SinoWhale-api\logs
```

---

## 8. 常见问题排查

### 8.1 启动失败：端口已被占用

```
Error response from daemon: ports are not available: ... bind: address already in use
```

**排查**：

```powershell
netstat -ano | findstr :3088
```

找到占用 PID 后：

```powershell
taskkill /PID <pid> /F
```

或者删除已存在的 new-api 容器：

```powershell
docker ps -a | findstr new-api
docker rm -f <container-name>
```

### 8.2 数据库连接失败

容器日志显示 `dial tcp ... connection refused`：

| 可能原因 | 解决 |
|---------|------|
| PostgreSQL 未运行 | `docker start sinowhalex-postgres-1` |
| `new-api` 数据库不存在 | 重做 Step 2 创建数据库 |
| 用户名/密码不匹配 | 进 SWX `.env` 查 `POSTGRES_USER` / `POSTGRES_PASSWORD` |
| 容器不在同一网络 | 改用 `--network sinowhalex_default` |

### 8.3 X-SWX-* Header 没有写入 logs.other

| 检查项 | 命令 |
|--------|------|
| `SWX_HEADER_ENABLED` 是否为 `true` | `docker exec new-api-plan-c env \| grep SWX` |
| 代码是否包含改动 | `docker exec new-api-plan-c /new-api --version` |
| Header 字符是否合法 | userId 必须匹配 `^[A-Za-z0-9_-]{1,128}$` |

如果发现 env 没有 `SWX_HEADER_ENABLED=true`，重建容器并加上参数。

### 8.4 镜像构建失败：网络超时

如果 `docker build` 拉取依赖时超时，配置 Docker 镜像加速器（`Docker Desktop → Settings → Docker Engine`）：

```json
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://hub-mirror.c.163.com"
  ]
}
```

保存后重启 Docker Desktop 再重试。

### 8.5 进入容器调试

```powershell
# 进入 shell
docker exec -it new-api-plan-c /bin/bash

# 查看运行的进程
docker exec new-api-plan-c ps aux

# 实时查看日志
docker logs -f new-api-plan-c
```

---

## 9. 启动一键脚本（可选）

把以下内容保存为 `e:\Code\SinoWhale-api\start-dev.ps1`：

```powershell
# start-dev.ps1 - SinoWhale-api 方案 C 本地开发启动脚本
$ErrorActionPreference = "Stop"

Write-Host "[1/4] 停止旧容器..." -ForegroundColor Cyan
docker stop new-api-plan-c 2>$null
docker rm new-api-plan-c 2>$null

Write-Host "[2/4] 构建镜像..." -ForegroundColor Cyan
docker build -t new-api-plan-c .

Write-Host "[3/4] 启动容器..." -ForegroundColor Cyan
docker run -d `
  --name new-api-plan-c `
  --network sinowhalex_default `
  -p 3088:3000 `
  -v ${PWD}/data:/data `
  -v ${PWD}/logs:/app/logs `
  -e SQL_DSN="postgresql://admin:dev_password@sinowhalex-postgres-1:5432/new-api" `
  -e REDIS_CONN_STRING="redis://sinowhalex-redis-1:6379" `
  -e SWX_HEADER_ENABLED="true" `
  -e SWX_HEADER_STRICT="false" `
  -e SWX_HEADER_LOG_QUERY_ROLE="admin" `
  -e TZ="Asia/Shanghai" `
  -e ERROR_LOG_ENABLED="true" `
  new-api-plan-c

Write-Host "[4/4] 等待启动..." -ForegroundColor Cyan
Start-Sleep -Seconds 3
docker logs new-api-plan-c --tail 20

Write-Host "`n✅ 启动完成！" -ForegroundColor Green
Write-Host "Console: http://localhost:3088" -ForegroundColor Yellow
Write-Host "API:     http://localhost:3088/v1/chat/completions" -ForegroundColor Yellow
```

使用：

```powershell
cd e:\Code\SinoWhale-api
.\start-dev.ps1
```

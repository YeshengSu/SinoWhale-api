# SinoWhale-api 本地开发指南

> **目标读者**：SinoWhale-api 前后端开发者
> **关联文档**：[部署指南](./deployment.md)、[方案 C Docker 联调](./integration/LOCAL-DEV-DOCKER.md)

---

## 1. 架构原理（为什么必须先构建前端）

本项目最终产物是**单个 Go 二进制**：前端构建产物在**编译时**通过 `go:embed`（[main.go](../main.go)）嵌入二进制，由同一个 Gin 服务在默认端口 **3000** 上同时提供 API 和静态页面。

因此：**`web/default/dist` 和 `web/classic/dist` 不存在时，Go 编译会直接失败**（报 `pattern web/*/dist: no matching files found`）。任何开发形态都要求先完成一次前端构建。

- 数据库默认 **SQLite**（根目录 `one-api.db`，GORM 自动建表，零配置），可用 `SQL_DSN` 切换 MySQL/PostgreSQL
- 首次访问 `http://localhost:3000` 会进入 setup 向导创建 root 管理员

---

## 2. 前置依赖

| 软件 | 版本要求 | 验证命令 |
|------|---------|---------|
| Go | ≥ 1.25.1（以 `go.mod` 为准） | `go version` |
| Bun | ≥ 1.1（前端包管理器，项目 lockfile 基于 Bun） | `bun --version` |
| Docker（仅方案 C） | ≥ 20.x | `docker --version` |

> 可用 npm 替代 Bun，但项目 lockfile 与约定基于 Bun，混用可能出现依赖版本偏差。

---

## 3. 根目录命令一览

项目根目录提供 [package.json](../package.json)，所有命令**在项目根目录直接运行**，无需 `cd` 进子目录：

| 命令 | 作用 |
|------|------|
| `bun run setup` | 一次性准备：安装前端依赖 + 构建两个前端主题 |
| `bun run dev` | 启动后端（`go run main.go`，:3000） |
| `bun run dev:fe` | 启动前端 dev server（default 主题，:5173，热更新） |
| `bun run dev:fe:classic` | 启动前端 dev server（classic 主题，:5174，热更新） |
| `bun run build:fe` | 重新构建两个前端主题（方案 A 改前端后用） |
| `bun run install:web` | git pull 后依赖有变时，重新安装前端依赖 |

## 4. 一次性准备（三种方案共用的前提）

```powershell
bun run setup
```

> ⚠️ `web/classic/package.json` 中的 `"date-fns": "2.30.0"` 是**必须保留**的固定版本：Semi UI 依赖的 `date-fns-tz@1.x` 与 workspace 根被 default 主题提升的 `date-fns@4.x` 不兼容，去掉会导致 classic 构建失败。

---

## 5. 方案 A：完整构建运行（发布形态验证）

一个进程一个端口，跑的是嵌入的前端产物。

```powershell
bun run dev
```

访问 `http://localhost:3000`。**没有热更新**：

| 改动 | 生效方式 |
|------|---------|
| 后端 Go 代码 | Ctrl+C → 重新 `bun run dev` |
| 前端代码 | `bun run build:fe` → 重启 `bun run dev`（分钟级） |

**适用**：纯后端开发、验证生产形态、演示。

---

## 6. 方案 B：前后端分离开发（日常推荐，热更新）

两个终端并行，前端源码热更新，请求自动代理到后端。

```powershell
# 终端 1 —— 后端（:3000）
bun run dev

# 终端 2 —— 前端 dev server（:5173，热更新）
bun run dev:fe
```

**浏览器访问 `http://localhost:5173`**（不是 3000）。dev server 将 `/api`、`/mj`、`/pg` 代理到 `:3000`（见 [rsbuild.config.ts](../web/default/rsbuild.config.ts)），与生产行为一致，前端代码无需环境区分。

| 改动内容 | 需要做什么 |
|---------|-----------|
| 前端代码（web/default/src） | 什么都不用做，保存即热更新 |
| 后端代码（Go） | 终端 1 Ctrl+C → 重新 `bun run dev` |
| git pull 后依赖有变 | `bun run install:web` |

可选：开发 classic 主题时，第三个终端运行 `bun run dev:fe:classic`（端口 5174）。

**适用**：前后端同时开发、频繁改码——**团队日常开发标准形态**。

---

## 7. 方案 C：Docker 本地联调

用本地代码构建镜像，容器形态运行，适合验证容器化行为或与 SinoWhaleX（SWX）端到端联调。

```powershell
docker-compose -f docker-compose.dev.yml up -d          # 启动（:3088）
docker-compose -f docker-compose.dev.yml up -d --build  # 改代码后重建
docker-compose -f docker-compose.dev.yml logs -f new-api # 看日志
docker-compose -f docker-compose.dev.yml down            # 停止
```

详细步骤（复用 SWX 已有的 PostgreSQL/Redis 容器、端口规划、故障排查）见 **[方案 C Docker 联调指南](./integration/LOCAL-DEV-DOCKER.md)**。

---

## 8. 三方案速查

| | 方案 A | 方案 B | 方案 C |
|---|---|---|---|
| 进程 | 1（go run） | 2（go run + dev server） | 3 容器 |
| 前端热更新 | ❌ | ✅ :5173 | ❌（需 rebuild 镜像） |
| 后端生效 | 手动重启 | 手动重启 | 镜像重建 |
| 访问端口 | 3000 | 5173 | 3088 |
| 适用 | 发布验证 | **日常开发** | 容器/SWX 联调 |

---

## 9. 常见问题

| 问题 | 处理 |
|------|------|
| `go:embed ... no matching files` | 前端 dist 缺失，回到第 4 节运行一次 `bun run setup` |
| 端口占用 | `go run main.go -port 3001`，或环境变量 `PORT`；dev server 端口固定 5173/5174 |
| 想用 MySQL/PostgreSQL/Redis | 复制 `.env.example` 为 `.env`，设置 `SQL_DSN` / `REDIS_CONN_STRING` 后重启 |
| 重置全部数据 | 停服务后删除根目录 `one-api.db` |
| classic 构建报 date-fns 深度导入错误 | 确认 `web/classic/package.json` 仍固定 `"date-fns": "2.30.0"`，重新 `bun run install:web` |
| 完整环境变量说明 | 见根目录 `.env.example`（含注释） |

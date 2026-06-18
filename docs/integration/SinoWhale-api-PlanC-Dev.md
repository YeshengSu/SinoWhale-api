# SinoWhale-api 方案C 开发文档

> **文档版本**：v1.0.0
> **生效日期**：2026-06-18
> **文档负责人**：SinoWhale-api 平台组
> **目标读者**：SinoWhale-api 后端工程师、QA、SRE
> **关联文档**：《SinoWhaleX 对接 SinoWhale-api 技术文档》

---

## 目录

- [1. 文档导读](#1-文档导读)
- [2. 整体设计概述](#2-整体设计概述)
- [3. 技术栈选型说明](#3-技术栈选型说明)
- [4. 核心功能模块详细设计](#4-核心功能模块详细设计)
  - [4.1 自定义 Header 注入与提取](#41-自定义-header-注入与提取)
  - [4.2 Context 透传链路](#42-context-透传链路)
  - [4.3 日志落库扩展](#43-日志落库扩展)
  - [4.4 查询接口扩展（按 SWX 用户维度过滤）](#44-查询接口扩展按-swx-用户维度过滤)
- [5. 接口定义规范](#5-接口定义规范)
- [6. 数据模型说明](#6-数据模型说明)
- [7. 业务逻辑流程图](#7-业务逻辑流程图)
- [8. 开发环境配置](#8-开发环境配置)
- [9. 编码规范](#9-编码规范)
- [10. 版本控制策略](#10-版本控制策略)
- [11. 单元测试与集成测试](#11-单元测试与集成测试)
- [12. 验收标准](#12-验收标准)
- [13. 功能回归测试清单](#13-功能回归测试清单)
- [14. 索引](#14-索引)
- [15. 术语表](#15-术语表)

---

## 1. 文档导读

### 1.1 背景

SinoWhaleX（业务网站）通过 **服务账号模式** 调用 SinoWhale-api（聚合平台）执行 AI 文本/图像/视频生成。在该模式下，聚合平台仅持有一个服务账号 Token，无法天然区分调用方背后的真实业务用户。

**方案C** 的目标：在 **不破坏聚合平台原有用户体系、不进行用户映射** 的前提下，通过 **自定义 HTTP Header 透传 SWX 用户身份**，使聚合平台日志能够按 SWX 用户维度进行可观测、可对账、可审计。

### 1.2 范围

| 范围 | 说明 |
|------|------|
| ✅ 包含 | Header 提取、Context 透传、日志 `Other` 字段扩展、按 SWX 用户过滤的日志查询接口 |
| ❌ 不包含 | 聚合平台用户与 SWX 用户的双向同步、SWX 侧积分/支付、聚合平台 UI 改造 |

### 1.3 设计约束

1. **侵入性最小**：仅新增字段与中间件钩子，不修改任何已有计费/分发/通道选择逻辑
2. **向前兼容**：未携带自定义 Header 的请求行为与现状完全一致
3. **可关闭**：通过环境变量 `SWX_HEADER_ENABLED` 一键开关
4. **零安全降级**：自定义 Header 为 **附加可观测信息**，不替代任何认证/鉴权字段

---

## 2. 整体设计概述

### 2.1 架构定位

```mermaid
flowchart LR
    A[SWX 前端] -->|JWT| B[SWX Backend<br/>ai-service]
    B -->|sk-xxx + X-SWX-* Headers| C[SinoWhale-api<br/>Gateway:3088]
    C -->|TokenAuth + Header 提取| D[Relay Channel]
    D --> E[(上游 LLM/图像/视频)]
    C -->|RecordConsumeLog<br/>Other 含 swx_user_id| F[(MySQL/Postgres logs 表)]
```

### 2.2 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 用户身份载体 | HTTP Header（`X-SWX-User-Id`/`X-SWX-Trace-Id`/`X-SWX-Biz-Type`） | 与 Token 解耦、运维友好、易调试 |
| 透传载体 | Gin Context Key + RelayInfo | 复用聚合平台既有上下文体系 |
| 落库位置 | `logs.other` JSON 字段 | 零迁移成本、查询可索引 |
| 查询能力 | 扩展现有 `/api/log/` 增加 `swx_user_id` filter | 单一日志查询入口 |
| 回退策略 | Header 缺失 → 不写入、不报错、行为同 B 方案 | 保证降级安全 |

### 2.3 边界声明

- **不参与计费**：自定义 Header 不影响 quota、ratio、pricing 任何计算
- **不参与鉴权**：必须先通过 `TokenAuth()`，未授权请求 Header 不会被信任
- **不参与路由**：不参与 channel 选择与模型映射

---

## 3. 技术栈选型说明

| 层 | 选型 | 版本 | 说明 |
|---|------|------|------|
| 语言 | Go | 1.21+ | 沿用聚合平台原生栈 |
| HTTP 框架 | gin-gonic/gin | v1.x | 沿用 |
| ORM | gorm.io/gorm | v1.25+ | 沿用 |
| 数据库 | MySQL 8 / PostgreSQL 14+ | - | logs 表已存在 |
| 容器 | Docker / docker-compose | - | 端口 3088:3000 已配置 |
| 测试 | testify + httptest | latest | 沿用 |

> **理由**：方案C 是对现有系统的薄增强，引入任何新栈都会增加运维成本。

---

## 4. 核心功能模块详细设计

### 4.1 自定义 Header 注入与提取

#### 4.1.1 Header 规约

| Header 名称 | 类型 | 必填 | 示例 | 说明 |
|------------|------|------|------|------|
| `X-SWX-User-Id` | string | 否 | `user_64f0a1b2c3d4e5` | SWX 用户唯一 ID（建议 ObjectId 字符串） |
| `X-SWX-Trace-Id` | string | 否 | `swx-2026-06-18-abc123` | SWX 侧生成的链路 ID，用于跨平台对账 |
| `X-SWX-Biz-Type` | string | 否 | `text` / `image` / `video` | 业务类型，便于报表分组 |
| `X-SWX-Request-Id` | string | 否 | UUID v4 | SWX 单次请求 ID |

> ⚠️ **注意**：所有 `X-SWX-*` Header 必须经过 SWX 后端**白名单校验后**注入。前端原始请求中携带的同名 Header 必须被 SWX 后端**剥离重写**，防止伪造。

#### 4.1.2 提取位置

提取动作发生在 [`middleware/auth.go`](file:///e:/Code/SinoWhale-api/middleware/auth.go) 的 `TokenAuth()` 中间件 **认证通过之后**，紧邻 `c.Set("id", token.UserId)` 一段。

#### 4.1.3 实现要点

```go
// middleware/auth.go 内 TokenAuth() 末尾、c.Next() 之前追加
extractSWXContext(c)
```

```go
// middleware/swx_header.go (新增文件)
package middleware

import (
    "github.com/QuantumNous/new-api/common"
    "github.com/QuantumNous/new-api/constant"
    "github.com/gin-gonic/gin"
    "os"
    "strings"
)

const (
    HeaderSWXUserId    = "X-SWX-User-Id"
    HeaderSWXTraceId   = "X-SWX-Trace-Id"
    HeaderSWXBizType   = "X-SWX-Biz-Type"
    HeaderSWXRequestId = "X-SWX-Request-Id"

    maxHeaderLen = 128
)

func extractSWXContext(c *gin.Context) {
    if os.Getenv("SWX_HEADER_ENABLED") != "true" {
        return
    }
    if v := safeHeader(c, HeaderSWXUserId); v != "" {
        common.SetContextKey(c, constant.ContextKeySWXUserId, v)
    }
    if v := safeHeader(c, HeaderSWXTraceId); v != "" {
        common.SetContextKey(c, constant.ContextKeySWXTraceId, v)
    }
    if v := safeHeader(c, HeaderSWXBizType); v != "" {
        common.SetContextKey(c, constant.ContextKeySWXBizType, v)
    }
    if v := safeHeader(c, HeaderSWXRequestId); v != "" {
        common.SetContextKey(c, constant.ContextKeySWXRequestId, v)
    }
}

func safeHeader(c *gin.Context, key string) string {
    v := strings.TrimSpace(c.GetHeader(key))
    if len(v) == 0 || len(v) > maxHeaderLen {
        return ""
    }
    // 仅允许：字母、数字、下划线、连字符
    for _, ch := range v {
        if !(ch == '-' || ch == '_' ||
            (ch >= '0' && ch <= '9') ||
            (ch >= 'a' && ch <= 'z') ||
            (ch >= 'A' && ch <= 'Z')) {
            return ""
        }
    }
    return v
}
```

### 4.2 Context 透传链路

#### 4.2.1 Context Key 定义

新增到 [`constant/context_key.go`](file:///e:/Code/SinoWhale-api/constant/context_key.go) 末尾：

```go
/* SinoWhaleX integration keys (Plan C) */
ContextKeySWXUserId    ContextKey = "swx_user_id"
ContextKeySWXTraceId   ContextKey = "swx_trace_id"
ContextKeySWXBizType   ContextKey = "swx_biz_type"
ContextKeySWXRequestId ContextKey = "swx_request_id"
```

#### 4.2.2 透传范围

| 阶段 | 是否透传 | 备注 |
|------|---------|------|
| Auth 中间件 | ✅ 写入 | 入口 |
| Distributor / Channel Select | ✅ 不参与决策，仅保留 | 只读 |
| Relay 实际请求 | ❌ 不向上游 LLM 透传 | 防止泄漏到第三方 |
| 计费/扣费 | ✅ 不参与计算，仅保留 | 只读 |
| 日志落库 | ✅ 注入 `Other` map | 终点 |

### 4.3 日志落库扩展

#### 4.3.1 扩展点

修改 [`service/log_info_generate.go`](file:///e:/Code/SinoWhale-api/service/log_info_generate.go) 中 `GenerateTextOtherInfo`、`GenerateImageOtherInfo`（如有）等所有产出 `other` map 的函数，统一在 **return 前** 调用追加函数：

```go
// service/log_info_generate.go 在每个 Generate*OtherInfo 函数 return 前追加
appendSWXContext(ctx, other)
```

```go
// service/log_info_generate.go 末尾追加
func appendSWXContext(ctx *gin.Context, other map[string]interface{}) {
    if ctx == nil || other == nil {
        return
    }
    if v := common.GetContextKeyString(ctx, constant.ContextKeySWXUserId); v != "" {
        other["swx_user_id"] = v
    }
    if v := common.GetContextKeyString(ctx, constant.ContextKeySWXTraceId); v != "" {
        other["swx_trace_id"] = v
    }
    if v := common.GetContextKeyString(ctx, constant.ContextKeySWXBizType); v != "" {
        other["swx_biz_type"] = v
    }
    if v := common.GetContextKeyString(ctx, constant.ContextKeySWXRequestId); v != "" {
        other["swx_request_id"] = v
    }
}
```

#### 4.3.2 落库结构示例

`logs.other`（JSON 字符串）落库示例：

```json
{
  "model_ratio": 0.002,
  "group_ratio": 1.0,
  "completion_ratio": 1.0,
  "swx_user_id": "user_64f0a1b2c3d4e5",
  "swx_trace_id": "swx-2026-06-18-abc123",
  "swx_biz_type": "image",
  "swx_request_id": "8a1f4e2c-9d6b-4a7e-b3c1-1d9e5f0a8c2d"
}
```

### 4.4 查询接口扩展（按 SWX 用户维度过滤）

#### 4.4.1 扩展位置

[`controller/log.go`](file:///e:/Code/SinoWhale-api/controller/log.go) 中 `GetAllLogs` / `GetUserLogs` 增加查询参数 `swx_user_id`、`swx_trace_id`、`swx_biz_type`。

#### 4.4.2 查询语义

- 数据库为 MySQL 8：使用 `JSON_EXTRACT(other, '$.swx_user_id') = ?`
- 数据库为 PostgreSQL：使用 `other::jsonb ->> 'swx_user_id' = ?`
- 推荐为 `(other->>'swx_user_id')` 建立函数索引以加速

```sql
-- PostgreSQL 索引（仅在 SWX 集成启用后执行）
CREATE INDEX IF NOT EXISTS idx_logs_swx_user_id
  ON logs ((other::jsonb ->> 'swx_user_id'));

-- MySQL 8 虚拟列 + 索引
ALTER TABLE logs
  ADD COLUMN swx_user_id_v VARCHAR(64)
  GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(other, '$.swx_user_id'))) VIRTUAL,
  ADD INDEX idx_logs_swx_user_id (swx_user_id_v);
```

---

## 5. 接口定义规范

### 5.1 改动接口一览

| 接口 | 方法 | 路径 | 改动类型 |
|------|------|------|---------|
| 列出全部日志（管理员） | GET | `/api/log/` | 扩展 query 参数 |
| 列出当前用户日志 | GET | `/api/log/self` | 扩展 query 参数 |
| 任意 v1 推理接口 | POST | `/v1/*` | 透传 Header（无响应变化） |

### 5.2 请求 Header 规范（推理类）

```
POST /v1/chat/completions HTTP/1.1
Host: api.sinowhale.local:3088
Authorization: Bearer sk-sinowhalex-xxxxxxxxxxxx
Content-Type: application/json
X-SWX-User-Id: user_64f0a1b2c3d4e5
X-SWX-Trace-Id: swx-2026-06-18-abc123
X-SWX-Biz-Type: text
X-SWX-Request-Id: 8a1f4e2c-9d6b-4a7e-b3c1-1d9e5f0a8c2d
```

### 5.3 查询接口扩展

#### 5.3.1 GET `/api/log/`

新增 query 参数：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `swx_user_id` | string | 否 | 精确匹配 SWX 用户 ID |
| `swx_trace_id` | string | 否 | 精确匹配 SWX 链路 ID |
| `swx_biz_type` | string | 否 | 枚举：`text` / `image` / `video` |

**响应体**（兼容原有结构，仅增加字段）：

```json
{
  "success": true,
  "message": "",
  "data": {
    "items": [
      {
        "id": 10086,
        "user_id": 1,
        "username": "sinowhalex_service",
        "model_name": "gpt-4o-mini",
        "prompt_tokens": 120,
        "completion_tokens": 350,
        "quota": 940,
        "created_at": 1781771550,
        "other": {
          "swx_user_id": "user_64f0a1b2c3d4e5",
          "swx_trace_id": "swx-2026-06-18-abc123",
          "swx_biz_type": "text"
        }
      }
    ],
    "total": 1
  }
}
```

### 5.4 错误码

| HTTP | code | message | 触发条件 |
|------|------|---------|---------|
| 400 | `swx_header_invalid` | `X-SWX-* header 格式不合法` | Header 含非法字符或长度超限（仅在严格模式下返回；默认静默忽略） |
| 401 | `token_invalid` | `令牌无效` | Token 校验失败（与原有一致） |
| 403 | `swx_disabled` | `SWX 集成未启用` | 仅当对方明确请求查询 SWX 维度但服务端关闭时返回 |

---

## 6. 数据模型说明

### 6.1 logs 表（不修改 DDL，仅扩展 JSON 字段）

| 字段 | 类型 | 改动 | 说明 |
|------|------|------|------|
| `id` | bigint | - | 主键 |
| `user_id` | int | - | 聚合平台用户 ID（即 SWX 服务账号 ID） |
| `model_name` | varchar | - | 模型名 |
| `quota` | int | - | 计费额度（原有） |
| `other` | longtext/jsonb | **扩展键** | 新增：`swx_user_id` / `swx_trace_id` / `swx_biz_type` / `swx_request_id` |
| `created_at` | bigint | - | 时间戳 |

> ⚠️ **必须**保持 DDL 零变更——保证回滚成本可控。索引通过虚拟列/函数索引实现。

### 6.2 配置项（环境变量）

| 变量 | 默认 | 说明 |
|------|------|------|
| `SWX_HEADER_ENABLED` | `false` | 总开关：`true` 启用方案C |
| `SWX_HEADER_STRICT` | `false` | `true` 时非法 Header 返回 400；`false` 静默忽略 |
| `SWX_HEADER_LOG_QUERY_ROLE` | `admin` | 谁可查询 SWX 维度日志：`admin` / `root` / `any` |

---

## 7. 业务逻辑流程图

### 7.1 推理调用主流程（带 SWX Header）

```mermaid
sequenceDiagram
    autonumber
    participant SWX as SWX Backend
    participant GW as SinoWhale-api Gateway
    participant Auth as TokenAuth Middleware
    participant SWXMW as extractSWXContext
    participant Relay as Relay/Channel
    participant LLM as 上游 LLM
    participant DB as logs 表

    SWX->>GW: POST /v1/chat/completions<br/>Authorization: sk-xxx<br/>X-SWX-User-Id / Trace-Id / Biz-Type
    GW->>Auth: 校验 sk-xxx
    Auth-->>GW: token, userId(=service_account_id)
    GW->>SWXMW: extractSWXContext(c)
    SWXMW->>SWXMW: 校验 Header 字符 / 长度
    SWXMW-->>GW: 写入 ContextKeySWX*
    GW->>Relay: 选 channel + 转发上游
    Relay->>LLM: 不携带 X-SWX-* Header
    LLM-->>Relay: 推理响应
    Relay-->>SWX: 流式/非流式响应
    Relay->>DB: RecordConsumeLog(other:{swx_user_id, swx_trace_id, ...})
```

### 7.2 异常分支

```mermaid
flowchart TD
    A[请求到达] --> B{SWX_HEADER_ENABLED?}
    B -- false --> C[忽略 X-SWX-*<br/>退化为 B 方案]
    B -- true --> D{Header 格式合法?}
    D -- 是 --> E[写入 Context]
    D -- 否 --> F{SWX_HEADER_STRICT?}
    F -- true --> G[400 swx_header_invalid]
    F -- false --> H[静默忽略<br/>记 SysLog warn]
    E --> I[继续 c.Next]
    H --> I
    C --> I
```

---

## 8. 开发环境配置

### 8.1 本地启动

```powershell
# 1. 拉取依赖
go mod tidy

# 2. 设置 SWX 集成环境变量（PowerShell）
$env:SWX_HEADER_ENABLED = "true"
$env:SWX_HEADER_STRICT = "false"
$env:PORT = "3000"

# 3. 启动
go run main.go
```

### 8.2 Docker 启动

参考已配置的 [`docker-compose.yml`](file:///e:/Code/SinoWhale-api/docker-compose.yml)（端口 3088:3000）：

```powershell
docker-compose up -d
```

并在 `.env` 中追加：

```env
SWX_HEADER_ENABLED=true
SWX_HEADER_STRICT=false
SWX_HEADER_LOG_QUERY_ROLE=admin
```

### 8.3 调试验证

```powershell
curl -X POST http://localhost:3088/v1/chat/completions `
  -H "Authorization: Bearer sk-sinowhalex-xxxxxxxx" `
  -H "X-SWX-User-Id: user_test001" `
  -H "X-SWX-Trace-Id: swx-test-001" `
  -H "X-SWX-Biz-Type: text" `
  -H "Content-Type: application/json" `
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

随后到聚合平台后台 `/log` 页面或调用 `/api/log/?swx_user_id=user_test001` 验证落库。

---

## 9. 编码规范

### 9.1 通用规范

- 严格遵守 `.trae/rules/karpathy-guidelines.md`：精准修改、简洁优先
- 使用 `gofmt -s` 与 `golangci-lint`，零 warning 才能合入
- 包级新增标识符必须有 godoc 注释（中文/英文均可）

### 9.2 本方案专属规范

| 规范 | 说明 |
|------|------|
| 严禁修改 `RecordConsumeLog` 入参签名 | 通过 `Other` map 传递，避免上层级联改造 |
| 严禁在 Relay 转发到 LLM 时携带 `X-SWX-*` Header | 防止泄漏到第三方 |
| 严禁基于 `X-SWX-*` 做任何鉴权/路由决策 | 它只是观测信号 |
| 严禁记录 SWX 用户的明文邮箱/手机号 | 仅允许 ID 类标识 |

### 9.3 命名

- Context Key 常量统一以 `ContextKeySWX` 为前缀
- Header 名以 `X-SWX-` 开头
- 日志 `other` JSON 键以 `swx_` 开头

---

## 10. 版本控制策略

### 10.1 分支模型

```
main                ← 受保护，只接受 PR
└── feature/swx-plan-c-header       ← 本方案开发分支
    ├── chore/swx-context-keys      ← 子任务分支（可选）
    ├── feat/swx-middleware
    ├── feat/swx-log-other
    └── feat/swx-log-query-filter
```

### 10.2 提交规范

遵循 Conventional Commits：

```
feat(middleware): add extractSWXContext for Plan C
feat(service/log): inject swx_* keys into Other map
feat(controller/log): support swx_user_id filter in GetAllLogs
test(middleware): cover swx header validation edge cases
docs(integration): add SinoWhale-api Plan C dev doc
```

### 10.3 灰度策略

1. **阶段 1**：合入 main，但 `SWX_HEADER_ENABLED=false`，验证零回归
2. **阶段 2**：预发环境开启 `true`，SWX 联调
3. **阶段 3**：生产开启 `SWX_HEADER_ENABLED=true`，严格模式仍为 `false`
4. **阶段 4**：建立索引、查询验证后切 `SWX_HEADER_STRICT=true`

---

## 11. 单元测试与集成测试

### 11.1 单元测试

| 用例 ID | 用例 | 文件 | 关注点 |
|--------|------|------|--------|
| UT-01 | `safeHeader` 接受合法字符 | `middleware/swx_header_test.go` | 边界字符 |
| UT-02 | `safeHeader` 拒绝非法字符 | 同上 | `<`,`>`,空格,注入 |
| UT-03 | `safeHeader` 拒绝超长 | 同上 | 129 字节 |
| UT-04 | `extractSWXContext` 开关关闭时 noop | 同上 | env=false |
| UT-05 | `extractSWXContext` 全部 4 个 Header 注入 Context | 同上 | 正常路径 |
| UT-06 | `appendSWXContext` 部分键缺失时不写空值 | `service/log_info_generate_test.go` | 仅写入有值 |

#### 示例：UT-05

```go
func TestExtractSWXContext_AllHeaders(t *testing.T) {
    t.Setenv("SWX_HEADER_ENABLED", "true")
    w := httptest.NewRecorder()
    c, _ := gin.CreateTestContext(w)
    c.Request = httptest.NewRequest("POST", "/", nil)
    c.Request.Header.Set("X-SWX-User-Id", "user_001")
    c.Request.Header.Set("X-SWX-Trace-Id", "trace-abc")
    c.Request.Header.Set("X-SWX-Biz-Type", "image")
    c.Request.Header.Set("X-SWX-Request-Id", "req-1")

    extractSWXContext(c)

    assert.Equal(t, "user_001",
        common.GetContextKeyString(c, constant.ContextKeySWXUserId))
    assert.Equal(t, "trace-abc",
        common.GetContextKeyString(c, constant.ContextKeySWXTraceId))
    assert.Equal(t, "image",
        common.GetContextKeyString(c, constant.ContextKeySWXBizType))
    assert.Equal(t, "req-1",
        common.GetContextKeyString(c, constant.ContextKeySWXRequestId))
}
```

### 11.2 集成测试

| 用例 ID | 场景 | 验证 |
|--------|------|------|
| IT-01 | 端到端：携带全部 Header 调用 `/v1/chat/completions` | logs.other 含 4 个 swx_* 键 |
| IT-02 | 端到端：不携带 Header | logs.other 不含 swx_* 键，请求成功 |
| IT-03 | 端到端：开关关闭 | 携带 Header 也不写入 |
| IT-04 | 查询：`/api/log/?swx_user_id=user_001` | 仅返回该 SWX 用户的记录 |
| IT-05 | 查询：`/api/log/?swx_trace_id=trace-abc` | 命中单条 |
| IT-06 | 安全：携带 `<script>` 注入 Header | 静默忽略 + SysLog 一条 warn |

### 11.3 性能基准

| 指标 | 期望 | 测量方法 |
|------|------|---------|
| 中间件单次开销 | < 10 µs | `go test -bench` `BenchmarkExtractSWX` |
| QPS 退化 | < 1% | `wrk` 对比开关 on/off |
| 日志查询 P95 | < 200ms（百万级 logs） | 索引建立后压测 |

---

## 12. 验收标准

### 12.1 功能验收（必须 100% 通过）

- [ ] FA-1：所有 UT/IT 用例通过
- [ ] FA-2：开关 `SWX_HEADER_ENABLED=false` 时，端到端无任何行为差异
- [ ] FA-3：开关 `=true` 时，logs.other 正确落入 4 个 swx_* 键
- [ ] FA-4：`/api/log/?swx_user_id=` 能精确过滤
- [ ] FA-5：响应体结构对老客户端 100% 兼容（diff = 0）

### 12.2 非功能验收

- [ ] NFA-1：性能基准达标（见 11.3）
- [ ] NFA-2：`golangci-lint` 0 错误
- [ ] NFA-3：覆盖率 ≥ 80%（新增代码行）
- [ ] NFA-4：CI（pr-check.yml）全绿

### 12.3 安全验收

- [ ] SA-1：注入字符（`<`, `>`, `;`, `'`, 空格）被拒绝
- [ ] SA-2：上游 LLM 请求体不含任何 `X-SWX-*` Header
- [ ] SA-3：未通过 TokenAuth 的请求即使携带 Header 也不会落库

---

## 13. 功能回归测试清单

> 严格按 [`dev-conventions.md`](file:///e:/Code/SinoWhale-api/.trae/rules/dev-conventions.md) 第 2 节要求拆分维度。

### 13.1 数据模型层

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.1.1 | 老 logs 行（无 swx_*）反序列化 | 升级前的旧记录 | API 正常返回，other 中无 swx_* 键 | P0 |
| 13.1.2 | 新写入 logs.other 含 4 个 swx_* | 开启开关、正常调用 | DB 中 JSON 含全部键 | P0 |
| 13.1.3 | logs.other JSON 长度过大（> 16KB） | 异常 trace_id 长度仍受限 128 | 不出现 row size 异常 | P1 |

### 13.2 核心业务逻辑

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.2.1 | 计费逻辑不受 Header 影响 | 同模型同 prompt 在 on/off 下 | quota 完全一致 | P0 |
| 13.2.2 | 通道选择不受 Header 影响 | 多 channel 配置 | 命中相同 channel | P0 |
| 13.2.3 | 流式响应正常 | SSE 场景 | 与 B 方案完全一致 | P0 |
| 13.2.4 | 并发 100 路同时携带不同 user_id | 压测 | 落库 user_id 一一对应 | P0 |

### 13.3 API 路由层

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.3.1 | `/v1/chat/completions` 携带 Header | 合法 token | 200，other 含 swx_* | P0 |
| 13.3.2 | `/v1/images/generations` 携带 Header | 合法 token | 200，other 含 swx_* + biz_type=image | P0 |
| 13.3.3 | `/v1/video/...`（如启用）携带 Header | 合法 token | 200，other 含 swx_* | P1 |
| 13.3.4 | `/api/log/?swx_user_id=xxx` 管理员调用 | admin 登录 | 精确过滤 | P0 |
| 13.3.5 | `/api/log/?swx_user_id=xxx` 普通用户 | common user | 403（按 `SWX_HEADER_LOG_QUERY_ROLE` 配置） | P1 |

### 13.4 代理/中间层

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.4.1 | TokenAuth 失败 | sk 无效 | 401，不进入 extractSWXContext | P0 |
| 13.4.2 | 上游 LLM 抓包 | 任意请求 | 不含 X-SWX-* | P0 |
| 13.4.3 | 多重中间件顺序：CORS→TokenAuth→SWX→Distributor | 正常请求 | Context 在 Distributor 中可读 | P0 |

### 13.5 安全与权限

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.5.1 | 伪造 Header（无 token） | 仅带 X-SWX-* | 401 | P0 |
| 13.5.2 | 注入 SQL 字符 `' OR 1=1 --` | 携带于 user_id | safeHeader 拒绝 | P0 |
| 13.5.3 | 超长 Header（10MB） | 通过 nginx limit | nginx 拦截 + safeHeader 兜底 | P1 |

### 13.6 现有功能无回归

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---------|---------|---------|--------|
| 13.6.1 | 关闭开关后所有 e2e 用例通过 | `SWX_HEADER_ENABLED=false` | 与升级前 100% 一致 | P0 |
| 13.6.2 | 升级时不需 schema migration | 仅部署新二进制 | DB 版本不变 | P0 |
| 13.6.3 | 关闭后回滚 | 切回上一个 tag | 数据/接口无破坏 | P0 |

---

## 14. 索引

| 关键词 | 章节 |
|-------|------|
| Header 规约 | [4.1.1](#411-header-规约) |
| Context Key | [4.2.1](#421-context-key-定义) |
| 日志 Other 字段 | [4.3](#43-日志落库扩展) |
| 查询过滤 | [4.4](#44-查询接口扩展按-swx-用户维度过滤) |
| 环境变量 | [6.2](#62-配置项环境变量) |
| 流程图 | [7](#7-业务逻辑流程图) |
| 单元测试用例 | [11.1](#111-单元测试) |
| 验收标准 | [12](#12-验收标准) |
| 回归清单 | [13](#13-功能回归测试清单) |

---

## 15. 术语表

| 术语 | 全称/英文 | 含义 |
|------|----------|------|
| SWX | SinoWhaleX | 业务网站名称 |
| SinoWhale-api | new-api fork | 聚合平台（多模型路由 + 计费） |
| 服务账号 | Service Account | SWX 后端持有的单一聚合平台账号 |
| 方案 C | Plan C | 通过自定义 Header 透传 SWX 用户身份 |
| Context Key | Gin Context Key | gin 请求生命周期内的键值存储 |
| Other 字段 | logs.other | logs 表中存储扩展信息的 JSON 列 |
| Trace ID | swx_trace_id | SWX 侧生成、跨平台对账锚点 |
| Biz Type | swx_biz_type | 业务类型枚举：text/image/video |
| 严格模式 | Strict Mode | 非法 Header 直接返回 400 的模式 |
| 灰度 | Canary Rollout | 按环境/百分比逐步开启的发布策略 |

---

**END OF DOCUMENT**

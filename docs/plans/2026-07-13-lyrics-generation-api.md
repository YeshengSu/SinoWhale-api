# SWAPI 歌词生成接口开发文档

> 文档版本：1.0  
> 创建日期：2026-07-13  
> 关联第三方 API：[MiniMax Lyrics Generation](https://platform.minimaxi.com/docs/api-reference/lyrics-generation)

---

## 目录

1. [功能概述](#1-功能概述)
2. [接口设计规范](#2-接口设计规范)
3. [与第三方歌词生成 API 的集成方案](#3-与第三方歌词生成-api-的集成方案)
4. [用户输入内容的处理逻辑](#4-用户输入内容的处理逻辑)
5. [错误处理机制和状态码定义](#5-错误处理机制和状态码定义)
6. [性能优化策略](#6-性能优化策略)
7. [部署和测试指南](#7-部署和测试指南)
8. [附录：关键源码索引](#8-附录关键源码索引)

---

## 1. 功能概述

### 1.1 功能定位

SWAPI（SinoWhale-API）作为 AI 聚合网关，提供统一的 `POST /v1/lyrics_generation` 端点，将下游（SWX ai-service）的歌词生成请求中继到 MiniMax 第三方 API，并完成认证、计费、错误处理等横切逻辑。

### 1.2 架构位置

```
SWX Frontend (music-creation page)
  ↓ POST /api/ai/music/lyrics
SWX ai-service (lyricsGenerator.ts)
  ↓ POST /v1/lyrics_generation
SWAPI (relay-router.go → AudioHelper → minimax.Adaptor)
  ↓ POST https://api.minimaxi.com/v1/lyrics_generation
MiniMax 第三方 API
```

### 1.3 已实现状态

该接口已在 SWAPI 中完整实现，包含：

| 模块 | 文件 | 状态 |
|------|------|------|
| 路由注册 | `router/relay-router.go` | 已实现 |
| RelayMode 常量 | `relay/constant/relay_mode.go` | 已实现 |
| 路径映射 | `relay/constant/relay_mode.go` Path2RelayMode | 已实现 |
| 控制器分发 | `controller/relay.go` relayHandler | 已实现 |
| 请求转换 | `relay/channel/minimax/music.go` audioRequest2MiniMaxLyricsRequest | 已实现 |
| 响应处理 | `relay/channel/minimax/music.go` handleLyricsResponse | 已实现 |
| URL 构建 | `relay/channel/minimax/relay-minimax.go` GetRequestURL | 已实现 |
| 适配器分发 | `relay/channel/minimax/adaptor.go` DoResponse | 已实现 |
| 模型列表 | `relay/channel/minimax/constants.go` | 已实现 |

---

## 2. 接口设计规范

### 2.1 请求方法与 URL

| 属性 | 值 |
|------|-----|
| 方法 | `POST` |
| 路径 | `/v1/lyrics_generation` |
| 认证 | `Authorization: Bearer <SWAPI_TOKEN>` |
| Content-Type | `application/json` |
| RelayFormat | `RelayFormatOpenAIAudio` |

### 2.2 请求参数

SWAPI 端点接收 `dto.AudioRequest` 结构体，其中歌词生成使用以下字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | `string` | 是 | 模型标识，固定为 `minimax-lyrics`（SWAPI 内部路由用，非 MiniMax 原生模型名） |
| `prompt` | `string` | 否 | 歌词主题与风格描述。为空时 MiniMax 随机生成一首歌曲。最大 2000 字符 |
| `input` | `string` | 否 | 兼容字段，当 `prompt` 为空时回退使用 `input` 值 |
| `metadata` | `json.RawMessage` | 否 | JSON 扩展字段，用于传递 `mode` 和 `lyrics` 参数 |

#### metadata 字段说明

`metadata` 是一个 JSON 原始字节，会被 overlay 到 `MiniMaxLyricsRequest` 结构体上：

```json
{
  "mode": "write_full_song",
  "lyrics": "[Verse 1]\n已有歌词内容..."
}
```

| metadata 字段 | 类型 | 默认值 | 说明 |
|---------------|------|--------|------|
| `mode` | `string` | `"write_full_song"` | 生成模式：`write_full_song`（创作完整歌曲）、`edit`（编辑/续写已有歌词） |
| `lyrics` | `string` | - | 已有歌词内容，仅 `edit` 模式有效。最大 3500 字符 |
| `title` | `string` | - | 歌曲标题。提供时输出标题保持不变 |

### 2.3 请求示例

#### 2.3.1 生成完整歌词（write_full_song 模式）

```bash
curl -X POST https://<swapi-host>/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": "一首关于夏日海边的轻快情歌，流行曲风，女声演唱"
  }'
```

#### 2.3.2 编辑/续写已有歌词（edit 模式）

```bash
curl -X POST https://<swapi-host>/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": "在副歌部分增加更多关于海洋的意象",
    "metadata": {
      "mode": "edit",
      "lyrics": "[Verse 1]\n海风轻轻吹拂你发梢\n...\n[Chorus]\n夏日的海边，我们的约定",
      "title": "夏日海风的约定"
    }
  }'
```

### 2.4 响应格式

#### 2.4.1 成功响应（HTTP 200）

SWAPI 透传 MiniMax 原始响应：

```json
{
  "data": {
    "lyrics": "[Intro]\n(Ooh-ooh-ooh)\n...\n[Verse 1]\n海风轻轻吹拂你发梢\n...",
    "title": "夏日海风的约定"
  },
  "trace_id": "023a7c494d764ec4880e1f9a2c2c5b55",
  "base_resp": {
    "status_code": 0,
    "status_msg": "success"
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `data.lyrics` | `string` | 生成的歌词，包含结构标签（`[Intro]`、`[Verse]`、`[Chorus]` 等 14 种） |
| `data.title` | `string` | 生成的歌曲标题 |
| `trace_id` | `string` | MiniMax 追踪 ID，用于问题排查 |
| `base_resp.status_code` | `int` | 状态码，`0` 表示成功 |
| `base_resp.status_msg` | `string` | 状态信息 |

#### 2.4.2 歌词结构标签

生成的 `lyrics` 字段支持以下 14 种结构标签：

```
[Intro] [Verse] [Pre-Chorus] [Chorus] [Hook] [Drop] [Bridge] [Solo]
[Build-up] [Instrumental] [Breakdown] [Break] [Interlude] [Outro]
```

这些标签可直接用于后续 `POST /v1/music_generation` 的 `lyrics` 参数。

---

## 3. 与第三方歌词生成 API 的集成方案

### 3.1 MiniMax API 规格

| 属性 | 值 |
|------|-----|
| 端点 | `POST https://api.minimaxi.com/v1/lyrics_generation`（国内）|
| 端点 | `POST https://api.minimax.io/v1/lyrics_generation`（国际）|
| 认证 | `Authorization: Bearer <MINIMAX_API_KEY>` |
| 计费 | 约 $0.01 / 次 |

### 3.2 适配器模式

SWAPI 使用 Adaptor（同步 Relay）模式集成 MiniMax 歌词生成：

```
请求流转：
  HTTP Request
    → Gin Router (relay-router.go)
    → Middleware Chain (CORS → TokenAuth → Distribute → ...)
    → controller.Relay(c, RelayFormatOpenAIAudio)
    → relayHandler → relay.AudioHelper
    → minimax.Adaptor.ConvertAudioRequest  (请求转换)
    → minimax.Adaptor.DoRequest            (发送 HTTP 请求)
    → minimax.Adaptor.DoResponse           (响应处理)
    → PostTextConsumeQuota                 (计费结算)
    → HTTP Response
```

### 3.3 请求转换

文件：`relay/channel/minimax/music.go`，函数 `audioRequest2MiniMaxLyricsRequest`

```go
// 将 SWAPI AudioRequest 转换为 MiniMax 歌词生成请求
func audioRequest2MiniMaxLyricsRequest(request dto.AudioRequest) MiniMaxLyricsRequest {
    minimaxReq := MiniMaxLyricsRequest{
        Prompt: request.Prompt,
    }

    // 从 metadata 中提取 mode、lyrics 等扩展字段
    if len(request.Metadata) > 0 {
        _ = json.Unmarshal(request.Metadata, &minimaxReq)
    }

    // 兼容：允许用 input 字段传 prompt
    if minimaxReq.Prompt == "" && request.Input != "" {
        minimaxReq.Prompt = request.Input
    }

    // 默认 mode
    if minimaxReq.Mode == "" {
        minimaxReq.Mode = "write_full_song"
    }

    return minimaxReq
}
```

转换逻辑说明：
1. 从 `request.Prompt` 提取主题描述
2. 通过 `json.Unmarshal` 将 `metadata` overlay 到请求结构体（提取 `mode`、`lyrics`、`title`）
3. 兼容回退：`prompt` 为空时使用 `input` 字段
4. 默认 `mode` 为 `write_full_song`

### 3.4 响应处理

文件：`relay/channel/minimax/music.go`，函数 `handleLyricsResponse`

```go
func handleLyricsResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
    // 1. 读取响应体
    body, readErr := io.ReadAll(resp.Body)
    
    // 2. 解析为 MiniMaxLyricsResponse
    var minimaxResp MiniMaxLyricsResponse
    json.Unmarshal(body, &minimaxResp)
    
    // 3. 检查 base_resp 状态码
    if minimaxResp.BaseResp.StatusCode != 0 {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("minimax lyrics error: %d - %s",
                minimaxResp.BaseResp.StatusCode,
                minimaxResp.BaseResp.StatusMsg),
            types.ErrorCodeBadResponse,
            http.StatusBadRequest,
        )
    }
    
    // 4. 透传 MiniMax 响应
    c.JSON(http.StatusOK, minimaxResp)
    
    // 5. 按次计费（token 设为 0）
    usage = &dto.Usage{
        PromptTokens:     0,
        CompletionTokens: 0,
        TotalTokens:      0,
    }
    return usage, nil
}
```

### 3.5 URL 构建

文件：`relay/channel/minimax/relay-minimax.go`

```go
case constant.RelayModeLyricsGeneration:
    return fmt.Sprintf("%s/v1/lyrics_generation", baseUrl), nil
```

`baseUrl` 来源于渠道配置（`info.ChannelBaseUrl`），若为空则回退到 `ChannelTypeMiniMax` 的默认 BaseURL。

### 3.6 适配器分发

文件：`relay/channel/minimax/adaptor.go`

```go
// ConvertAudioRequest - 根据 RelayMode 分发到对应转换函数
func (a *Adaptor) ConvertAudioRequest(...) {
    switch info.RelayMode {
    case constant.RelayModeLyricsGeneration:
        minimaxRequest := audioRequest2MiniMaxLyricsRequest(request)
        // ...
    }
}

// DoResponse - 根据 RelayMode 分发到对应响应处理
func (a *Adaptor) DoResponse(...) {
    if info.RelayMode == constant.RelayModeLyricsGeneration {
        return handleLyricsResponse(c, resp, info)
    }
}
```

### 3.7 计费机制

歌词生成采用**按次计费**模式：

- `usage.PromptTokens = 0`
- `usage.CompletionTokens = 0`
- `usage.TotalTokens = 0`
- 实际计费通过 `PostTextConsumeQuota` 按模型配置的倍率扣除

模型 `minimax-lyrics` 的计费倍率在管理后台的模型定价配置中设置。

---

## 4. 用户输入内容的处理逻辑

### 4.1 输入字段映射

SWAPI 端点接收的 `dto.AudioRequest` 中，歌词生成相关字段如下：

| SWAPI 字段 | MiniMax 字段 | 来源 | 说明 |
|-----------|-------------|------|------|
| `prompt` | `prompt` | 用户输入 | 歌词主题与风格描述 |
| `input` | `prompt`（回退） | 用户输入 | 当 prompt 为空时使用 |
| `metadata.mode` | `mode` | 系统设定 | `write_full_song` 或 `edit` |
| `metadata.lyrics` | `lyrics` | 用户输入 | 已有歌词（edit 模式） |
| `metadata.title` | `title` | 用户输入 | 歌曲标题 |

### 4.2 Prompt 智能整合策略

SWAPI 层面不修改 prompt 内容，**prompt 的智能整合在 SWX ai-service 层完成**。SWAPI 负责透传 prompt 到 MiniMax。

上游（SWX ai-service）应按以下策略构建 prompt：

```
prompt = f(主题, 风格)
```

示例整合规则：

| 场景 | 主题输入 | 风格输入 | 整合后 prompt |
|------|---------|---------|--------------|
| 主题+风格 | "夏日海边的回忆" | "流行, 轻快, 女声" | "一首关于夏日海边的回忆的流行歌曲，风格轻快，女声演唱" |
| 仅主题 | "离别与思念" | - | "一首关于离别与思念的歌曲" |
| 仅风格 | - | "摇滚, 热血, 男声" | "一首摇滚风格的热血歌曲，男声演唱" |
| 都为空 | - | - | "" (MiniMax 随机生成) |

### 4.3 Mode 选择逻辑

| 条件 | mode | 说明 |
|------|------|------|
| 有已有歌词 + 有 prompt | `edit` | 在已有歌词基础上根据 prompt 编辑/续写 |
| 有已有歌词 + 无 prompt | `edit` | 仅优化已有歌词 |
| 无已有歌词 + 有/无 prompt | `write_full_song` | 从零创作完整歌词 |

该逻辑在 SWX ai-service 的 `lyricsGenerator.ts` 中实现，SWAPI 侧通过 `metadata.mode` 接收。

---

## 5. 错误处理机制和状态码定义

### 5.1 HTTP 状态码

| HTTP Status | 含义 | 触发条件 |
|------------|------|---------|
| 200 | 成功 | MiniMax 返回 `base_resp.status_code = 0` |
| 400 | 请求错误 | MiniMax 返回非 0 的 `base_resp.status_code`，或响应解析失败 |
| 401 | 未授权 | SWAPI Token 无效或过期 |
| 403 | 禁止访问 | 权限不足或 IP 被限制 |
| 429 | 请求频率超限 | 触发模型级或用户级限流 |
| 500 | 服务器错误 | 读取响应体失败或内部异常 |
| 502 | 网关错误 | MiniMax 不可达或返回非 200 |

### 5.2 MiniMax 业务错误码

当 HTTP 200 但 `base_resp.status_code != 0` 时，SWAPI 返回 HTTP 400 并携带错误信息：

| 错误码 | 含义 | 解决方法 |
|--------|------|---------|
| 1000 | 未知错误 | 稍后再试 |
| 1001 | 请求超时 | 稍后再试 |
| 1002 | 请求频率超限 | 稍后再试，建议指数退避重试 |
| 1004 | 未授权/Token 不匹配 | 检查 MiniMax API Key |
| 1008 | 余额不足 | 检查 MiniMax 账户余额 |
| 1024 | 内部错误 | 稍后再试 |
| 1026 | 输入内容涉敏 | 调整输入内容 |
| 1027 | 输出内容涉敏 | 调整输入内容 |
| 1033 | 系统错误 | 稍后再试 |
| 1039 | Token 限制 | 调整 max_tokens |
| 1041 | 连接数限制 | 联系平台 |
| 1042 | 非法字符超限 | 检查输入内容 |
| 2013 | 参数错误 | 检查请求参数 |
| 2049 | 无效的 API Key | 检查 API Key 格式 |
| 2056 | 超出 Plan 限制 | 等待下一个时间段 |

### 5.3 SWAPI 内部错误处理链

```
MiniMax HTTP 非 200
  → service.RelayErrorHandler
    → 解析响应体为通用错误格式
    → 根据 Content-Type 提取 OpenAI/Claude/Gemini 格式错误
    → 返回 *types.NewAPIError

MiniMax HTTP 200 但 base_resp.status_code != 0
  → handleLyricsResponse 返回 NewErrorWithStatusCode
    → ErrorCodeBadResponse, HTTP 400

控制器 defer 块
  → controller/relay.go
    → newAPIError.ToOpenAIError() 转换为 OpenAI 错误格式
    → relayInfo.Billing.Refund(c) 退还预扣费
```

### 5.4 错误响应格式

SWAPI 统一使用 OpenAI 兼容的错误格式：

```json
{
  "error": {
    "message": "minimax lyrics error: 1026 - input content sensitive",
    "type": "new_api_error",
    "code": "bad_response",
    "param": null
  }
}
```

### 5.5 计费回退

歌词生成请求失败时，预扣的积分将全额退还：

1. 请求前：`PreConsumeQuota` 预扣积分
2. 请求成功：`PostTextConsumeQuota` 结算实际积分
3. 请求失败：`relayInfo.Billing.Refund(c)` 全额退还

---

## 6. 性能优化策略

### 6.1 HTTP 客户端优化

SWAPI 使用全局共享的 HTTP 客户端（`service/http_client.go`），支持以下优化：

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `RelayMaxIdleConns` | 全局最大空闲连接数 | 环境变量配置 |
| `RelayMaxIdleConnsPerHost` | 单 Host 最大空闲连接数 | 环境变量配置 |
| `RelayIdleConnTimeout` | 空闲连接超时 | 环境变量配置 |
| `RelayTimeout` | 请求超时（秒，0 为不限制） | 环境变量配置 |

### 6.2 超时控制

歌词生成为同步请求，超时由以下层级控制：

| 层级 | 超时配置 | 说明 |
|------|---------|------|
| SWAPI 全局 | `RELAY_TIMEOUT` 环境变量 | 所有 relay 请求的统一超时 |
| HTTP 客户端 | `http.Client.Timeout` | 底层 HTTP 请求超时 |
| MiniMax 侧 | 服务端超时 | MiniMax 内部处理超时 |

建议 `RELAY_TIMEOUT` 设置为至少 30 秒，歌词生成通常在 5-15 秒内完成。

### 6.3 连接复用

- 全局 HTTP 客户端复用 TCP 连接，避免重复握手
- 空闲连接池保持与 MiniMax API 的持久连接
- 支持 HTTP/2 多路复用

### 6.4 限流保护

| 限流层级 | 中间件 | 说明 |
|---------|--------|------|
| 模型级 | `middleware.ModelRequestRateLimit` | 按模型维度限流 |
| 用户级 | Token 维度限流 | 按 API Key 维度限流 |
| 系统级 | `middleware.SystemPerformanceCheck` | 系统过载时拒绝新请求 |

### 6.5 内存缓存

- `MEMORY_CACHE_ENABLED=true` 时启用模型能力、渠道配置等内存缓存
- 减少 DB 查询开销，加速请求分发

### 6.6 请求体大小限制

通过 `middleware.RequestBodyLimit` 限制请求体大小，防止超大 prompt 导致内存溢出。歌词生成的 prompt 最大 2000 字符，远在限制范围内。

---

## 7. 部署和测试指南

### 7.1 环境配置

#### 7.1.1 必需的环境变量

```env
# MiniMax 渠道配置
# 在管理后台添加 MiniMax 渠道，填入 API Key
# 渠道 Base URL: https://api.minimaxi.com (国内) 或 https://api.minimax.io (国际)

# SWAPI 运行配置
PORT=3088
RELAY_TIMEOUT=60
MEMORY_CACHE_ENABLED=true
```

#### 7.1.2 渠道配置

1. 登录 SWAPI 管理后台
2. 进入「渠道管理」
3. 添加新渠道：
   - 类型：MiniMax
   - Base URL：`https://api.minimaxi.com`（国内）或 `https://api.minimax.io`（国际）
   - API Key：填入 MiniMax 平台获取的 API Key
4. 配置模型列表，确保包含 `minimax-lyrics`
5. 配置模型倍率（建议按 $0.01/次 设置）

#### 7.1.3 模型定价配置

在管理后台的模型定价中为 `minimax-lyrics` 设置：
- 计费方式：按次计费
- 单次价格：根据 MiniMax 计费（$0.01/次）换算为积分

### 7.2 测试指南

#### 7.2.1 单元测试

测试请求转换函数：

```bash
cd e:\Code\SinoWhale-api
go test ./relay/channel/minimax/ -run TestAudioRequest2MiniMaxLyrics -v
```

测试响应处理函数：

```bash
go test ./relay/channel/minimax/ -run TestHandleLyricsResponse -v
```

#### 7.2.2 集成测试

使用 cURL 测试完整链路：

```bash
# 测试 write_full_song 模式
curl -X POST http://localhost:3088/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": "一首关于春天花开的民谣"
  }'

# 测试 edit 模式
curl -X POST http://localhost:3088/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": "增加副歌的重复段",
    "metadata": {
      "mode": "edit",
      "lyrics": "[Verse 1]\n春风吹过花瓣落\n[Chorus]\n花开花落又一年"
    }
  }'

# 测试空 prompt（随机生成）
curl -X POST http://localhost:3088/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": ""
  }'
```

#### 7.2.3 错误场景测试

```bash
# 测试无效 Token
curl -X POST http://localhost:3088/v1/lyrics_generation \
  -H "Authorization: Bearer invalid_token" \
  -H "Content-Type: application/json" \
  -d '{"model": "minimax-lyrics", "prompt": "test"}'
# 预期：401 Unauthorized

# 测试超长 prompt（>2000 字符）
curl -X POST http://localhost:3088/v1/lyrics_generation \
  -H "Authorization: Bearer <SWAPI_TOKEN>" \
  -H "Content-Type: application/json" \
  -d "{\"model\": \"minimax-lyrics\", \"prompt\": \"$(python -c 'print("a"*2001)')\"}"
# 预期：400 或 MiniMax 返回参数错误
```

#### 7.2.4 性能测试

```bash
# 使用 wrk 进行压测
wrk -t4 -c10 -d30s -s lyrics_bench.lua http://localhost:3088/v1/lyrics_generation
```

建议指标：
- P50 延迟 < 8 秒
- P99 延迟 < 20 秒
- 错误率 < 1%

### 7.3 部署步骤

1. **构建**：
   ```bash
   cd e:\Code\SinoWhale-api
   go build -o new-api .
   ```

2. **Docker 部署**：
   ```bash
   docker-compose -f docker-compose.dev.yml up -d
   ```

3. **验证**：
   ```bash
   # 检查服务健康
   curl http://localhost:3088/api/status
   
   # 检查模型可用性
   curl http://localhost:3088/v1/models \
     -H "Authorization: Bearer <SWAPI_TOKEN>"
   ```

### 7.4 监控指标

| 指标 | 说明 | 告警阈值 |
|------|------|---------|
| 请求成功率 | 200 响应占比 | < 95% |
| 平均延迟 | 端到端延迟 | > 15s |
| MiniMax 错误率 | base_resp 非 0 占比 | > 5% |
| 余额不足错误 | 1008 错误次数 | > 0 |

---

## 8. 附录：关键源码索引

### 8.1 文件清单

| 文件路径 | 职责 |
|---------|------|
| `router/relay-router.go:139-141` | 路由注册 `POST /v1/lyrics_generation` |
| `relay/constant/relay_mode.go:57` | `RelayModeLyricsGeneration` 常量定义 |
| `relay/constant/relay_mode.go:94-95` | Path2RelayMode 路径映射 |
| `controller/relay.go:35-60` | relayHandler 分发到 AudioHelper |
| `relay/audio_handler.go` | AudioHelper 同步 Relay 管线 |
| `relay/channel/minimax/adaptor.go:35-93` | ConvertAudioRequest 请求转换 |
| `relay/channel/minimax/adaptor.go:138-160` | DoResponse 响应处理分发 |
| `relay/channel/minimax/music.go:38-41` | MiniMaxLyricsRequest 请求结构体 |
| `relay/channel/minimax/music.go:65-72` | MiniMaxLyricsResponse 响应结构体 |
| `relay/channel/minimax/music.go:100-122` | audioRequest2MiniMaxLyricsRequest 请求转换函数 |
| `relay/channel/minimax/music.go:199-237` | handleLyricsResponse 响应处理函数 |
| `relay/channel/minimax/relay-minimax.go:30-31` | GetRequestURL URL 构建 |
| `relay/channel/minimax/constants.go:32` | `minimax-lyrics` 模型注册 |
| `relay/relay_adaptor.go` | GetAdaptor 适配器工厂 |
| `dto/audio.go:12-38` | AudioRequest DTO 定义 |
| `middleware/swx_header.go` | SWX Header 透传中间件 |
| `service/error.go` | RelayErrorHandler 上游错误处理 |
| `service/http_client.go` | HTTP 客户端配置 |
| `types/error.go` | NewAPIError 错误类型体系 |

### 8.2 中间件链路

```
请求 → CORS
     → DecompressRequestMiddleware
     → BodyStorageCleanup
     → StatsMiddleware
     → RouteTag("relay")
     → SystemPerformanceCheck
     → TokenAuth
     → ModelRequestRateLimit
     → Distribute
     → controller.Relay
     → relayHandler
     → relay.AudioHelper
     → minimax.Adaptor
     → MiniMax API
```

### 8.3 与音乐生成的联动

歌词生成结果可直接用于音乐生成：

```
Step 1: POST /v1/lyrics_generation
        → 返回 { data: { lyrics, title }, style_tags }

Step 2: POST /v1/music_generation
        body: { model: "music-2.6", lyrics: "<Step 1 的 lyrics>", prompt: "<style_tags>" }
        → 返回音频 URL
```

`style_tags` 字段（MiniMax 响应中）可直接作为音乐生成 API 的 `prompt` 参数使用。

---

> 本文档由 SWAPI 团队维护，最后更新于 2026-07-13。

# MiniMax 音乐生成与歌词生成接入实现计划

> **For Claude:** Use `${SUPERPOWERS_SKILLS_ROOT}/skills/collaboration/executing-plans/SKILL.md` to implement this plan task-by-task.

**Goal:** 在 SWAPI 中接入 MiniMax 的音乐生成（`/v1/music_generation`）与歌词生成（`/v1/lyrics_generation`）API，复用现有同步 Relay 框架。

**Architecture:** MiniMax 音乐/歌词生成是**同步接口**（非异步任务），请求-响应模式与现有 TTS（`/v1/t2a_v2`）一致。因此复用 `RelayFormatOpenAIAudio` + `AudioHelper` 管线，在现有 minimax adaptor 上横向扩展，而非新建 Task adaptor。通过新增 `RelayMode` 区分音乐/歌词模式，在 `ConvertAudioRequest` 和 `DoResponse` 中分支处理。

**Tech Stack:** Go (gin框架)、SWAPI Relay 框架（RelayMode/RelayFormat/Adaptor）、MiniMax Music 2.6 API

---

## 架构决策记录

### 为什么选择同步 Relay 而非 Task 异步框架？

| 维度 | MiniMax 音乐生成 | Suno 音乐生成（已接入） |
|---|---|---|
| 调用模式 | **同步**：一次请求直接返回音频 | 异步：提交→轮询 |
| 端点 | `POST /v1/music_generation` | `POST /suno/submit/MUSIC` + `/suno/fetch` |
| 响应 | 直接返回 `data.audio`（hex/url） | 返回 `task_id`，需轮询 |
| 框架匹配 | `Adaptor`（同步 Relay） | `TaskAdaptor`（异步 Task） |

### 为什么复用 AudioRequest 而非新建 DTO？

1. **不修改 Adaptor 接口**：`ConvertAudioRequest` 已在接口中，无需新增方法（新增会影响 20+ adaptor）
2. **复用 AudioHelper 管线**：请求解析→渠道分发→转换→请求→响应→计费，全部复用
3. **Metadata 扩展机制**：AudioRequest 已有 `Metadata json.RawMessage` 字段，minimax TTS 已用此机制传递厂商参数
4. **最小侵入**：仅新增 2 个 optional 字段 + 2 个 RelayMode

### 请求体映射方案

MiniMax 原生请求 → SWAPI AudioRequest 映射：

```
MiniMax 原生                    SWAPI AudioRequest
─────────────                   ──────────────────
model          ──────────→      Model
prompt         ──────────→      Prompt (新增字段)
lyrics         ──────────→      Lyrics (新增字段)
audio_setting  ──────────→      Metadata (json.RawMessage, 已有)
stream         ──────────→      Metadata
output_format  ──────────→      Metadata / ResponseFormat (已有)
is_instrumental─────────→      Metadata
lyrics_optimizer────────→      Metadata
```

> 客户端也可将 prompt/lyrics 放入 metadata，adaptor 会 overlay 到请求结构体上（与 TTS 的 metadata 机制一致）。

---

## 文件变更总览

| # | 文件 | 操作 | 说明 |
|---|---|---|---|
| 1 | `relay/constant/relay_mode.go` | 修改 | 新增 2 个 RelayMode + Path2RelayMode 映射 |
| 2 | `dto/audio.go` | 修改 | AudioRequest 新增 Prompt、Lyrics 字段 |
| 3 | `relay/helper/valid_request.go` | 修改 | GetAndValidAudioRequest 支持新 mode |
| 4 | `router/relay-router.go` | 修改 | 注册 2 条新路由 |
| 5 | `controller/relay.go` | 修改 | relayHandler 分发新 mode 到 AudioHelper |
| 6 | `relay/channel/minimax/relay-minimax.go` | 修改 | GetRequestURL 新增端点映射 |
| 7 | `relay/channel/minimax/music.go` | **新建** | 音乐/歌词请求类型、响应处理 |
| 8 | `relay/channel/minimax/adaptor.go` | 修改 | ConvertAudioRequest + DoResponse 分支 |
| 9 | `relay/channel/minimax/constants.go` | 修改 | ModelList 新增音乐模型 |
| 10 | `relay/channel/minimax/adaptor_test.go` | 修改 | 新增测试用例 |

---

## Task 1: 新增 RelayMode 常量与路径映射

**Files:**
- Modify: `relay/constant/relay_mode.go`

**Step 1: 编写失败测试**

在 `relay/constant/relay_mode_test.go`（新建）中添加：

```go
package constant

import "testing"

func TestPath2RelayMode_MusicGeneration(t *testing.T) {
    tests := []struct {
        path string
        want int
    }{
        {"/v1/music_generation", RelayModeMusicGeneration},
        {"/v1/lyrics_generation", RelayModeLyricsGeneration},
    }
    for _, tt := range tests {
        got := Path2RelayMode(tt.path)
        if got != tt.want {
            t.Errorf("Path2RelayMode(%q) = %d, want %d", tt.path, got, tt.want)
        }
    }
}
```

**Step 2: 运行测试验证失败**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/constant/ -run TestPath2RelayMode_MusicGeneration -v`
Expected: FAIL（常量未定义）

**Step 3: 实现常量与路径映射**

在 `relay/constant/relay_mode.go` 的 `const` 块中，在 `RelayModeResponsesCompact` 后新增：

```go
    RelayModeMusicGeneration    // minimax 音乐生成
    RelayModeLyricsGeneration   // minimax 歌词生成
```

在 `Path2RelayMode` 函数中，在 `return relayMode` 之前添加：

```go
    } else if strings.HasPrefix(path, "/v1/music_generation") {
        relayMode = RelayModeMusicGeneration
    } else if strings.HasPrefix(path, "/v1/lyrics_generation") {
        relayMode = RelayModeLyricsGeneration
    }
```

**Step 4: 运行测试验证通过**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/constant/ -run TestPath2RelayMode_MusicGeneration -v`
Expected: PASS

**Step 5: Commit**

```bash
git add relay/constant/relay_mode.go relay/constant/relay_mode_test.go
git commit -m "feat: add RelayMode for minimax music/lyrics generation"
```

---

## Task 2: 扩展 AudioRequest DTO

**Files:**
- Modify: `dto/audio.go`
- Test: `dto/audio_test.go`（新建或追加）

**Step 1: 编写失败测试**

```go
package dto

import (
    "encoding/json"
    "testing"
)

func TestAudioRequest_UnmarshalMusicGeneration(t *testing.T) {
    raw := `{
        "model": "music-2.6",
        "prompt": "独立民谣,忧郁,内省",
        "lyrics": "[verse]\n街灯微亮",
        "metadata": {
            "audio_setting": {"sample_rate": 44100, "bitrate": 256000, "format": "mp3"},
            "stream": false,
            "output_format": "url"
        }
    }`
    var req AudioRequest
    if err := json.Unmarshal([]byte(raw), &req); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }
    if req.Model != "music-2.6" {
        t.Errorf("Model = %q, want %q", req.Model, "music-2.6")
    }
    if req.Prompt != "独立民谣,忧郁,内省" {
        t.Errorf("Prompt = %q, want %q", req.Prompt, "独立民谣,忧郁,内省")
    }
    if req.Lyrics != "[verse]\n街灯微亮" {
        t.Errorf("Lyrics = %q, want %q", req.Lyrics, "[verse]\n街灯微亮")
    }
    if len(req.Metadata) == 0 {
        t.Error("Metadata should not be empty")
    }
}

func TestAudioRequest_UnmarshalLyricsGeneration(t *testing.T) {
    raw := `{
        "model": "minimax-lyrics",
        "prompt": "一首欢乐的新年歌曲",
        "metadata": {
            "mode": "write_full_song"
        }
    }`
    var req AudioRequest
    if err := json.Unmarshal([]byte(raw), &req); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }
    if req.Prompt != "一首欢乐的新年歌曲" {
        t.Errorf("Prompt = %q, want %q", req.Prompt, "一首欢乐的新年歌曲")
    }
}

func TestAudioRequest_TTSFieldsUnaffected(t *testing.T) {
    // 确保新增字段不影响现有 TTS 请求解析
    raw := `{
        "model": "speech-02-hd",
        "input": "你好世界",
        "voice": "male-qn-qingse"
    }`
    var req AudioRequest
    if err := json.Unmarshal([]byte(raw), &req); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }
    if req.Input != "你好世界" {
        t.Errorf("Input = %q, want %q", req.Input, "你好世界")
    }
    if req.Prompt != "" {
        t.Errorf("Prompt should be empty for TTS, got %q", req.Prompt)
    }
}
```

**Step 2: 运行测试验证失败**

Run: `cd e:\Code\SinoWhale-api && go test ./dto/ -run TestAudioRequest_UnmarshalMusic -v`
Expected: FAIL（`req.Prompt` 未定义）

**Step 3: 在 AudioRequest 中新增字段**

在 `dto/audio.go` 的 `AudioRequest` struct 中，在 `Metadata` 字段之前添加：

```go
    // 音乐生成扩展字段（minimax music_generation）
    // prompt: 音乐风格描述，如 "流行音乐, 难过, 适合在下雨的晚上"
    Prompt string `json:"prompt,omitempty"`
    // lyrics: 歌词内容，支持结构标签 [Verse] [Chorus] 等
    Lyrics string `json:"lyrics,omitempty"`
```

**Step 4: 运行测试验证通过**

Run: `cd e:\Code\SinoWhale-api && go test ./dto/ -run TestAudioRequest -v`
Expected: PASS（全部 3 个测试）

**Step 5: Commit**

```bash
git add dto/audio.go dto/audio_test.go
git commit -m "feat: add Prompt/Lyrics fields to AudioRequest for music generation"
```

---

## Task 3: 更新请求校验函数

**Files:**
- Modify: `relay/helper/valid_request.go`

**Step 1: 编写失败测试**

在 `relay/helper/valid_request_test.go`（新建或追加）中：

```go
package helper

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/QuantumNous/new-api/common"
    relayconstant "github.com/QuantumNous/new-api/relay/constant"
    "github.com/gin-gonic/gin"
)

func TestGetAndValidAudioRequest_MusicGeneration(t *testing.T) {
    body := `{
        "model": "music-2.6",
        "prompt": "独立民谣,忧郁",
        "lyrics": "[verse]\n街灯微亮"
    }`
    c, _ := gin.CreateTestContext(httptest.NewRecorder())
    c.Request = httptest.NewRequest(http.MethodPost, "/v1/music_generation", strings.NewReader(body))
    c.Request.Header.Set("Content-Type", "application/json")
    common.SetContextKey(c, "relay_mode", relayconstant.RelayModeMusicGeneration)

    req, err := GetAndValidAudioRequest(c, relayconstant.RelayModeMusicGeneration)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if req.Model != "music-2.6" {
        t.Errorf("Model = %q, want %q", req.Model, "music-2.6")
    }
    if req.Prompt == "" {
        t.Error("Prompt should not be empty for music generation")
    }
}

func TestGetAndValidAudioRequest_LyricsGeneration_NoModelRequired(t *testing.T) {
    // 歌词生成 minimax 原生不需要 model，但 SWAPI 需要用于渠道路由
    body := `{
        "model": "minimax-lyrics",
        "prompt": "一首欢乐的新年歌曲"
    }`
    c, _ := gin.CreateTestContext(httptest.NewRecorder())
    c.Request = httptest.NewRequest(http.MethodPost, "/v1/lyrics_generation", strings.NewReader(body))
    c.Request.Header.Set("Content-Type", "application/json")

    req, err := GetAndValidAudioRequest(c, relayconstant.RelayModeLyricsGeneration)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if req.Model != "minimax-lyrics" {
        t.Errorf("Model = %q, want %q", req.Model, "minimax-lyrics")
    }
}
```

**Step 2: 运行测试验证失败**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/helper/ -run TestGetAndValidAudioRequest_MusicGeneration -v`
Expected: FAIL（新 mode 的校验逻辑未实现，可能报 "model is required"）

**Step 3: 更新 GetAndValidAudioRequest**

在 `relay/helper/valid_request.go` 的 `GetAndValidAudioRequest` 函数中，修改 switch 块：

```go
func GetAndValidAudioRequest(c *gin.Context, relayMode int) (*dto.AudioRequest, error) {
    audioRequest := &dto.AudioRequest{}
    err := common.UnmarshalBodyReusable(c, audioRequest)
    if err != nil {
        return nil, err
    }
    switch relayMode {
    case relayconstant.RelayModeAudioSpeech:
        if audioRequest.Model == "" {
            return nil, errors.New("model is required")
        }
    case relayconstant.RelayModeMusicGeneration:
        if audioRequest.Model == "" {
            return nil, errors.New("model is required")
        }
        if audioRequest.Prompt == "" && audioRequest.Input != "" {
            // 兼容：允许用 input 字段传 prompt
            audioRequest.Prompt = audioRequest.Input
        }
    case relayconstant.RelayModeLyricsGeneration:
        if audioRequest.Model == "" {
            return nil, errors.New("model is required")
        }
        if audioRequest.Prompt == "" && audioRequest.Input != "" {
            audioRequest.Prompt = audioRequest.Input
        }
    default:
        if audioRequest.Model == "" {
            return nil, errors.New("model is required")
        }
        if audioRequest.ResponseFormat == "" {
            audioRequest.ResponseFormat = "json"
        }
    }
    return audioRequest, nil
}
```

**Step 4: 运行测试验证通过**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/helper/ -run TestGetAndValidAudioRequest -v`
Expected: PASS

**Step 5: Commit**

```bash
git add relay/helper/valid_request.go relay/helper/valid_request_test.go
git commit -m "feat: support music/lyrics generation in audio request validation"
```

---

## Task 4: 注册新路由

**Files:**
- Modify: `router/relay-router.go`

**Step 1: 实现路由注册**

无需单独测试（路由注册是声明式代码，由集成测试覆盖）。

在 `router/relay-router.go` 的 `httpRouter` 块中，在 `audio/speech` 路由之后添加：

```go
        // music generation routes (minimax)
        httpRouter.POST("/music_generation", func(c *gin.Context) {
            controller.Relay(c, types.RelayFormatOpenAIAudio)
        })
        httpRouter.POST("/lyrics_generation", func(c *gin.Context) {
            controller.Relay(c, types.RelayFormatOpenAIAudio)
        })
```

**Step 2: 验证编译通过**

Run: `cd e:\Code\SinoWhale-api && go build ./router/`
Expected: 编译成功，无错误

**Step 3: Commit**

```bash
git add router/relay-router.go
git commit -m "feat: register /v1/music_generation and /v1/lyrics_generation routes"
```

---

## Task 5: 更新 relayHandler 分发逻辑

**Files:**
- Modify: `controller/relay.go`

**Step 1: 实现分发逻辑**

在 `controller/relay.go` 的 `relayHandler` 函数中，在 `RelayModeAudioTranscription` case 的 `fallthrough` 链中添加新 mode：

```go
func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
    var err *types.NewAPIError
    switch info.RelayMode {
    case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
        err = relay.ImageHelper(c, info)
    case relayconstant.RelayModeAudioSpeech:
        fallthrough
    case relayconstant.RelayModeAudioTranslation:
        fallthrough
    case relayconstant.RelayModeAudioTranscription:
        fallthrough
    case relayconstant.RelayModeMusicGeneration:
        fallthrough
    case relayconstant.RelayModeLyricsGeneration:
        err = relay.AudioHelper(c, info)
    case relayconstant.RelayModeRerank:
        err = relay.RerankHelper(c, info)
    case relayconstant.RelayModeEmbeddings:
        err = relay.EmbeddingHelper(c, info)
    case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
        err = relay.ResponsesHelper(c, info)
    default:
        err = relay.TextHelper(c, info)
    }
    return err
}
```

**Step 2: 验证编译通过**

Run: `cd e:\Code\SinoWhale-api && go build ./controller/`
Expected: 编译成功

**Step 3: Commit**

```bash
git add controller/relay.go
git commit -m "feat: dispatch music/lyrics generation to AudioHelper"
```

---

## Task 6: 扩展 minimax GetRequestURL

**Files:**
- Modify: `relay/channel/minimax/relay-minimax.go`
- Test: `relay/channel/minimax/adaptor_test.go`

**Step 1: 编写失败测试**

在 `relay/channel/minimax/adaptor_test.go` 中追加：

```go
func TestGetRequestURLForMusicGeneration(t *testing.T) {
    t.Parallel()
    info := &relaycommon.RelayInfo{
        RelayMode: relayconstant.RelayModeMusicGeneration,
        ChannelMeta: &relaycommon.ChannelMeta{
            ChannelBaseUrl: "https://api.minimax.chat",
        },
    }
    got, err := GetRequestURL(info)
    if err != nil {
        t.Fatalf("GetRequestURL returned error: %v", err)
    }
    want := "https://api.minimax.chat/v1/music_generation"
    if got != want {
        t.Fatalf("GetRequestURL() = %q, want %q", got, want)
    }
}

func TestGetRequestURLForLyricsGeneration(t *testing.T) {
    t.Parallel()
    info := &relaycommon.RelayInfo{
        RelayMode: relayconstant.RelayModeLyricsGeneration,
        ChannelMeta: &relaycommon.ChannelMeta{
            ChannelBaseUrl: "https://api.minimax.chat",
        },
    }
    got, err := GetRequestURL(info)
    if err != nil {
        t.Fatalf("GetRequestURL returned error: %v", err)
    }
    want := "https://api.minimax.chat/v1/lyrics_generation"
    if got != want {
        t.Fatalf("GetRequestURL() = %q, want %q", got, want)
    }
}
```

**Step 2: 运行测试验证失败**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/channel/minimax/ -run TestGetRequestURLForMusicGeneration -v`
Expected: FAIL（unsupported relay mode）

**Step 3: 实现 URL 映射**

在 `relay/channel/minimax/relay-minimax.go` 的 `GetRequestURL` 函数的 `switch info.RelayMode` 块中，在 `default` 之前添加：

```go
        case constant.RelayModeMusicGeneration:
            return fmt.Sprintf("%s/v1/music_generation", baseUrl), nil
        case constant.RelayModeLyricsGeneration:
            return fmt.Sprintf("%s/v1/lyrics_generation", baseUrl), nil
```

**Step 4: 运行测试验证通过**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/channel/minimax/ -run TestGetRequestURL -v`
Expected: PASS（全部）

**Step 5: Commit**

```bash
git add relay/channel/minimax/relay-minimax.go relay/channel/minimax/adaptor_test.go
git commit -m "feat: add music/lyrics generation URL mapping in minimax adaptor"
```

---

## Task 7: 创建 minimax music.go（请求/响应类型与处理函数）

**Files:**
- Create: `relay/channel/minimax/music.go`

**Step 1: 实现请求/响应类型与处理函数**

```go
package minimax

import (
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"

    "github.com/QuantumNous/new-api/common"
    "github.com/QuantumNous/new-api/dto"
    relaycommon "github.com/QuantumNous/new-api/relay/common"
    "github.com/QuantumNous/new-api/relay/constant"
    "github.com/QuantumNous/new-api/types"
    "github.com/gin-gonic/gin"
)

// ── 请求类型 ──────────────────────────────────────────────────────

// MiniMaxMusicRequest 对应 minimax /v1/music_generation 请求体
// 文档: https://platform.minimaxi.com/docs/api-reference/music-generation
type MiniMaxMusicRequest struct {
    Model           string          `json:"model"`
    Prompt          string          `json:"prompt,omitempty"`
    Lyrics          string          `json:"lyrics,omitempty"`
    Stream          bool            `json:"stream,omitempty"`
    OutputFormat    string          `json:"output_format,omitempty"`
    AudioSetting    *AudioSetting   `json:"audio_setting,omitempty"`
    AigcWatermark   *bool           `json:"aigc_watermark,omitempty"`
    LyricsOptimizer *bool           `json:"lyrics_optimizer,omitempty"`
    IsInstrumental  *bool           `json:"is_instrumental,omitempty"`
    AudioURL        string          `json:"audio_url,omitempty"`
    AudioBase64     string          `json:"audio_base64,omitempty"`
    CoverFeatureID  string          `json:"cover_feature_id,omitempty"`
}

// MiniMaxLyricsRequest 对应 minimax /v1/lyrics_generation 请求体
// 文档: https://platform.minimaxi.com/docs/api-reference/lyrics-generation
type MiniMaxLyricsRequest struct {
    Mode   string `json:"mode"`
    Prompt string `json:"prompt"`
}

// ── 响应类型 ──────────────────────────────────────────────────────

// MiniMaxMusicResponse 对应 minimax /v1/music_generation 响应
type MiniMaxMusicResponse struct {
    Data struct {
        Audio  string `json:"audio"`
        Status int    `json:"status"`
    } `json:"data"`
    TraceID    string          `json:"trace_id"`
    ExtraInfo  MusicExtraInfo  `json:"extra_info"`
    BaseResp   MiniMaxBaseResp `json:"base_resp"`
}

type MusicExtraInfo struct {
    MusicDuration   int    `json:"music_duration"`
    MusicSampleRate int    `json:"music_sample_rate"`
    MusicChannel    int    `json:"music_channel"`
    Bitrate         int    `json:"bitrate"`
    MusicSize       int64  `json:"music_size"`
}

// MiniMaxLyricsResponse 对应 minimax /v1/lyrics_generation 响应
type MiniMaxLyricsResponse struct {
    Data struct {
        Lyrics string `json:"lyrics"`
        Title  string `json:"title"`
    } `json:"data"`
    TraceID string          `json:"trace_id"`
    BaseResp MiniMaxBaseResp `json:"base_resp"`
}

// ── 请求转换 ──────────────────────────────────────────────────────

// audioRequest2MiniMaxMusicRequest 将 SWAPI AudioRequest 转换为 minimax 音乐生成请求
func audioRequest2MiniMaxMusicRequest(request dto.AudioRequest, modelName string) MiniMaxMusicRequest {
    minimaxReq := MiniMaxMusicRequest{
        Model:  modelName,
        Prompt: request.Prompt,
        Lyrics: request.Lyrics,
    }

    // 从 metadata 中 overlay 厂商扩展字段（与 TTS 的 metadata 机制一致）
    if len(request.Metadata) > 0 {
        _ = json.Unmarshal(request.Metadata, &minimaxReq)
        // 确保 model 使用映射后的名称
        minimaxReq.Model = modelName
    }

    // 兼容：允许用 input 字段传 prompt
    if minimaxReq.Prompt == "" && request.Input != "" {
        minimaxReq.Prompt = request.Input
    }

    return minimaxReq
}

// audioRequest2MiniMaxLyricsRequest 将 SWAPI AudioRequest 转换为 minimax 歌词生成请求
func audioRequest2MiniMaxLyricsRequest(request dto.AudioRequest) MiniMaxLyricsRequest {
    minimaxReq := MiniMaxLyricsRequest{
        Prompt: request.Prompt,
    }

    // 从 metadata 中提取 mode
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

// ── 响应处理 ──────────────────────────────────────────────────────

// handleMusicResponse 处理 minimax /v1/music_generation 响应
// 响应格式：{data: {audio: "hex/url", status: 2}, extra_info: {...}, base_resp: {...}}
func handleMusicResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
    body, readErr := io.ReadAll(resp.Body)
    if readErr != nil {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("failed to read minimax music response: %w", readErr),
            types.ErrorCodeReadResponseBodyFailed,
            http.StatusInternalServerError,
        )
    }
    defer resp.Body.Close()

    var minimaxResp MiniMaxMusicResponse
    if unmarshalErr := json.Unmarshal(body, &minimaxResp); unmarshalErr != nil {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("failed to unmarshal minimax music response: %w", unmarshalErr),
            types.ErrorCodeBadResponseBody,
            http.StatusInternalServerError,
        )
    }

    // 检查 base_resp 状态
    if minimaxResp.BaseResp.StatusCode != 0 {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("minimax music error: %d - %s", minimaxResp.BaseResp.StatusCode, minimaxResp.BaseResp.StatusMsg),
            types.ErrorCodeBadResponse,
            http.StatusBadRequest,
        )
    }

    // 检查音频数据
    if minimaxResp.Data.Audio == "" {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("no audio data in minimax music response"),
            types.ErrorCodeBadResponse,
            http.StatusBadRequest,
        )
    }

    // 输出音频（与 TTS 处理逻辑一致）
    if isURL(minimaxResp.Data.Audio) {
        // url 格式：直接重定向或返回 JSON
        c.JSON(http.StatusOK, gin.H{
            "data": gin.H{
                "audio": minimaxResp.Data.Audio,
                "status": minimaxResp.Data.Status,
            },
            "extra_info": minimaxResp.ExtraInfo,
            "trace_id": minimaxResp.TraceID,
        })
    } else {
        // hex 格式：解码为二进制音频
        audioData, decodeErr := hex.DecodeString(minimaxResp.Data.Audio)
        if decodeErr != nil {
            return nil, types.NewErrorWithStatusCode(
                fmt.Errorf("failed to decode hex audio data: %w", decodeErr),
                types.ErrorCodeBadResponse,
                http.StatusInternalServerError,
            )
        }
        contentType := getContentTypeByFormat("mp3")
        c.Data(http.StatusOK, contentType, audioData)
    }

    // 返回 usage（按次计费，token 设为 0）
    usage = &dto.Usage{
        PromptTokens:     0,
        CompletionTokens: 0,
        TotalTokens:      0,
    }
    return usage, nil
}

// handleLyricsResponse 处理 minimax /v1/lyrics_generation 响应
// 响应格式：{data: {lyrics: "...", title: "..."}, trace_id, base_resp}
func handleLyricsResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
    body, readErr := io.ReadAll(resp.Body)
    if readErr != nil {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("failed to read minimax lyrics response: %w", readErr),
            types.ErrorCodeReadResponseBodyFailed,
            http.StatusInternalServerError,
        )
    }
    defer resp.Body.Close()

    var minimaxResp MiniMaxLyricsResponse
    if unmarshalErr := json.Unmarshal(body, &minimaxResp); unmarshalErr != nil {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("failed to unmarshal minimax lyrics response: %w", unmarshalErr),
            types.ErrorCodeBadResponseBody,
            http.StatusInternalServerError,
        )
    }

    if minimaxResp.BaseResp.StatusCode != 0 {
        return nil, types.NewErrorWithStatusCode(
            fmt.Errorf("minimax lyrics error: %d - %s", minimaxResp.BaseResp.StatusCode, minimaxResp.BaseResp.StatusMsg),
            types.ErrorCodeBadResponse,
            http.StatusBadRequest,
        )
    }

    // 透传 minimax 响应
    c.JSON(http.StatusOK, minimaxResp)

    // 按次计费
    usage = &dto.Usage{
        PromptTokens:     0,
        CompletionTokens: 0,
        TotalTokens:      0,
    }
    return usage, nil
}

// isURL 检查字符串是否为 URL
func isURL(s string) bool {
    return len(s) > 4 && (s[:4] == "http")
}
```

**Step 2: 验证编译通过**

Run: `cd e:\Code\SinoWhale-api && go build ./relay/channel/minimax/`
Expected: 编译成功

**Step 3: Commit**

```bash
git add relay/channel/minimax/music.go
git commit -m "feat: add minimax music/lyrics request types and response handlers"
```

---

## Task 8: 更新 minimax adaptor 的 ConvertAudioRequest 和 DoResponse

**Files:**
- Modify: `relay/channel/minimax/adaptor.go`
- Test: `relay/channel/minimax/adaptor_test.go`

**Step 1: 编写失败测试**

在 `adaptor_test.go` 中追加：

```go
func TestConvertAudioRequest_MusicGeneration(t *testing.T) {
    t.Parallel()
    adaptor := &Adaptor{}
    info := &relaycommon.RelayInfo{
        RelayMode:       relayconstant.RelayModeMusicGeneration,
        OriginModelName: "music-2.6",
    }
    request := dto.AudioRequest{
        Model:  "music-2.6",
        Prompt: "独立民谣,忧郁",
        Lyrics: "[verse]\n街灯微亮",
    }

    reader, err := adaptor.ConvertAudioRequest(
        gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New()),
        info,
        request,
    )
    if err != nil {
        t.Fatalf("ConvertAudioRequest returned error: %v", err)
    }

    body, _ := io.ReadAll(reader)
    var minimaxReq MiniMaxMusicRequest
    if err := json.Unmarshal(body, &minimaxReq); err != nil {
        t.Fatalf("failed to unmarshal request body: %v", err)
    }
    if minimaxReq.Model != "music-2.6" {
        t.Errorf("Model = %q, want %q", minimaxReq.Model, "music-2.6")
    }
    if minimaxReq.Prompt != "独立民谣,忧郁" {
        t.Errorf("Prompt = %q, want %q", minimaxReq.Prompt, "独立民谣,忧郁")
    }
    if minimaxReq.Lyrics != "[verse]\n街灯微亮" {
        t.Errorf("Lyrics mismatch")
    }
}

func TestConvertAudioRequest_LyricsGeneration(t *testing.T) {
    t.Parallel()
    adaptor := &Adaptor{}
    info := &relaycommon.RelayInfo{
        RelayMode:       relayconstant.RelayModeLyricsGeneration,
        OriginModelName: "minimax-lyrics",
    }
    request := dto.AudioRequest{
        Model:  "minimax-lyrics",
        Prompt: "一首欢乐的新年歌曲",
    }

    reader, err := adaptor.ConvertAudioRequest(
        gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New()),
        info,
        request,
    )
    if err != nil {
        t.Fatalf("ConvertAudioRequest returned error: %v", err)
    }

    body, _ := io.ReadAll(reader)
    var minimaxReq MiniMaxLyricsRequest
    if err := json.Unmarshal(body, &minimaxReq); err != nil {
        t.Fatalf("failed to unmarshal request body: %v", err)
    }
    if minimaxReq.Prompt != "一首欢乐的新年歌曲" {
        t.Errorf("Prompt = %q, want %q", minimaxReq.Prompt, "一首欢乐的新年歌曲")
    }
    if minimaxReq.Mode != "write_full_song" {
        t.Errorf("Mode = %q, want %q", minimaxReq.Mode, "write_full_song")
    }
}
```

**Step 2: 运行测试验证失败**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/channel/minimax/ -run TestConvertAudioRequest_MusicGeneration -v`
Expected: FAIL（`unsupported audio relay mode`）

**Step 3: 更新 ConvertAudioRequest**

在 `relay/channel/minimax/adaptor.go` 的 `ConvertAudioRequest` 方法中，在 `if info.RelayMode != constant.RelayModeAudioSpeech` 判断之前，添加音乐/歌词分支：

```go
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
    switch info.RelayMode {
    case constant.RelayModeAudioSpeech:
        // 原有 TTS 逻辑保持不变
        voiceID := request.Voice
        speed := lo.FromPtrOr(request.Speed, 0.0)
        outputFormat := request.ResponseFormat

        minimaxRequest := MiniMaxTTSRequest{
            Model: info.OriginModelName,
            Text:  request.Input,
            VoiceSetting: VoiceSetting{
                VoiceID: voiceID,
                Speed:   speed,
            },
            AudioSetting: &AudioSetting{
                Format: outputFormat,
            },
            OutputFormat: outputFormat,
        }

        if len(request.Metadata) > 0 {
            if err := json.Unmarshal(request.Metadata, &minimaxRequest); err != nil {
                return nil, fmt.Errorf("error unmarshalling metadata to minimax request: %w", err)
            }
        }

        jsonData, err := json.Marshal(minimaxRequest)
        if err != nil {
            return nil, fmt.Errorf("error marshalling minimax request: %w", err)
        }
        if outputFormat != "hex" {
            outputFormat = "url"
        }
        c.Set("response_format", outputFormat)
        return bytes.NewReader(jsonData), nil

    case constant.RelayModeMusicGeneration:
        minimaxRequest := audioRequest2MiniMaxMusicRequest(request, info.OriginModelName)
        jsonData, err := json.Marshal(minimaxRequest)
        if err != nil {
            return nil, fmt.Errorf("error marshalling minimax music request: %w", err)
        }
        return bytes.NewReader(jsonData), nil

    case constant.RelayModeLyricsGeneration:
        minimaxRequest := audioRequest2MiniMaxLyricsRequest(request)
        jsonData, err := json.Marshal(minimaxRequest)
        if err != nil {
            return nil, fmt.Errorf("error marshalling minimax lyrics request: %w", err)
        }
        return bytes.NewReader(jsonData), nil

    default:
        return nil, errors.New("unsupported audio relay mode")
    }
}
```

**Step 4: 更新 DoResponse 分支**

在 `relay/channel/minimax/adaptor.go` 的 `DoResponse` 方法中，在 TTS 和 Image 分支之前添加音乐/歌词分支：

```go
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
    if info.RelayMode == constant.RelayModeMusicGeneration {
        return handleMusicResponse(c, resp, info)
    }
    if info.RelayMode == constant.RelayModeLyricsGeneration {
        return handleLyricsResponse(c, resp, info)
    }
    if info.RelayMode == constant.RelayModeAudioSpeech {
        return handleTTSResponse(c, resp, info)
    }
    if info.RelayMode == constant.RelayModeImagesGenerations {
        return miniMaxImageHandler(c, resp, info)
    }

    switch info.RelayFormat {
    case types.RelayFormatClaude:
        adaptor := claude.Adaptor{}
        return adaptor.DoResponse(c, resp, info)
    default:
        adaptor := openai.Adaptor{}
        return adaptor.DoResponse(c, resp, info)
    }
}
```

**Step 5: 运行测试验证通过**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/channel/minimax/ -v`
Expected: PASS（全部测试）

**Step 6: Commit**

```bash
git add relay/channel/minimax/adaptor.go relay/channel/minimax/adaptor_test.go
git commit -m "feat: integrate music/lyrics generation into minimax adaptor"
```

---

## Task 9: 更新 minimax 模型列表

**Files:**
- Modify: `relay/channel/minimax/constants.go`

**Step 1: 添加音乐模型到 ModelList**

在 `relay/channel/minimax/constants.go` 的 `ModelList` 中追加：

```go
var ModelList = []string{
    // ... 现有模型 ...
    "image-01",
    "image-01-live",
    // 音乐生成模型
    "music-2.6",
    "music-cover",
    "music-2.6-free",
    "music-cover-free",
    // 歌词生成模型（SWAPI 内部路由用，非 minimax 原生模型名）
    "minimax-lyrics",
}
```

**Step 2: 验证编译通过**

Run: `cd e:\Code\SinoWhale-api && go build ./relay/channel/minimax/`
Expected: 编译成功

**Step 3: Commit**

```bash
git add relay/channel/minimax/constants.go
git commit -m "feat: add music/lyrics models to minimax model list"
```

---

## Task 10: 端到端集成验证

**Files:**
- 无新文件，使用现有项目编译和测试

**Step 1: 全量编译验证**

Run: `cd e:\Code\SinoWhale-api && go build ./...`
Expected: 编译成功，无错误

**Step 2: 全量单元测试**

Run: `cd e:\Code\SinoWhale-api && go test ./relay/... ./dto/... ./controller/... -v -count=1`
Expected: 所有测试 PASS

**Step 3: API 路由验证**

启动 SWAPI 后，验证路由注册：

```bash
# 音乐生成
curl -X POST http://localhost:3000/v1/music_generation \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "music-2.6-free",
    "prompt": "流行音乐, 欢快",
    "lyrics": "[verse]\n测试歌词",
    "metadata": {
      "output_format": "url"
    }
  }'
# 预期：返回音频 URL 或 hex 数据（取决于上游响应）

# 歌词生成
curl -X POST http://localhost:3000/v1/lyrics_generation \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-lyrics",
    "prompt": "一首欢乐的新年歌曲",
    "metadata": {
      "mode": "write_full_song"
    }
  }'
# 预期：返回歌词文本
```

**Step 4: Commit**

```bash
git commit --allow-empty -m "chore: verify minimax music/lyrics generation integration"
```

---

## Task 11: 功能回归测试清单

> **根据开发约定 §2，所有开发计划最后一个 Task 必须是功能回归测试清单。**

### 数据模型层

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.1.1 | AudioRequest 反序列化音乐生成请求 | 无 | `Prompt`、`Lyrics`、`Model`、`Metadata` 字段正确填充 | P0 |
| 11.1.2 | AudioRequest 反序列化歌词生成请求 | 无 | `Prompt`、`Model` 字段正确，`Metadata.mode` 正确 | P0 |
| 11.1.3 | AudioRequest 反序列化 TTS 请求不受影响 | 无 | `Input`、`Voice` 字段正确，`Prompt`/`Lyrics` 为空 | P0 |
| 11.1.4 | AudioRequest 反序列化 STT 请求不受影响 | 无 | `Input`、`Model` 字段正确 | P1 |

### 核心业务逻辑

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.2.1 | audioRequest2MiniMaxMusicRequest 转换正确 | AudioRequest 含 prompt/lyrics/metadata | MiniMaxMusicRequest 字段正确映射 | P0 |
| 11.2.2 | audioRequest2MiniMaxLyricsRequest 转换正确 | AudioRequest 含 prompt/metadata.mode | MiniMaxLyricsRequest 字段正确，mode 默认 write_full_song | P0 |
| 11.2.3 | input 字段兼容映射到 prompt | AudioRequest.Input 非空，Prompt 为空 | 转换后 Prompt 取 Input 值 | P1 |
| 11.2.4 | Metadata overlay 不覆盖 model | metadata 含 model 字段 | 转换后 model 仍为 info.OriginModelName | P1 |

### API 路由层

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.3.1 | POST /v1/music_generation 路由可达 | SWAPI 已启动 | 路由匹配，进入 AudioHelper | P0 |
| 11.3.2 | POST /v1/lyrics_generation 路由可达 | SWAPI 已启动 | 路由匹配，进入 AudioHelper | P0 |
| 11.3.3 | Path2RelayMode 正确识别 /v1/music_generation | 无 | 返回 RelayModeMusicGeneration | P0 |
| 11.3.4 | Path2RelayMode 正确识别 /v1/lyrics_generation | 无 | 返回 RelayModeLyricsGeneration | P0 |
| 11.3.5 | relayHandler 将 music/lyrics mode 分发到 AudioHelper | 无 | 进入 AudioHelper 而非 TextHelper | P0 |

### 代理中间层（Minimax Adaptor）

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.4.1 | GetRequestURL 返回 music_generation 端点 | RelayMode=MusicGeneration | URL = {baseUrl}/v1/music_generation | P0 |
| 11.4.2 | GetRequestURL 返回 lyrics_generation 端点 | RelayMode=LyricsGeneration | URL = {BaseUrl}/v1/lyrics_generation | P0 |
| 11.4.3 | ConvertAudioRequest 音乐模式生成正确 JSON | RelayMode=MusicGeneration | 请求体含 model/prompt/lyrics 字段 | P0 |
| 11.4.4 | ConvertAudioRequest 歌词模式生成正确 JSON | RelayMode=LyricsGeneration | 请求体含 mode/prompt 字段 | P0 |
| 11.4.5 | ConvertAudioRequest TTS 模式不受影响 | RelayMode=AudioSpeech | 请求体含 text/voice_setting（原有逻辑） | P0 |
| 11.4.6 | DoResponse 音乐模式 hex 音频解码 | 上游返回 hex 编码音频 | 客户端收到二进制音频数据 | P0 |
| 11.4.7 | DoResponse 音乐模式 url 格式返回 | 上游返回 url 格式 | 客户端收到 JSON 含 audio url | P1 |
| 11.4.8 | DoResponse 歌词模式返回 JSON | 上游返回歌词 JSON | 客户端收到透传 JSON | P0 |
| 11.4.9 | DoResponse 上游错误时返回错误码 | base_resp.status_code != 0 | 返回 400 + 错误信息 | P1 |
| 11.4.10 | DoResponse TTS 模式不受影响 | RelayMode=AudioSpeech | 走 handleTTSResponse（原有逻辑） | P0 |

### 跨服务端到端

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.5.1 | 音乐生成完整调用链 | minimax 渠道已配置，model=music-2.6-free | 200 + 音频数据/URL | P0 |
| 11.5.2 | 歌词生成完整调用链 | minimax 渠道已配置，model=minimax-lyrics | 200 + 歌词文本 | P0 |
| 11.5.3 | 音乐生成计费正确 | model 有按次定价配置 | 按次扣费，非按 token | P1 |
| 11.5.4 | 音乐生成失败退费 | 上游返回错误 | 预扣额度退还 | P1 |

### 安全与权限

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.6.1 | 无 Token 请求音乐生成 | 不带 Authorization | 401 Unauthorized | P0 |
| 11.6.2 | 余额不足请求音乐生成 | 用户余额为 0 | 返回余额不足错误 | P1 |

### 现有功能无回归

| # | 测试场景 | 前置条件 | 预期结果 | 优先级 |
|---|---|---|---|---|
| 11.7.1 | minimax 文本对话（chatcompletion_v2）正常 | RelayMode=ChatCompletions | 响应与改动前一致 | P0 |
| 11.7.2 | minimax TTS（t2a_v2）正常 | RelayMode=AudioSpeech | 响应与改动前一致 | P0 |
| 11.7.3 | minimax 图像生成正常 | RelayMode=ImagesGenerations | 响应与改动前一致 | P0 |
| 11.7.4 | minimax Claude 兼容格式正常 | RelayFormat=Claude | 响应与改动前一致 | P1 |
| 11.7.5 | 其他渠道 TTS（OpenAI等）正常 | 非 minimax 渠道 | AudioRequest 新字段不影响其他渠道 | P0 |
| 11.7.6 | 其他渠道 STT 正常 | 非 minimax 渠道 | AudioRequest 新字段不影响 STT | P1 |
| 11.7.7 | Suno 音乐生成不受影响 | /suno/submit/MUSIC | 走 TaskAdaptor，不受 minimax 改动影响 | P0 |
| 11.7.8 | Suno 歌词生成不受影响 | /suno/submit/LYRICS | 走 TaskAdaptor，不受 minimax 改动影响 | P0 |

---

## 实现顺序依赖图

```
Task 1 (RelayMode) ──┐
                     ├─→ Task 3 (Validation) ──→ Task 4 (Routes) ──→ Task 5 (Dispatch)
Task 2 (DTO) ────────┘                                                        │
                                                                              ↓
Task 6 (URL Mapping) ──→ Task 7 (music.go) ──→ Task 8 (Adaptor) ──→ Task 9 (Models)
                                                                       │
                                                                       ↓
                                                              Task 10 (E2E Verify)
                                                                       │
                                                                       ↓
                                                              Task 11 (Regression Checklist)
```

## 超时风险应对

MiniMax 音乐生成同步接口可能耗时 10-60 秒（25 秒音频）。若超时问题严重，后续可封装为 Task 异步模式：

1. SWAPI 侧生成内部 `task_id`
2. 异步调用 minimax `/v1/music_generation`
3. 存储结果供 `GET /v1/music_generation/{task_id}` 轮询

此为后续优化项，当前先以同步模式上线。

package minimax

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// ── 请求类型 ──────────────────────────────────────────────────────

// MiniMaxMusicRequest 对应 minimax /v1/music_generation 请求体
// 文档: https://platform.minimaxi.com/docs/api-reference/music-generation
type MiniMaxMusicRequest struct {
	Model           string        `json:"model"`
	Prompt          string        `json:"prompt,omitempty"`
	Lyrics          string        `json:"lyrics,omitempty"`
	Stream          bool          `json:"stream,omitempty"`
	OutputFormat    string        `json:"output_format,omitempty"`
	AudioSetting    *AudioSetting `json:"audio_setting,omitempty"`
	AigcWatermark   *bool         `json:"aigc_watermark,omitempty"`
	LyricsOptimizer *bool         `json:"lyrics_optimizer,omitempty"`
	IsInstrumental  *bool         `json:"is_instrumental,omitempty"`
	AudioURL        string        `json:"audio_url,omitempty"`
	AudioBase64     string        `json:"audio_base64,omitempty"`
	CoverFeatureID  string        `json:"cover_feature_id,omitempty"`
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
	TraceID   string          `json:"trace_id"`
	ExtraInfo MusicExtraInfo  `json:"extra_info"`
	BaseResp  MiniMaxBaseResp `json:"base_resp"`
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
	TraceID  string          `json:"trace_id"`
	BaseResp MiniMaxBaseResp `json:"base_resp"`
}

// ── 请求转换 ──────────────────────────────────────────────────────

// audioRequest2MiniMaxMusicRequest 将 SWAPI AudioRequest 转换为 minimax 音乐生成请求
func audioRequest2MiniMaxMusicRequest(request dto.AudioRequest, modelName string) MiniMaxMusicRequest {
	minimaxReq := MiniMaxMusicRequest{
		Model:        modelName,
		Prompt:       request.Prompt,
		Lyrics:       request.Lyrics,
		OutputFormat: request.OutputFormat,
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
	if strings.HasPrefix(minimaxResp.Data.Audio, "http") {
		// url 格式：返回 JSON
		c.JSON(http.StatusOK, gin.H{
			"data": gin.H{
				"audio":  minimaxResp.Data.Audio,
				"status": minimaxResp.Data.Status,
			},
			"extra_info": minimaxResp.ExtraInfo,
			"trace_id":   minimaxResp.TraceID,
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

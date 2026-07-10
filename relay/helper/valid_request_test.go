package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

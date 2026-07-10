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

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

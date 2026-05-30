//go:build !js && !cuda

package shasolve

import (
	"strings"
	"testing"
)

func TestBuildWebGPUDifficultyCheck(t *testing.T) {
	tests := []struct {
		difficulty uint32
		want       string
	}{
		{0, "return true;"},
		{1, "return (h7 & 0x00000001u) == 0u;"},
		{28, "return (h7 & 0x0fffffffu) == 0u;"},
		{32, "return h7 == 0u;"},
		{33, "return h7 == 0u && (h6 & 0x00000001u) == 0u;"},
		{64, "return h7 == 0u && h6 == 0u;"},
		{80, "return h7 == 0u && h6 == 0u && (h5 & 0x0000ffffu) == 0u;"},
	}
	for _, tt := range tests {
		got := strings.TrimSpace(buildWebGPUDifficultyCheck(tt.difficulty))
		if got != tt.want {
			t.Fatalf("difficulty %d: got %q, want %q", tt.difficulty, got, tt.want)
		}
	}
}

func TestBuildWebGPUShaderReplacesPlaceholders(t *testing.T) {
	header, err := NewPowcoinHeader(BenchmarkSignature(), 28)
	if err != nil {
		t.Fatal(err)
	}
	for _, difficulty := range []uint32{0, 1, 28, 32, 64, 80} {
		shader := buildWebGPUShader(header, difficulty)
		if strings.Contains(shader, "{{") {
			t.Fatalf("difficulty %d: unreplaced template placeholder in shader", difficulty)
		}
		if !strings.Contains(shader, "const MID_0: u32") || !strings.Contains(shader, "const TAIL_2: u32") {
			t.Fatalf("difficulty %d: shader is missing generated work constants", difficulty)
		}
	}
}

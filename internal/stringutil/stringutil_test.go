package stringutil

import "testing"

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"empty string", "", 10, ""},
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"maxLen 4 (minimum)", "hello", 4, "h..."},
		{"maxLen 3 (too small)", "hello", 3, "hello"},
		{"maxLen 0", "hello", 0, "hello"},
		{"maxLen negative", "hello", -1, "hello"},
		{"unicode string", "héllo wörld", 8, "héllo..."},
		{"unicode truncation", "日本語テスト", 5, "日本..."},
		{"emoji", "👋🌍🎉", 2, "👋🌍🎉"},                 // maxLen < 4, returns unchanged
		{"emoji no truncate", "👋🌍🎉🚀🌟", 5, "👋🌍🎉🚀🌟"}, // exactly 5 runes = maxLen
		{"emoji truncate", "👋🌍🎉🚀🌟🎊", 5, "👋🌍..."},   // 6 runes > maxLen 5
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func BenchmarkTruncate(b *testing.B) {
	s := "This is a moderately long string that will need to be truncated"
	for range b.N {
		_ = Truncate(s, 20)
	}
}

func BenchmarkTruncate_NoTruncation(b *testing.B) {
	s := "short"
	for range b.N {
		_ = Truncate(s, 20)
	}
}

func BenchmarkTruncate_Unicode(b *testing.B) {
	s := "日本語のテスト文字列です"
	for range b.N {
		_ = Truncate(s, 8)
	}
}

package compcache

import (
	"strings"
	"testing"
)

func TestCompressDecompress(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	if c.enc == nil || c.dec == nil {
		t.Skip("zstd not available")
	}

	original := strings.Repeat("The quick brown fox jumps over the lazy dog. Token optimization is important for reducing API costs. ", 30)
	data := []byte(original)

	compressed := c.compress(data)
	if len(compressed) >= len(data) {
		t.Errorf("compressed (%d) should be smaller than original (%d)", len(compressed), len(data))
	}

	decompressed, err := c.decompress(compressed)
	if err != nil {
		t.Fatalf("decompress failed: %v", err)
	}
	if string(decompressed) != original {
		t.Error("decompressed data doesn't match original")
	}
}

func TestCompressSmall(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	if c.enc == nil {
		t.Skip("zstd not available")
	}

	small := strings.Repeat("x", 100)
	ratio := c.CompressionRatio([]byte(small))
	if ratio != 0 {
		t.Error("small values should not be compressed")
	}
}

func TestCompressionRatio(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	if c.enc == nil {
		t.Skip("zstd not available")
	}

	data := strings.Repeat("abc def ghi jkl mno pqr stu vwx yz ", 100)
	ratio := c.CompressionRatio([]byte(data))
	if ratio == 0 {
		t.Error("repetitive data should compress")
	}
	if ratio > 0.5 {
		t.Errorf("compression ratio should be < 0.5, got %.3f", ratio)
	}
}

func TestCompressionRatioDisabled(t *testing.T) {
	c := &CompCache{cfg: Config{Enabled: false, MinSize: 512, Level: 3}}
	ratio := c.CompressionRatio([]byte(strings.Repeat("x", 1000)))
	if ratio != 0 {
		t.Error("no encoder should return 0 ratio")
	}
}

func TestNewNilRedis(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	// Should not panic
	_, err := c.CompressedGet(nil, "test")
	if err == nil {
		t.Error("nil redis should return error")
	}
}

func TestCompressedGetSetNilRedis(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	err := c.CompressedSet(nil, "key", "value", 0)
	if err != nil {
		t.Error("nil redis set should not error")
	}
}

func TestCompressEmpty(t *testing.T) {
	c := New(nil, Config{Enabled: true, MinSize: 512, Level: 3})
	if c.enc == nil {
		t.Skip("zstd not available")
	}

	compressed := c.compress([]byte{})
	// Zstd EncodeAll on empty may return empty or a frame header - both valid
	_ = compressed
}

func TestLoadConfig(t *testing.T) {
	cfg := LoadConfig()
	if !cfg.Enabled {
		t.Error("enabled should default to true")
	}
	if cfg.MinSize != 512 {
		t.Errorf("minSize should default to 512, got %d", cfg.MinSize)
	}
	if cfg.Level != 3 {
		t.Errorf("level should default to 3, got %d", cfg.Level)
	}
}

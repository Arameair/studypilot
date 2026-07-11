package course

import (
	"path/filepath"
	"testing"
)

func TestAssetDirectory(t *testing.T) {
	tests := []struct {
		kind AssetKind
		want string
	}{
		{kind: AssetScreenshot, want: filepath.Join("Assets", "Screenshots")},
		{kind: AssetAudio, want: filepath.Join("Assets", "Audio")},
		{kind: AssetVideo, want: filepath.Join("Assets", "Video")},
		{kind: AssetDocument, want: filepath.Join("Assets", "Documents")},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			got, err := AssetDirectory(test.kind)
			if err != nil {
				t.Fatalf("AssetDirectory() error = %v", err)
			}
			if got != test.want {
				t.Errorf("AssetDirectory() = %q, want %q", got, test.want)
			}
		})
	}
	if _, err := AssetDirectory("unknown"); err == nil {
		t.Fatal("AssetDirectory(unknown) error = nil, want error")
	}
}

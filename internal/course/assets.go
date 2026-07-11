package course

import (
	"fmt"
	"path/filepath"
)

type AssetKind string

const (
	AssetScreenshot AssetKind = "screenshot"
	AssetAudio      AssetKind = "audio"
	AssetVideo      AssetKind = "video"
	AssetDocument   AssetKind = "document"
)

// AssetDirectory returns the module-relative directory for an asset kind.
func AssetDirectory(kind AssetKind) (string, error) {
	switch kind {
	case AssetScreenshot:
		return filepath.Join("Assets", "Screenshots"), nil
	case AssetAudio:
		return filepath.Join("Assets", "Audio"), nil
	case AssetVideo:
		return filepath.Join("Assets", "Video"), nil
	case AssetDocument:
		return filepath.Join("Assets", "Documents"), nil
	default:
		return "", fmt.Errorf("unknown asset kind %q", kind)
	}
}

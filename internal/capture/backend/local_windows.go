//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Arameair/studypilot/internal/platformfs"
)

func localExecutableIssue(path string) (string, string) {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Base(path), "ffmpeg.exe") {
		return "capture_executable_unsafe", "the configured capture executable is not an allowed ffmpeg file"
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "capture_executable_missing", "the configured capture executable is unavailable"
	}
	reparse, reparseErr := platformfs.PathHasReparsePoint(path)
	multiple, linkErr := platformfs.HasMultipleHardLinks(path)
	if reparseErr != nil || reparse || linkErr != nil || multiple || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "capture_executable_unsafe", "the configured capture executable is not a safe regular executable"
	}
	return "", ""
}

func localDriverIssue(driver string) (string, string) {
	switch driver {
	case "dshow":
		return "", ""
	case "pulse", "alsa":
		return "capture_driver_unsupported_on_platform", "the configured capture driver is not supported on this platform"
	default:
		return "capture_driver_unsupported", "the configured capture driver is unsupported"
	}
}

func validPlatformDevice(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "audio=") || strings.HasPrefix(lower, "video=") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func localFFmpegArgs(_ string, device, output string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", "dshow",
		"-i", "audio=" + device,
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		"-map_metadata", "-1",
		"-fflags", "+bitexact",
		"-flags:a", "+bitexact",
		"-f", "wav",
		"-y", output,
	}
}

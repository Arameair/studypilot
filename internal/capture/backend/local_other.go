//go:build !windows

package backend

import (
	"os"
	"path/filepath"
)

func localExecutableIssue(path string) (string, string) {
	if !filepath.IsAbs(path) || filepath.Base(path) != "ffmpeg" {
		return "capture_executable_unsafe", "the configured capture executable is not an allowed ffmpeg file"
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "capture_executable_missing", "the configured capture executable is unavailable"
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 || hasMultipleHardLinks(info) {
		return "capture_executable_unsafe", "the configured capture executable is not a safe regular executable"
	}
	return "", ""
}

func localDriverIssue(driver string) (string, string) {
	switch driver {
	case "pulse", "alsa":
		return "", ""
	case "dshow":
		return "capture_driver_unsupported_on_platform", "the configured capture driver is not supported on this platform"
	default:
		return "capture_driver_unsupported", "the configured capture driver is unsupported"
	}
}

func validPlatformDevice(string) bool { return true }

func localFFmpegArgs(driver, device, output string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-f", driver, "-i", device, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-map_metadata", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-f", "wav", "-y", output}
}

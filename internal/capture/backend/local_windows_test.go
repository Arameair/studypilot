//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowsLocalConfigurationAndDirectShowArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "FFmpeg Unicode 路径", "bin with spaces")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(dir, "FFMPEG.EXE")
	if err := os.WriteFile(executable, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	device := "Microphone Array (Realtek(R) Audio) & Café"
	if issues := LocalConfigurationIssues(executable, "dshow", device); len(issues) != 0 {
		t.Fatalf("valid Windows configuration issues=%+v", issues)
	}
	output := filepath.Join(t.TempDir(), "001-audio.wav.partial")
	want := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "dshow",
		"-i", "audio=" + device,
		"-ac", "1", "-ar", "16000",
		"-c:a", "pcm_s16le",
		"-map_metadata", "-1",
		"-fflags", "+bitexact",
		"-flags:a", "+bitexact",
		"-f", "wav", "-y", output,
	}
	if got := localFFmpegArgs("dshow", device, output); !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments=%q", got)
	}
}

func TestWindowsLocalConfigurationRejectsUnsafeValues(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(valid, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	for name, executable := range map[string]string{
		"relative":       "ffmpeg.exe",
		"wrong basename": filepath.Join(dir, "recorder.exe"),
		"batch":          filepath.Join(dir, "ffmpeg.cmd"),
		"missing":        filepath.Join(dir, "missing", "ffmpeg.exe"),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := LocalConfigurationIssues(executable, "dshow", "Microphone"); !hasCapabilityIssue(issues, func() string {
				if name == "missing" {
					return "capture_executable_missing"
				}
				return "capture_executable_unsafe"
			}()) {
				t.Fatalf("issues=%+v", issues)
			}
		})
	}
	directoryExecutable := filepath.Join(t.TempDir(), "ffmpeg.exe")
	if err := os.Mkdir(directoryExecutable, 0o750); err != nil {
		t.Fatal(err)
	}
	if issues := LocalConfigurationIssues(directoryExecutable, "dshow", "Microphone"); !hasCapabilityIssue(issues, "capture_executable_unsafe") {
		t.Fatalf("directory issues=%+v", issues)
	}
	for _, driver := range []string{"pulse", "alsa"} {
		if issues := LocalConfigurationIssues(valid, driver, "Microphone"); !hasCapabilityIssue(issues, "capture_driver_unsupported_on_platform") {
			t.Fatalf("driver=%s issues=%+v", driver, issues)
		}
	}
	for _, device := range []string{"", "audio=Microphone", "VIDEO=Camera", "bad\x00device", "bad\rdevice", "bad\ndevice", "bad\tdevice"} {
		if issues := LocalConfigurationIssues(valid, "dshow", device); !hasCapabilityIssue(issues, "capture_device_missing") {
			t.Fatalf("device=%q issues=%+v", device, issues)
		}
	}
}

func TestWindowsLocalConfigurationRejectsHardlinkedExecutable(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.exe")
	executable := filepath.Join(dir, "ffmpeg.exe")
	if err := os.WriteFile(source, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, executable); err != nil {
		t.Fatal(err)
	}
	if issues := LocalConfigurationIssues(executable, "dshow", "Microphone"); !hasCapabilityIssue(issues, "capture_executable_unsafe") {
		t.Fatalf("hardlinked executable issues=%+v", issues)
	}
}

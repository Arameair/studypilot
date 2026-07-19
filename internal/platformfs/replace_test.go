package platformfs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceExistingFileRepeatedly(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "runtime.json")
	for i := 0; i < 50; i++ {
		temporary := filepath.Join(root, fmt.Sprintf(".runtime-%02d.tmp", i))
		want := []byte(fmt.Sprintf(`{"revision":%d}`, i))
		if err := os.WriteFile(temporary, want, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := Replace(temporary, target); err != nil {
			t.Fatalf("replace %d: %v", i, err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("replace %d content=%q want=%q", i, got, want)
		}
		if _, err := os.Stat(temporary); !os.IsNotExist(err) {
			t.Fatalf("temporary source remained after replace %d: %v", i, err)
		}
	}
}

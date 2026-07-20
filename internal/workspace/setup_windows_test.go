//go:build windows

package workspace

import "testing"

func TestWindowsSetupPathsSupportSpacesAndUnicode(t *testing.T) {
	for _, root := range []string{
		`C:\Users\Example User\Documents\vaults`,
		`C:\Users\學習 User\Documents\vaults`,
	} {
		paths, err := PathsFromRoot(root)
		if err != nil {
			t.Fatalf("PathsFromRoot(%q) error = %v", root, err)
		}
		if paths.Root != root || paths.Private == paths.Portfolio {
			t.Fatalf("PathsFromRoot(%q) = %+v", root, paths)
		}
	}
}

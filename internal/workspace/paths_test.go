package workspace

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() error = %v", err)
	}

	root := filepath.Join(home, "Documents", "StudyPilot")
	want := Paths{
		Root:      root,
		Private:   filepath.Join(root, privateVaultName),
		Portfolio: filepath.Join(root, portfolioName),
	}
	if paths != want {
		t.Fatalf("DefaultPaths() = %#v, want %#v", paths, want)
	}
}

func TestPathsFromRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	absolute := filepath.Join(t.TempDir(), "studypilot")

	tests := []struct {
		name     string
		root     string
		wantRoot string
		wantErr  bool
	}{
		{name: "absolute", root: absolute, wantRoot: absolute},
		{name: "home relative", root: "~/Documents/StudyPilot", wantRoot: filepath.Join(home, "Documents", "StudyPilot")},
		{name: "relative", root: filepath.Join("relative", "path")},
		{name: "empty", root: "", wantErr: true},
		{name: "other user's home", root: "~someone/Documents/StudyPilot", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, err := PathsFromRoot(test.root)
			if test.wantErr {
				if err == nil {
					t.Fatal("PathsFromRoot() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("PathsFromRoot() error = %v", err)
			}

			wantRoot := test.wantRoot
			if wantRoot == "" {
				wantRoot, err = filepath.Abs(filepath.Clean(test.root))
				if err != nil {
					t.Fatalf("resolve expected root: %v", err)
				}
			}
			if paths.Root != wantRoot {
				t.Errorf("Root = %q, want %q", paths.Root, wantRoot)
			}
			if paths.Private != filepath.Join(wantRoot, privateVaultName) {
				t.Errorf("Private = %q", paths.Private)
			}
			if paths.Portfolio != filepath.Join(wantRoot, portfolioName) {
				t.Errorf("Portfolio = %q", paths.Portfolio)
			}
		})
	}
}

func TestPathsValidateRejectsInvalidLayouts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	private := filepath.Join(root, privateVaultName)
	portfolio := filepath.Join(root, portfolioName)

	tests := []struct {
		name  string
		paths Paths
	}{
		{name: "empty root", paths: Paths{Private: private, Portfolio: portfolio}},
		{name: "empty private", paths: Paths{Root: root, Portfolio: portfolio}},
		{name: "empty portfolio", paths: Paths{Root: root, Private: private}},
		{name: "relative root", paths: Paths{Root: "workspace", Private: private, Portfolio: portfolio}},
		{name: "relative private", paths: Paths{Root: root, Private: privateVaultName, Portfolio: portfolio}},
		{name: "relative portfolio", paths: Paths{Root: root, Private: private, Portfolio: portfolioName}},
		{name: "identical vaults", paths: Paths{Root: root, Private: private, Portfolio: private}},
		{name: "private outside root", paths: Paths{Root: root, Private: filepath.Join(t.TempDir(), "private"), Portfolio: portfolio}},
		{name: "portfolio outside root", paths: Paths{Root: root, Private: private, Portfolio: filepath.Join(t.TempDir(), "portfolio")}},
		{name: "private inside portfolio", paths: Paths{Root: root, Private: filepath.Join(portfolio, "private"), Portfolio: portfolio}},
		{name: "portfolio inside private", paths: Paths{Root: root, Private: private, Portfolio: filepath.Join(private, "portfolio")}},
		{name: "root equals private", paths: Paths{Root: root, Private: root, Portfolio: portfolio}},
		{name: "root equals portfolio", paths: Paths{Root: root, Private: private, Portfolio: root}},
		{name: "parent traversal", paths: Paths{Root: root, Private: root + string(filepath.Separator) + ".." + string(filepath.Separator) + "private", Portfolio: portfolio}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.paths.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestPathsValidateAcceptsSeparatedVaults(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	paths := Paths{
		Root:      root,
		Private:   filepath.Join(root, privateVaultName),
		Portfolio: filepath.Join(root, portfolioName),
	}
	if err := paths.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

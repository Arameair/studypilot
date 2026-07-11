package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var fixtureContracts = []struct {
	name     string
	root     string
	expected []string
}{
	{
		name: "private vault",
		root: filepath.Join("..", "..", "testdata", "expected-private-vault"),
		expected: []string{
			".gitignore",
			".obsidian/", ".obsidian/.gitkeep",
			".studypilot/", ".studypilot/.gitkeep",
			"00 Dashboard/", "00 Dashboard/.gitkeep",
			"01 Courses/", "01 Courses/.gitkeep",
			"02 Study/", "02 Study/.gitkeep",
			"03 Draft Knowledge/", "03 Draft Knowledge/.gitkeep",
			"04 Personal/", "04 Personal/.gitkeep",
			"README.md",
			"Templates/", "Templates/.gitkeep",
		},
	},
	{
		name: "public portfolio",
		root: filepath.Join("..", "..", "testdata", "expected-public-portfolio"),
		expected: []string{
			".gitignore",
			".obsidian/", ".obsidian/.gitkeep",
			"00 Portfolio Index/", "00 Portfolio Index/.gitkeep",
			"01 Projects/", "01 Projects/.gitkeep",
			"02 Procedures/", "02 Procedures/.gitkeep",
			"03 Troubleshooting/", "03 Troubleshooting/.gitkeep",
			"04 Concepts/", "04 Concepts/.gitkeep",
			"05 Labs/", "05 Labs/.gitkeep",
			"06 Professional Development/", "06 Professional Development/.gitkeep",
			"PUBLICATION-POLICY.md",
			"README.md",
			"Templates/", "Templates/.gitkeep",
			"assets/", "assets/.gitkeep",
		},
	},
}

func TestFixtureContracts(t *testing.T) {
	for _, contract := range fixtureContracts {
		contract := contract
		t.Run(contract.name, func(t *testing.T) {
			actual := walkFixture(t, contract.root)
			expected := slices.Clone(contract.expected)
			slices.Sort(expected)

			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("fixture paths differ\nactual:   %q\nexpected: %q", actual, expected)
			}
		})
	}
}

func TestPrivateFixtureWarning(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "expected-private-vault")
	warning, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read private warning: %v", err)
	}

	text := strings.ToLower(string(warning))
	for _, required := range []string{"copyrighted paid-course", "must never be made public"} {
		if !strings.Contains(text, required) {
			t.Errorf("private warning does not contain %q", required)
		}
	}
}

func TestPublicFixturePublicationPolicy(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "expected-public-portfolio")
	policy, err := os.ReadFile(filepath.Join(root, "PUBLICATION-POLICY.md"))
	if err != nil {
		t.Fatalf("read publication policy: %v", err)
	}

	text := strings.ToLower(string(policy))
	for _, required := range []string{"explicit human approval", "never copy transcripts"} {
		if !strings.Contains(text, required) {
			t.Errorf("publication policy does not contain %q", required)
		}
	}
}

func TestPublicFixtureHasNoTranscriptOrRecordingDirectories(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "expected-public-portfolio")
	prohibited := map[string]bool{
		"transcript":  true,
		"transcripts": true,
		"recording":   true,
		"recordings":  true,
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && prohibited[strings.ToLower(entry.Name())] {
			t.Errorf("public fixture contains prohibited directory %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk public fixture: %v", err)
	}
}

func walkFixture(t *testing.T, root string) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			relative += "/"
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture %q: %v", root, err)
	}

	slices.Sort(paths)
	return paths
}

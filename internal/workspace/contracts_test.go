package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVaultContractsAreValid(t *testing.T) {
	tests := []struct {
		name     string
		contract VaultContract
	}{
		{name: "private", contract: PrivateVaultContract()},
		{name: "portfolio", contract: PublicPortfolioContract()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.contract.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestPrivateContractContentSafety(t *testing.T) {
	contract := PrivateVaultContract()
	files := requiredFilesByPath(contract)

	readme := strings.ToLower(files["README.md"])
	for _, required := range []string{
		"permanently private",
		"paid-course transcripts",
		"paid-course assets",
		"must never be made public",
		"public material must be created through a separate review process",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("private contract README does not contain %q", required)
		}
	}

	gitignore := strings.ToLower(files[".gitignore"])
	if strings.Contains(gitignore, "*.md") || strings.Contains(gitignore, ".md") {
		t.Error("private contract .gitignore must not ignore Markdown content")
	}
}

func TestVaultContractRejectsMalformedContracts(t *testing.T) {
	validPrivate := PrivateVaultContract()
	validPublic := PublicPortfolioContract()

	tests := []struct {
		name     string
		contract VaultContract
	}{
		{name: "unknown kind", contract: VaultContract{Kind: "unknown", Directories: []string{".obsidian"}}},
		{name: "empty directory", contract: withDirectory(validPrivate, "")},
		{name: "absolute directory", contract: withDirectory(validPrivate, string(filepath.Separator)+"tmp")},
		{name: "parent traversal directory", contract: withDirectory(validPrivate, "notes/../private")},
		{name: "duplicate directory", contract: withDirectory(validPrivate, validPrivate.Directories[0])},
		{name: "empty file", contract: withFile(validPrivate, RequiredFile{})},
		{name: "duplicate file", contract: withFile(validPrivate, validPrivate.Files[0])},
		{name: "directory and file collision", contract: withFile(validPrivate, RequiredFile{Path: validPrivate.Directories[0]})},
		{name: "missing obsidian", contract: withoutDirectory(validPrivate, ".obsidian")},
		{name: "private missing studypilot", contract: withoutDirectory(validPrivate, ".studypilot")},
		{name: "private missing readme", contract: withoutFile(validPrivate, "README.md")},
		{name: "public contains studypilot", contract: withDirectory(validPublic, ".studypilot")},
		{name: "public missing policy", contract: withoutFile(validPublic, "PUBLICATION-POLICY.md")},
		{name: "public transcript directory", contract: withDirectory(validPublic, "archive/TrAnScRiPtS")},
		{name: "public recording directory", contract: withDirectory(validPublic, "Recordings")},
		{name: "public audio directory", contract: withDirectory(validPublic, "AUDIO")},
		{name: "public course materials directory", contract: withDirectory(validPublic, "Course Materials")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.contract.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestContractFixturesMatchMachineReadableContracts(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		contract VaultContract
	}{
		{name: "private vault", root: filepath.Join("..", "..", "testdata", "expected-private-vault"), contract: PrivateVaultContract()},
		{name: "public portfolio", root: filepath.Join("..", "..", "testdata", "expected-public-portfolio"), contract: PublicPortfolioContract()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := walkFixture(t, test.root)
			expected := expectedFixturePaths(test.contract)
			if !slices.Equal(actual, expected) {
				t.Fatalf("fixture paths differ\nactual:   %q\nexpected: %q", actual, expected)
			}

			for _, file := range test.contract.Files {
				contents, err := os.ReadFile(filepath.Join(test.root, filepath.FromSlash(file.Path)))
				if err != nil {
					t.Fatalf("read required file %q: %v", file.Path, err)
				}
				if strings.TrimSpace(string(contents)) == "" {
					t.Errorf("required file %q is empty", file.Path)
				}
			}
		})
	}
}

func TestPrivateFixtureWarning(t *testing.T) {
	warning, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expected-private-vault", "README.md"))
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
	policy, err := os.ReadFile(filepath.Join("..", "..", "testdata", "expected-public-portfolio", "PUBLICATION-POLICY.md"))
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

func expectedFixturePaths(contract VaultContract) []string {
	paths := make([]string, 0, len(contract.Directories)*2+len(contract.Files))
	for _, directory := range contract.Directories {
		directory = filepath.ToSlash(filepath.Clean(directory))
		paths = append(paths, directory+"/", directory+"/.gitkeep")
	}
	for _, file := range contract.Files {
		paths = append(paths, filepath.ToSlash(filepath.Clean(file.Path)))
	}
	slices.Sort(paths)
	return paths
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

func withDirectory(contract VaultContract, path string) VaultContract {
	contract.Directories = append(slices.Clone(contract.Directories), path)
	contract.Files = slices.Clone(contract.Files)
	return contract
}

func withoutDirectory(contract VaultContract, path string) VaultContract {
	contract.Directories = slices.DeleteFunc(slices.Clone(contract.Directories), func(candidate string) bool { return candidate == path })
	contract.Files = slices.Clone(contract.Files)
	return contract
}

func withFile(contract VaultContract, file RequiredFile) VaultContract {
	contract.Directories = slices.Clone(contract.Directories)
	contract.Files = append(slices.Clone(contract.Files), file)
	return contract
}

func withoutFile(contract VaultContract, path string) VaultContract {
	contract.Directories = slices.Clone(contract.Directories)
	contract.Files = slices.DeleteFunc(slices.Clone(contract.Files), func(candidate RequiredFile) bool { return candidate.Path == path })
	return contract
}

func requiredFilesByPath(contract VaultContract) map[string]string {
	files := make(map[string]string, len(contract.Files))
	for _, file := range contract.Files {
		files[file.Path] = file.Content
	}
	return files
}

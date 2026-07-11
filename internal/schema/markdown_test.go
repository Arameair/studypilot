package schema

import (
	"bytes"
	"errors"
	"testing"
)

func TestMarkdownPreservesUnknownFieldsUnicodeAndNewlines(t *testing.T) {
	input := []byte("---\r\nschema_version: 1\r\ncustom: café 日本語\r\n---\r\n# User heading\r\nhandwritten  text\r\n")
	document, err := ParseMarkdown(input, []string{"schema_version", "status"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetManagedField("status", "verified"); err != nil {
		t.Fatal(err)
	}
	output := document.Bytes()
	if !bytes.Contains(output, []byte("custom: café 日本語\r\n")) || !bytes.Contains(output, []byte("# User heading\r\nhandwritten  text\r\n")) || !bytes.HasSuffix(output, []byte("\r\n")) {
		t.Fatalf("preservation failed:\n%s", output)
	}
}

func TestMarkdownRenameAndDuplicateManagedKeys(t *testing.T) {
	document, err := ParseMarkdown([]byte("---\nold: value\ncustom: keep\n---\nbody"), []string{"old", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.RenameManagedField("old", "new"); err != nil {
		t.Fatal(err)
	}
	want := "---\nnew: value\ncustom: keep\n---\nbody"
	if string(document.Bytes()) != want {
		t.Fatalf("got %q want %q", document.Bytes(), want)
	}
	_, err = ParseMarkdown([]byte("---\nstatus: one\nstatus: two\n---\n"), []string{"status"}, nil)
	if !errors.Is(err, ErrDuplicateManagedKey) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestMarkdownRejectsMalformedFrontmatter(t *testing.T) {
	for _, input := range []string{"body only", "---\nstatus: x\n"} {
		if _, err := ParseMarkdown([]byte(input), []string{"status"}, nil); !errors.Is(err, ErrMalformedDocument) {
			t.Fatalf("input %q: %v", input, err)
		}
	}
}

func TestManagedRegionsPreserveUserContentAndAreIdempotent(t *testing.T) {
	input := []byte("---\nschema_version: 1\n---\nBefore user\n<!-- studypilot:begin summary -->\nold\n<!-- studypilot:end summary -->\nAfter user\n")
	document, err := ParseMarkdown(input, []string{"schema_version"}, []string{"summary"})
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetManagedRegion("summary", []byte("new")); err != nil {
		t.Fatal(err)
	}
	once := document.Bytes()
	again, err := ParseMarkdown(once, []string{"schema_version"}, []string{"summary"})
	if err != nil {
		t.Fatal(err)
	}
	if err := again.SetManagedRegion("summary", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, again.Bytes()) {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, again.Bytes())
	}
	if !bytes.Contains(once, []byte("Before user\n")) || !bytes.Contains(once, []byte("After user\n")) {
		t.Fatal("user content changed")
	}
}

func TestManagedRegionAddAndMalformedMarkers(t *testing.T) {
	document, err := ParseMarkdown([]byte("---\nschema_version: 1\n---\nuser text\n"), []string{"schema_version"}, []string{"summary"})
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetManagedRegion("summary", nil); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(document.Bytes(), []byte("studypilot:begin summary")) != 1 {
		t.Fatal("region not added exactly once")
	}
	bad := []string{
		"---\nschema_version: 1\n---\n<!-- studypilot:begin summary -->\n",
		"---\nschema_version: 1\n---\n<!-- studypilot:end summary -->\n",
		"---\nschema_version: 1\n---\n<!-- studypilot:begin summary -->\n<!-- studypilot:begin summary -->\n<!-- studypilot:end summary -->\n<!-- studypilot:end summary -->\n",
	}
	for _, input := range bad {
		if _, err := ParseMarkdown([]byte(input), []string{"schema_version"}, []string{"summary"}); !errors.Is(err, ErrMalformedManagedRegion) {
			t.Fatalf("expected marker error: %v", err)
		}
	}
}

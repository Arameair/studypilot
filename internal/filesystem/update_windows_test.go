//go:build windows

package filesystem

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestWindowsRuntimeStyleMutationRepeatedlyReplacesExistingFile(t *testing.T) {
	_, authority, target, managed := courseMutationSetup(t)
	executor := NewMutationExecutor()
	for revision := 1; revision <= 25; revision++ {
		content := []byte(fmt.Sprintf(`{"revision":%d}`, revision))
		mutation, err := NewMutation(authority, target, managed.ExpectedState(), content, 0o640)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = executor.Apply(context.Background(), mutation); err != nil {
			t.Fatalf("revision %d: %v", revision, err)
		}
		managed, err = executor.Read(context.Background(), authority, target)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != string(content) {
			t.Fatalf("revision %d content=%q err=%v", revision, got, err)
		}
	}
}

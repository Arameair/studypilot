package application

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestGUIReadModelsUseAuthoritativePathFreeState(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), sequentialID())
	root := testRoot(t)
	initWorkspace(t, service, root)
	if _, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "GUI Course"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateModule(context.Background(), ModuleCreateRequest{Root: root, CourseRef: "GUI Course", Number: 1, Name: "GUI Module"}); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateSession(context.Background(), CreateSessionRequest{Root: root, CourseRef: "GUI Course", ModuleRef: "GUI Module", Title: "GUI Session"})
	if err != nil {
		t.Fatal(err)
	}
	courses, err := service.ListCourses(context.Background(), ListCoursesRequest{Root: root})
	if err != nil || len(courses) != 1 || courses[0].Modules != 1 {
		t.Fatalf("courses=%+v err=%v", courses, err)
	}
	modules, err := service.ListModules(context.Background(), ListModulesRequest{Root: root, CourseRef: courses[0].ID})
	if err != nil || len(modules) != 1 || modules[0].Sessions != 1 {
		t.Fatalf("modules=%+v err=%v", modules, err)
	}
	workspace, err := service.GetSessionWorkspace(context.Background(), SessionWorkspaceRequest{Root: root, CourseRef: courses[0].ID, ModuleRef: modules[0].ID, SessionRef: created.ID})
	if err != nil || workspace.Session.ID != created.ID || !workspace.Controls.StartSession || workspace.Controls.StartCapture || workspace.ArtifactRevision != 0 {
		t.Fatalf("workspace=%+v err=%v", workspace, err)
	}
	if workspace.Artifacts == nil || workspace.ArtifactIssues == nil || workspace.Transcription.RuntimeStates == nil {
		t.Fatal("GUI collections must be stable empty arrays")
	}
	dashboard, err := service.GetDashboard(context.Background(), DashboardRequest{Root: root})
	if err != nil || dashboard.Courses != 1 || dashboard.Modules != 1 || len(dashboard.UnfinishedSessions) != 1 {
		t.Fatalf("dashboard=%+v err=%v", dashboard, err)
	}
	encoded, _ := json.Marshal(struct {
		Courses   []CourseSummary
		Modules   []ModuleSummary
		Workspace SessionWorkspaceResult
		Dashboard DashboardResult
	}{courses, modules, workspace, dashboard})
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), "transcript body") {
		t.Fatal("GUI application model leaked private data")
	}
}

func TestConcurrentDashboardReads(t *testing.T) {
	service := newTestService(t, fixedClock(fixedDate), sequentialID())
	root := testRoot(t)
	initWorkspace(t, service, root)
	if _, err := service.CreateCourse(context.Background(), CourseCreateRequest{Root: root, Name: "Read Course"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateModule(context.Background(), ModuleCreateRequest{Root: root, CourseRef: "Read Course", Number: 1, Name: "Read Module"}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.GetDashboard(context.Background(), DashboardRequest{Root: root})
			if err != nil || result.Courses != 1 || result.Modules != 1 {
				t.Errorf("dashboard=%+v err=%v", result, err)
			}
		}()
	}
	wait.Wait()
}

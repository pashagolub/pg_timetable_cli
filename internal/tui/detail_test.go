package tui

import (
	"strings"
	"testing"

	"github.com/cybertec-postgresql/pg_timetable_cli/internal/client"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleChainDetail() (*client.ChainListItem, []client.ChainTask) {
	ch := &client.ChainListItem{}
	ch.ChainID = 7
	ch.ChainName = "nightly"
	ch.Live = true
	ch.MaxInstances = 1
	ch.RunAt = "0 2 * * *"
	ch.ClientName = "w1"

	tasks := []client.ChainTask{
		{TaskID: 1, TaskName: "extract", Kind: "SQL", Command: "SELECT 1", Timeout: 1000},
		{TaskID: 2, TaskName: "load", Kind: "PROGRAM", Command: "echo\nhi", IgnoreError: true, Autonomous: true},
	}
	return ch, tasks
}

func sampleRuns() []client.RunSummary {
	return []client.RunSummary{
		{Txid: 1001, StartedAt: "2026-06-23 02:00:00", DurationMS: 500, Status: "success", TotalTasks: 2},
		{Txid: 1002, StartedAt: "2026-06-23 03:00:00", DurationMS: 800, Status: "failed", TotalTasks: 2, FailedTasks: 1},
	}
}

func loadedDetailView() *detailView {
	v := newDetailView(nil, newStyles(false), "7", "nightly")
	v.SetSize(140, 30)
	ch, tasks := sampleChainDetail()
	uv, _ := v.Update(detailLoadedMsg{chain: ch, tasks: tasks})
	v = uv.(*detailView)
	uv, _ = v.Update(runsLoadedMsg{runs: sampleRuns()})
	return uv.(*detailView)
}

func TestDetailLoadsChainAndRuns(t *testing.T) {
	v := loadedDetailView()
	if v.chain == nil || v.chain.ChainID != 7 {
		t.Fatal("chain not loaded")
	}
	if len(v.tasks) != 2 || len(v.runs) != 2 {
		t.Fatalf("tasks=%d runs=%d, want 2/2", len(v.tasks), len(v.runs))
	}
	out := v.Body(140, 28)
	for _, want := range []string{"nightly", "extract", "load", "Tasks", "Recent runs", "1001", "1002"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail body missing %q", want)
		}
	}
}

func TestDetailFocusSwitchAndMove(t *testing.T) {
	v := loadedDetailView()
	if v.focus != paneTasks {
		t.Fatal("default focus should be tasks")
	}
	// Move within tasks.
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.taskCursor != 1 {
		t.Fatalf("taskCursor = %d, want 1", v.taskCursor)
	}
	// Tab to runs.
	v.Update(tea.KeyMsg{Type: tea.KeyTab})
	if v.focus != paneRuns {
		t.Fatal("tab did not switch to runs")
	}
	// Down moves the run cursor, not the task cursor.
	v.Update(tea.KeyMsg{Type: tea.KeyDown})
	if v.runCursor != 1 || v.taskCursor != 1 {
		t.Fatalf("runCursor=%d taskCursor=%d, want 1/1", v.runCursor, v.taskCursor)
	}
}

func TestDetailEnterOpensRunOnlyInRunsPane(t *testing.T) {
	v := loadedDetailView()

	// In tasks pane: Enter does nothing.
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter in tasks pane should not push a view")
	}

	// Switch to runs, select the failed run, Enter pushes run-detail.
	v.Update(tea.KeyMsg{Type: tea.KeyTab})
	v.Update(tea.KeyMsg{Type: tea.KeyDown}) // runCursor -> 1 (txid 1002)
	_, cmd = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in runs pane returned no command")
	}
	msg := cmd()
	pv, ok := msg.(pushViewMsg)
	if !ok {
		t.Fatalf("expected pushViewMsg, got %T", msg)
	}
	rd, ok := pv.v.(*runDetailView)
	if !ok {
		t.Fatalf("pushed view = %T, want *runDetailView", pv.v)
	}
	if rd.run.Txid != 1002 {
		t.Fatalf("run-detail txid = %d, want 1002", rd.run.Txid)
	}
}

func TestDetailErrorSurfacesErrMsg(t *testing.T) {
	v := newDetailView(nil, newStyles(false), "7", "nightly")
	_, cmd := v.Update(detailLoadedMsg{err: errTest("boom")})
	if cmd == nil {
		t.Fatal("no command on error")
	}
	if _, ok := cmd().(errMsg); !ok {
		t.Fatal("expected errMsg")
	}
}

func TestRunStatusLevel(t *testing.T) {
	cases := map[string]string{
		"success": "OK", "failed": "FAIL", "running": "RUNNING", "weird": "OK",
	}
	for in, want := range cases {
		if got := runStatusLevel(in); got != want {
			t.Fatalf("runStatusLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOneLineCollapsesWhitespace(t *testing.T) {
	if got := oneLine("a\n  b\t c"); got != "a b c" {
		t.Fatalf("oneLine = %q, want 'a b c'", got)
	}
}

func TestClampHelper(t *testing.T) {
	if got := clamp(5, 0); got != 0 {
		t.Fatalf("clamp empty = %d, want 0", got)
	}
	if got := clamp(-3, 4); got != 0 {
		t.Fatalf("clamp negative = %d, want 0", got)
	}
	if got := clamp(9, 4); got != 3 {
		t.Fatalf("clamp overflow = %d, want 3", got)
	}
}

// TestDetailSecretsLine verifies the chain header names every referenced
// secret once and marks the ones that will not resolve at execution time.
func TestDetailSecretsLine(t *testing.T) {
	v := loadedDetailView()
	uv, _ := v.Update(secretRefsLoadedMsg{refs: []client.SecretRef{
		{SecretName: "db_pass", TaskID: 1, Location: "connection", Resolvable: true},
		{SecretName: "smtp", TaskID: 2, Location: "parameter", Note: "no timetable.secret row"},
		{SecretName: "smtp", TaskID: 2, Location: "command", Note: "no timetable.secret row"},
	}})
	v = uv.(*detailView)

	out := v.Body(140, 28)
	if !strings.Contains(out, "secrets ") {
		t.Fatal("detail body missing the secrets line")
	}
	if !strings.Contains(out, "db_pass") || !strings.Contains(out, "!smtp") {
		t.Fatalf("secrets line must list db_pass and flag !smtp, got:\n%s", out)
	}
	if strings.Contains(out, "!db_pass") {
		t.Fatal("a resolvable secret must not be flagged")
	}
	if got := strings.Count(v.secretsLine(), "smtp"); got != 1 {
		t.Fatalf("smtp listed %d times, want once however many tasks reference it", got)
	}
}

// TestDetailSecretsLine_OmittedWithoutRefs verifies chains without references
// keep the two-line header, so the panes below are laid out at full height.
func TestDetailSecretsLine_OmittedWithoutRefs(t *testing.T) {
	v := loadedDetailView()
	if got := v.secretsLine(); got != "" {
		t.Fatalf("secrets line = %q, want empty", got)
	}
	if got := headerLines(v.secretRefs); got != 2 {
		t.Fatalf("headerLines = %d, want 2", got)
	}
	if got := headerLines([]client.SecretRef{{SecretName: "a"}}); got != 3 {
		t.Fatalf("headerLines with refs = %d, want 3", got)
	}
}

// TestDetailSecretRefsErrorIsNotFatal verifies a failed reference scan leaves
// the chain view intact: fetchSecretRefs reports no references rather than an
// error, so a database that cannot answer still renders the chain.
func TestDetailSecretRefsErrorIsNotFatal(t *testing.T) {
	v := loadedDetailView()
	uv, cmd := v.Update(secretRefsLoadedMsg{})
	v = uv.(*detailView)
	if cmd != nil {
		t.Fatal("an empty secret-ref result must not trigger a follow-up command")
	}
	if out := v.Body(140, 28); !strings.Contains(out, "nightly") {
		t.Fatal("chain header lost after an empty secret-ref result")
	}
}

package communitytool

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/internal/model"
)

func TestDefinitionsIncludesCodeGraph(t *testing.T) {
	def, ok := DefinitionFor(model.CommunityToolCodeGraph)
	if !ok {
		t.Fatal("CodeGraph definition not found")
	}
	if def.PackageName != "@colbymchenry/codegraph@latest" || def.CommandName != "codegraph" {
		t.Fatalf("CodeGraph definition = %#v", def)
	}
}

func TestCodeGraphCommands(t *testing.T) {
	want := [][]string{
		{"npm", "install", "-g", "@colbymchenry/codegraph@latest"},
		{"codegraph", "install", "--yes"},
	}
	if got := CodeGraphCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("CodeGraphCommands() = %#v, want %#v", got, want)
	}
	for _, command := range CodeGraphCommands() {
		if strings.Contains(strings.Join(command, " "), "codegraph init") {
			t.Fatalf("CodeGraphCommands() includes project init command: %#v", command)
		}
	}
}

func TestCodeGraphGuidanceContainsLazyInitAndUsageRules(t *testing.T) {
	guidance := CodeGraphGuidanceMarkdown()
	for _, want := range []string{
		"use CodeGraph before broad filesystem searches",
		"hard ordering rule",
		"git rev-parse --show-toplevel || pwd",
		"Do not initialize CodeGraph in `$HOME`, temporary directories, or non-project folders",
		"<project-root>/.codegraph/",
		"before any broad Read/Glob/Grep filesystem exploration",
		"immediately run `codegraph init <project-root>`",
		"codegraph init <project-root>",
		"codegraph_explore",
		"Do not fall back just because `.codegraph/` is missing",
		"missing index is the trigger to lazy-initialize",
		"Only fall back to normal filesystem tools after CodeGraph init or CodeGraph use fails",
		"Broad Read/Glob/Grep exploration before this CodeGraph check is explicitly discouraged",
	} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("CodeGraphGuidanceMarkdown() missing %q:\n%s", want, guidance)
		}
	}
}

func TestCodeGraphGuidanceInjectsForRepresentativeAgents(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{}`)
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), `{}`)
	mustWrite(t, filepath.Join(home, ".pi", "agent", "settings.json"), `{}`)

	installed := false
	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", home, RunnerFunc(func(string, ...string) error {
		installed = true
		return nil
	}), DetectorFunc(func(string) (string, error) {
		if installed {
			return "/bin/codegraph", nil
		}
		return "", errors.New("not found")
	}))
	if err != nil {
		t.Fatalf("InstallWithHome() error = %v", err)
	}
	if result.StatusAfter == nil {
		t.Fatal("StatusAfter = nil")
	}

	for _, path := range []string{
		filepath.Join(home, ".config", "opencode", "AGENTS.md"),
		filepath.Join(home, ".claude", "CLAUDE.md"),
		filepath.Join(home, ".pi", "agent", "APPEND_SYSTEM.md"),
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		text := string(content)
		if !strings.Contains(text, "<!-- gentle-ai:codegraph-guidance -->") || !strings.Contains(text, "codegraph init <project-root>") {
			t.Fatalf("%q missing CodeGraph guidance:\n%s", path, text)
		}
	}
	for _, path := range CodeGraphGuidancePaths(home) {
		if strings.Contains(path, "node_modules") || strings.Contains(path, string(filepath.Separator)+"agents"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+"chains"+string(filepath.Separator)) {
			t.Fatalf("guidance mutated forbidden package path %q", path)
		}
	}
}

func TestUnselectedCodeGraphDoesNotInjectGuidance(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{}`)

	result, err := InjectCodeGraphGuidanceIfSelected(home, nil)
	if err != nil {
		t.Fatalf("InjectCodeGraphGuidanceIfSelected() error = %v", err)
	}
	if result.Changed || len(result.Files) != 0 {
		t.Fatalf("result = %#v, want no-op for unselected CodeGraph", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("OpenCode AGENTS.md should not exist when CodeGraph is unselected; stat err = %v", err)
	}
}

func TestInstallRunsCommandsAndReturnsLazyProjectIndexManualAction(t *testing.T) {
	var ran []string
	installed := false
	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", t.TempDir(), RunnerFunc(func(name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...)...)
		installed = true
		return nil
	}), DetectorFunc(func(string) (string, error) {
		if installed {
			return "/bin/codegraph", nil
		}
		return "", errors.New("not found")
	}))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(ran) == 0 {
		t.Fatal("expected commands to run")
	}
	if len(result.ManualActions) != 1 {
		t.Fatalf("ManualActions = %#v, want one lazy project index instruction", result.ManualActions)
	}
	action := result.ManualActions[0]
	for _, want := range []string{"CodeGraph CLI was installed", "agents were connected", "Project indexes will be created automatically"} {
		if !strings.Contains(action, want) {
			t.Fatalf("ManualActions[0] = %q, want %q", action, want)
		}
	}
	if strings.Contains(action, "codegraph init") {
		t.Fatalf("ManualActions[0] = %q, should not instruct immediate project init", action)
	}
}

func TestDetectStatusReportsCLIAndPerAgentWiring(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "mcp", "codegraph.json"), `{"command":"codegraph"}`)
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{}`)

	status := DetectStatus(model.CommunityToolCodeGraph, home, DetectorFunc(func(name string) (string, error) {
		if name != "codegraph" {
			t.Fatalf("LookPath(%q), want codegraph", name)
		}
		return "/bin/codegraph", nil
	}))

	if status.CLI != AvailabilityAvailable || status.CLIPath != "/bin/codegraph" {
		t.Fatalf("CLI = (%s, %q), want available /bin/codegraph", status.CLI, status.CLIPath)
	}
	claude := findAgentStatus(t, status, model.AgentClaudeCode)
	if !claude.Detected || !claude.Configured || claude.Status != AgentStatusConfigured {
		t.Fatalf("claude status = %#v, want detected configured", claude)
	}
	opencode := findAgentStatus(t, status, model.AgentOpenCode)
	if !opencode.Detected || opencode.Configured || opencode.Status != AgentStatusMissing {
		t.Fatalf("opencode status = %#v, want detected missing", opencode)
	}
}

func TestDetectStatusReportsMissingCLIThroughMock(t *testing.T) {
	status := DetectStatus(model.CommunityToolCodeGraph, t.TempDir(), DetectorFunc(func(string) (string, error) {
		return "", errors.New("not found")
	}))
	if status.CLI != AvailabilityMissing {
		t.Fatalf("CLI = %s, want missing", status.CLI)
	}
}

func TestDetectStatusReportsPiRuntimeMissingWhenAppendSystemHasNoMarker(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".pi", "agent", "settings.json"), `{}`)

	status := DetectStatus(model.CommunityToolCodeGraph, home, DetectorFunc(func(string) (string, error) {
		return "/bin/codegraph", nil
	}))
	pi := findAgentStatus(t, status, model.AgentPi)
	if !pi.Detected || pi.Configured || pi.Status != AgentStatusMissing {
		t.Fatalf("Pi status = %#v, want detected missing", pi)
	}
	if pi.Path != filepath.Join(home, ".pi", "agent", "APPEND_SYSTEM.md") {
		t.Fatalf("Pi path = %q, want APPEND_SYSTEM.md path", pi.Path)
	}
}

func TestDetectStatusReportsPiRuntimeConfiguredWithAppendSystemMarker(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".pi", "agent", "APPEND_SYSTEM.md"), strings.Join([]string{
		"existing Pi guidance",
		"<!-- gentle-ai:codegraph-guidance -->",
		"CodeGraph guidance with `codegraph init <project-root>`",
		"<!-- /gentle-ai:codegraph-guidance -->",
	}, "\n"))

	status := DetectStatus(model.CommunityToolCodeGraph, home, DetectorFunc(func(string) (string, error) {
		return "/bin/codegraph", nil
	}))
	pi := findAgentStatus(t, status, model.AgentPi)
	if !pi.Detected || !pi.Configured || pi.Status != AgentStatusConfigured {
		t.Fatalf("Pi status = %#v, want detected configured", pi)
	}
}

func TestInjectCodeGraphGuidanceCreatesPiAppendSystemAndPreservesContent(t *testing.T) {
	home := t.TempDir()
	appendSystemPath := filepath.Join(home, ".pi", "agent", "APPEND_SYSTEM.md")
	mustWrite(t, appendSystemPath, "existing Pi instructions\n")

	result, err := InjectCodeGraphGuidance(home)
	if err != nil {
		t.Fatalf("InjectCodeGraphGuidance() error = %v", err)
	}
	if !result.Changed {
		t.Fatalf("InjectCodeGraphGuidance() Changed = false, want true")
	}

	content, err := os.ReadFile(appendSystemPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", appendSystemPath, err)
	}
	text := string(content)
	for _, want := range []string{"existing Pi instructions", "<!-- gentle-ai:codegraph-guidance -->", "codegraph init <project-root>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("APPEND_SYSTEM.md missing %q:\n%s", want, text)
		}
	}
	for _, path := range result.Files {
		if path != appendSystemPath {
			t.Fatalf("InjectCodeGraphGuidance() file = %q, want only %q", path, appendSystemPath)
		}
		if strings.Contains(path, "node_modules") || strings.Contains(path, string(filepath.Separator)+"agents"+string(filepath.Separator)) || strings.Contains(path, string(filepath.Separator)+"chains"+string(filepath.Separator)) {
			t.Fatalf("guidance mutated forbidden package path %q", path)
		}
	}
}

func TestInstallFailsWhenPostInstallContractStillMissing(t *testing.T) {
	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", t.TempDir(), RunnerFunc(func(string, ...string) error {
		return nil
	}), DetectorFunc(func(string) (string, error) {
		return "", errors.New("not found")
	}))
	if err == nil || !strings.Contains(err.Error(), "CLI available") {
		t.Fatalf("InstallWithHome() error = %v, want missing CLI validation", err)
	}
	if result.StatusAfter == nil {
		t.Fatal("StatusAfter = nil, want partial result context")
	}
}

func TestValidateCodeGraphInstallStatusFailsForDetectedMissingAgent(t *testing.T) {
	status := Status{
		Tool: model.CommunityToolCodeGraph,
		CLI:  AvailabilityAvailable,
		Agents: []AgentStatus{
			{Agent: model.AgentOpenCode, Name: "OpenCode", Detected: true, Configured: false},
		},
	}
	err := validateCodeGraphInstallStatus(status)
	if err == nil || !strings.Contains(err.Error(), "OpenCode") {
		t.Fatalf("validateCodeGraphInstallStatus() error = %v, want missing OpenCode", err)
	}
}

func TestInstallSkipsWhenCodeGraphAlreadyReconciled(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "mcp", "codegraph.json"), `{"command":"codegraph"}`)

	calls := 0
	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", home, RunnerFunc(func(string, ...string) error {
		calls++
		return nil
	}), DetectorFunc(func(string) (string, error) {
		return "/bin/codegraph", nil
	}))
	if err != nil {
		t.Fatalf("InstallWithHome() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0 for already reconciled install", calls)
	}
	if result.StatusAfter == nil || !result.StatusAfter.CodeGraphReconcileSatisfied() {
		t.Fatalf("StatusAfter = %#v, want reconciled", result.StatusAfter)
	}
}

func TestInstallRefreshesOldCodeGraphGuidanceMarker(t *testing.T) {
	home := t.TempDir()
	agentsPath := filepath.Join(home, ".config", "opencode", "AGENTS.md")
	mustWrite(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{}`)
	mustWrite(t, agentsPath, strings.Join([]string{
		"user content",
		"<!-- gentle-ai:codegraph-guidance -->",
		"old CodeGraph prompt",
		"<!-- /gentle-ai:codegraph-guidance -->",
	}, "\n"))

	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", home, RunnerFunc(func(string, ...string) error {
		return nil
	}), DetectorFunc(func(string) (string, error) {
		return "/bin/codegraph", nil
	}))
	if err != nil {
		t.Fatalf("InstallWithHome() error = %v", err)
	}
	if len(result.CommandsRun) != 0 {
		t.Fatalf("CommandsRun = %#v, want no install commands for configured CodeGraph", result.CommandsRun)
	}

	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", agentsPath, err)
	}
	text := string(content)
	if strings.Contains(text, "old CodeGraph prompt") {
		t.Fatalf("old guidance was not replaced:\n%s", text)
	}
	if !strings.Contains(text, "immediately run `codegraph init <project-root>`") || !strings.Contains(text, "user content") {
		t.Fatalf("latest guidance/user content missing after refresh:\n%s", text)
	}
}

func TestInstallRepairsMissingCLIWhenAgentMarkerExists(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "mcp", "codegraph.json"), `{"command":"codegraph"}`)

	var commands []string
	installed := false
	result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", home, RunnerFunc(func(name string, args ...string) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		installed = true
		return nil
	}), DetectorFunc(func(string) (string, error) {
		if installed {
			return "/bin/codegraph", nil
		}
		return "", errors.New("not found")
	}))
	if err != nil {
		t.Fatalf("InstallWithHome() error = %v", err)
	}
	want := []string{
		"npm install -g @colbymchenry/codegraph@latest",
		"codegraph install --yes",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	if result.StatusBefore == nil || result.StatusBefore.CLI != AvailabilityMissing {
		t.Fatalf("StatusBefore = %#v, want missing CLI", result.StatusBefore)
	}
}

func TestInstallFailurePaths(t *testing.T) {
	t.Run("nil runner", func(t *testing.T) {
		result, err := Install(model.CommunityToolCodeGraph, "/work/project", nil)
		if err == nil {
			t.Fatal("Install() error = nil, want configured runner error")
		}
		if result.Tool != "" || len(result.CommandsRun) != 0 {
			t.Fatalf("result = %#v, want empty result", result)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		result, err := Install(model.CommunityToolID("missing-tool"), "/work/project", RunnerFunc(func(string, ...string) error { return nil }))
		if err == nil || !strings.Contains(err.Error(), "unknown community tool") {
			t.Fatalf("Install() error = %v, want unknown tool error", err)
		}
		if result.Tool != "" || len(result.CommandsRun) != 0 {
			t.Fatalf("result = %#v, want empty result", result)
		}
	})

	t.Run("command runner failure preserves attempted command", func(t *testing.T) {
		boom := errors.New("npm failed")
		result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", t.TempDir(), RunnerFunc(func(string, ...string) error { return boom }), DetectorFunc(func(string) (string, error) {
			return "", errors.New("not found")
		}))
		if !errors.Is(err, boom) {
			t.Fatalf("Install() error = %v, want wrapped runner error", err)
		}
		if result.Tool != model.CommunityToolCodeGraph {
			t.Fatalf("result tool = %q, want CodeGraph", result.Tool)
		}
		if len(result.CommandsRun) != 1 || !strings.Contains(result.CommandsRun[0], "npm install -g @colbymchenry/codegraph@latest") {
			t.Fatalf("CommandsRun = %#v, want failed CLI install command", result.CommandsRun)
		}
	})

	t.Run("agent wiring failure preserves attempted commands", func(t *testing.T) {
		boom := errors.New("install failed")
		calls := 0
		result, err := InstallWithHome(model.CommunityToolCodeGraph, "/work/project", t.TempDir(), RunnerFunc(func(string, ...string) error {
			calls++
			if calls == 2 {
				return boom
			}
			return nil
		}), DetectorFunc(func(string) (string, error) {
			return "", errors.New("not found")
		}))
		if !errors.Is(err, boom) {
			t.Fatalf("Install() error = %v, want wrapped install error", err)
		}
		if calls != 2 {
			t.Fatalf("runner calls = %d, want 2", calls)
		}
		if len(result.CommandsRun) != 2 {
			t.Fatalf("CommandsRun = %#v, want CLI install and failed agent wiring command", result.CommandsRun)
		}
		got := strings.Join(result.CommandsRun, "\n")
		if !strings.Contains(got, "npm install -g @colbymchenry/codegraph@latest") || !strings.Contains(got, "codegraph install --yes") || strings.Contains(got, "codegraph init") {
			t.Fatalf("CommandsRun = %#v, want CLI install and agent wiring commands only", result.CommandsRun)
		}
	})
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func findAgentStatus(t *testing.T, status Status, id model.AgentID) AgentStatus {
	t.Helper()
	for _, agent := range status.Agents {
		if agent.Agent == id {
			return agent
		}
	}
	t.Fatalf("agent %s not found in %#v", id, status.Agents)
	return AgentStatus{}
}

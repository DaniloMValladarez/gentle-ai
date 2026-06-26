package communitytool

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/internal/agents"
	"github.com/gentleman-programming/gentle-ai/internal/components/filemerge"
	"github.com/gentleman-programming/gentle-ai/internal/model"
)

const codeGraphGuidanceSectionID = "codegraph-guidance"

// GuidanceInjectionResult describes the managed agent-instruction updates made
// when the CodeGraph community tool is enabled.
type GuidanceInjectionResult struct {
	Changed bool
	Files   []string
}

// CodeGraphGuidanceMarkdown is the shared instruction block injected into every
// detected supported agent when the CodeGraph community tool is selected.
func CodeGraphGuidanceMarkdown() string {
	return strings.Join([]string{
		"## CodeGraph",
		"",
		"When answering structural or codebase questions, use CodeGraph before broad filesystem searches. This is a hard ordering rule for repo maps, architecture, call flow, dependencies, symbol references, impact analysis, and “how does X work” questions.",
		"",
		"Required order for structural/codebase questions:",
		"",
		"1. Resolve the project root with `git rev-parse --show-toplevel || pwd`.",
		"2. Confirm the root is a real project/workspace. Do not initialize CodeGraph in `$HOME`, temporary directories, or non-project folders.",
		"3. Check for `<project-root>/.codegraph/` before any broad Read/Glob/Grep filesystem exploration.",
		"4. If `.codegraph/` is missing and CodeGraph is enabled/available, immediately run `codegraph init <project-root>` once, then use the `codegraph_explore` MCP tool or `codegraph explore \"...\"`.",
		"5. Do not fall back just because `.codegraph/` is missing; a missing index is the trigger to lazy-initialize, not a reason to skip CodeGraph.",
		"6. Only fall back to normal filesystem tools after CodeGraph init or CodeGraph use fails, and briefly explain the fallback.",
		"",
		"Broad Read/Glob/Grep exploration before this CodeGraph check is explicitly discouraged for structural/codebase questions.",
	}, "\n")
}

// InjectCodeGraphGuidanceIfSelected is the central community-tool hook for
// agent guidance. It is a no-op unless CodeGraph is among the selected tools.
func InjectCodeGraphGuidanceIfSelected(homeDir string, selected []model.CommunityToolID) (GuidanceInjectionResult, error) {
	if !slices.Contains(selected, model.CommunityToolCodeGraph) {
		return GuidanceInjectionResult{}, nil
	}
	return InjectCodeGraphGuidance(homeDir)
}

// RefreshCodeGraphGuidanceIfConfigured refreshes CodeGraph guidance during
// managed sync flows without requiring persisted Community Tools selection.
//
// It is deliberately conservative: guidance is refreshed only when the
// CodeGraph CLI is available and at least one detected supported agent already
// has CodeGraph wiring or a managed guidance marker. This prevents normal sync
// from introducing CodeGraph prompts for users who never installed/enabled it.
func RefreshCodeGraphGuidanceIfConfigured(homeDir string, detector Detector) (GuidanceInjectionResult, bool, error) {
	if !HasConfiguredCodeGraph(homeDir, detector) {
		return GuidanceInjectionResult{}, false, nil
	}

	result, err := InjectCodeGraphGuidance(homeDir)
	return result, true, err
}

func HasConfiguredCodeGraph(homeDir string, detector Detector) bool {
	status := DetectStatus(model.CommunityToolCodeGraph, homeDir, detector)
	if status.CLI != AvailabilityAvailable {
		return false
	}
	for _, agent := range status.Agents {
		if agent.Detected && agent.Configured {
			return true
		}
	}
	return false
}

// InjectCodeGraphGuidance writes the shared CodeGraph lifecycle guidance to all
// detected supported agents. Detection is intentionally based on existing agent
// config directories so standalone Community Tools setup does not create agent
// configs for tools the user does not use.
func InjectCodeGraphGuidance(homeDir string) (GuidanceInjectionResult, error) {
	reg, err := agents.NewDefaultRegistry()
	if err != nil {
		return GuidanceInjectionResult{}, err
	}

	installed := agents.DiscoverInstalled(reg, homeDir)
	result := GuidanceInjectionResult{}
	for _, installedAgent := range installed {
		adapter, ok := reg.Get(installedAgent.ID)
		if !ok || !isCodeGraphSupportedAgent(installedAgent.ID) || !adapter.SupportsSystemPrompt() {
			continue
		}

		file, changed, err := injectCodeGraphGuidanceForAgent(homeDir, adapter)
		if err != nil {
			return result, fmt.Errorf("inject CodeGraph guidance for %s: %w", installedAgent.ID, err)
		}
		if file == "" {
			continue
		}
		result.Changed = result.Changed || changed
		result.Files = append(result.Files, file)
	}

	return result, nil
}

// CodeGraphGuidancePaths returns the system prompt files that the CodeGraph
// guidance injector may touch for currently detected supported agents.
func CodeGraphGuidancePaths(homeDir string) []string {
	reg, err := agents.NewDefaultRegistry()
	if err != nil {
		return nil
	}

	installed := agents.DiscoverInstalled(reg, homeDir)
	paths := make([]string, 0, len(installed))
	for _, installedAgent := range installed {
		adapter, ok := reg.Get(installedAgent.ID)
		if !ok || !isCodeGraphSupportedAgent(installedAgent.ID) || !adapter.SupportsSystemPrompt() {
			continue
		}
		path := adapter.SystemPromptFile(homeDir)
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func injectCodeGraphGuidanceForAgent(homeDir string, adapter agents.Adapter) (string, bool, error) {
	promptPath := adapter.SystemPromptFile(homeDir)
	if strings.TrimSpace(promptPath) == "" {
		return "", false, nil
	}

	existing, err := readTextFileOrEmpty(promptPath)
	if err != nil {
		return "", false, err
	}
	updated := filemerge.InjectMarkdownSection(existing, codeGraphGuidanceSectionID, CodeGraphGuidanceMarkdown())

	writeResult, err := filemerge.WriteFileAtomic(promptPath, []byte(updated), 0o644)
	if err != nil {
		return "", false, err
	}
	return promptPath, writeResult.Changed, nil
}

func readTextFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", fmt.Errorf("read %q: %w", path, err)
}

func hasCodeGraphGuidance(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, "gentle-ai:"+codeGraphGuidanceSectionID) ||
		(strings.Contains(content, "codegraph") && strings.Contains(content, "codegraph init <project-root>"))
}

func codeGraphGuidancePath(homeDir string, adapter agents.Adapter) string {
	path := adapter.SystemPromptFile(homeDir)
	if strings.TrimSpace(path) != "" {
		return path
	}
	return filepath.Join(adapter.GlobalConfigDir(homeDir), "AGENTS.md")
}

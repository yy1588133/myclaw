package skills

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimeskills "github.com/cexll/agentsdk-go/pkg/runtime/skills"
)

func TestLoadSkills_LoadSingleSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillPath := filepath.Join(root, "writer", skillFileName)
	content := "---\nname: writer\ndescription: writing helper\nkeywords: [write, draft]\n---\n# Writer\nUse this skill for writing tasks.\n"
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	registrations, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(registrations) != 1 {
		t.Fatalf("registration count = %d, want 1", len(registrations))
	}

	registration := registrations[0]
	if registration.Definition.Name != "writer" {
		t.Fatalf("definition name = %q, want writer", registration.Definition.Name)
	}
	if registration.Definition.Description != "writing helper" {
		t.Fatalf("definition description = %q, want writing helper", registration.Definition.Description)
	}

	if len(registration.Definition.Matchers) != 1 {
		t.Fatalf("matchers count = %d, want 1", len(registration.Definition.Matchers))
	}
	match := registration.Definition.Matchers[0].Match(runtimeskills.ActivationContext{Prompt: "please draft a summary"})
	if !match.Matched {
		t.Fatalf("expected keyword matcher to match prompt")
	}

	result, execErr := registration.Handler.Execute(context.Background(), runtimeskills.ActivationContext{})
	if execErr != nil {
		t.Fatalf("execute handler: %v", execErr)
	}

	outputText, ok := result.Output.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", result.Output)
	}
	if outputText != "# Writer\nUse this skill for writing tasks." {
		t.Fatalf("unexpected output: %q", outputText)
	}

	if result.Metadata["system_prompt"] != outputText {
		t.Fatalf("system_prompt metadata mismatch")
	}
	if result.Metadata["source_path"] != skillPath {
		t.Fatalf("source_path metadata mismatch: %v", result.Metadata["source_path"])
	}
}

func TestLoadSkills_DirNotFound(t *testing.T) {
	t.Parallel()

	notFoundDir := filepath.Join(t.TempDir(), "missing")
	registrations, err := LoadSkills(notFoundDir)
	if err != nil {
		t.Fatalf("load skills from missing dir: %v", err)
	}
	if len(registrations) != 0 {
		t.Fatalf("registration count = %d, want 0", len(registrations))
	}
}

func TestLoadSkills_MissingFrontmatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillPath := filepath.Join(root, "broken", skillFileName)
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("# No frontmatter"), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	_, err := LoadSkills(root)
	if err == nil {
		t.Fatalf("expected error for invalid frontmatter")
	}
}

func TestLoadSkills_DuplicateSkillName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	firstPath := filepath.Join(root, "one", skillFileName)
	secondPath := filepath.Join(root, "two", skillFileName)
	firstContent := "---\nname: shared\ndescription: first\nkeywords: [a]\n---\nfirst body\n"
	secondContent := "---\nname: shared\ndescription: second\nkeywords: [b]\n---\nsecond body\n"

	if err := os.MkdirAll(filepath.Dir(firstPath), 0o755); err != nil {
		t.Fatalf("mkdir first skill dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(secondPath), 0o755); err != nil {
		t.Fatalf("mkdir second skill dir: %v", err)
	}
	if err := os.WriteFile(firstPath, []byte(firstContent), 0o600); err != nil {
		t.Fatalf("write first skill file: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte(secondContent), 0o600); err != nil {
		t.Fatalf("write second skill file: %v", err)
	}

	_, err := LoadSkills(root)
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
}

func TestLoadSkills_MultipleSkills(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestSkillFile(t, root, "alpha", "---\nname: alpha\ndescription: alpha helper\nkeywords: [alpha]\n---\nalpha body\n")
	writeTestSkillFile(t, root, "beta", "---\nname: beta\ndescription: beta helper\nkeywords: [beta]\n---\nbeta body\n")
	writeTestSkillFile(t, root, "gamma", "---\nname: gamma\ndescription: gamma helper\nkeywords: [gamma]\n---\ngamma body\n")

	registrations, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(registrations) != 3 {
		t.Fatalf("registration count = %d, want 3", len(registrations))
	}

	wantNames := []string{"alpha", "beta", "gamma"}
	for i, wantName := range wantNames {
		if registrations[i].Definition.Name != wantName {
			t.Fatalf("registration[%d].definition.name = %q, want %q", i, registrations[i].Definition.Name, wantName)
		}
	}
}

func TestLoadSkills_KeywordMatching(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestSkillFile(t, root, "web-search", "---\nname: web-search\ndescription: Search the web\nkeywords:\n  - \" Search \"\n  - WEB\n  - web\n  - find online\n  - \"  \"\n---\n# Web Search\n")

	registrations, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(registrations) != 1 {
		t.Fatalf("registration count = %d, want 1", len(registrations))
	}

	registration := registrations[0]
	if len(registration.Definition.Matchers) != 1 {
		t.Fatalf("matchers count = %d, want 1", len(registration.Definition.Matchers))
	}

	matcher, ok := registration.Definition.Matchers[0].(runtimeskills.KeywordMatcher)
	if !ok {
		t.Fatalf("matcher type = %T, want KeywordMatcher", registration.Definition.Matchers[0])
	}

	wantKeywords := []string{"find online", "search", "web"}
	if len(matcher.Any) != len(wantKeywords) {
		t.Fatalf("keyword count = %d, want %d", len(matcher.Any), len(wantKeywords))
	}
	for i, wantKeyword := range wantKeywords {
		if matcher.Any[i] != wantKeyword {
			t.Fatalf("keyword[%d] = %q, want %q", i, matcher.Any[i], wantKeyword)
		}
	}

	if !registration.Definition.Matchers[0].Match(runtimeskills.ActivationContext{Prompt: "please search the web"}).Matched {
		t.Fatalf("expected matcher to match prompt with keywords")
	}
	if registration.Definition.Matchers[0].Match(runtimeskills.ActivationContext{Prompt: "write me a poem"}).Matched {
		t.Fatalf("expected matcher not to match unrelated prompt")
	}
}

func TestLoadSkills_EmptyKeywords(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestSkillFile(t, root, "empty-keywords", "---\nname: empty-keywords\ndescription: no keywords\n---\n# Empty Keywords\nStill valid skill body.\n")

	registrations, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(registrations) != 1 {
		t.Fatalf("registration count = %d, want 1", len(registrations))
	}

	registration := registrations[0]
	if registration.Definition.Name != "empty-keywords" {
		t.Fatalf("definition name = %q, want empty-keywords", registration.Definition.Name)
	}
	if len(registration.Definition.Matchers) != 0 {
		t.Fatalf("matchers count = %d, want 0", len(registration.Definition.Matchers))
	}
}

func TestLoadSkills_InvalidYAML(t *testing.T) {
	root := t.TempDir()
	invalidSkillPath := writeTestSkillFile(t, root, "broken", "---\nname: broken\ndescription: invalid yaml\nkeywords: [search, web\n---\n# Broken\n")
	writeTestSkillFile(t, root, "ok", "---\nname: ok\ndescription: valid\nkeywords: [ok]\n---\n# OK\n")

	var logBuf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	originalPrefix := log.Prefix()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
		log.SetPrefix(originalPrefix)
	})

	registrations, err := LoadSkills(root)
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(registrations) != 1 {
		t.Fatalf("registration count = %d, want 1", len(registrations))
	}
	if registrations[0].Definition.Name != "ok" {
		t.Fatalf("definition name = %q, want ok", registrations[0].Definition.Name)
	}

	output := logBuf.String()
	if !strings.Contains(output, "skip invalid YAML skill") {
		t.Fatalf("expected warning log, got: %q", output)
	}
	if !strings.Contains(output, invalidSkillPath) {
		t.Fatalf("expected warning log to include invalid skill path %q, got: %q", invalidSkillPath, output)
	}
}

func writeTestSkillFile(t *testing.T, root, dirName, content string) string {
	t.Helper()

	skillPath := filepath.Join(root, dirName, skillFileName)
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}
	return skillPath
}

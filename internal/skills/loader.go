package skills

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/api"
	runtimeskills "github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"gopkg.in/yaml.v3"
)

const skillFileName = "SKILL.md"

var errInvalidSkillYAML = errors.New("invalid skill YAML frontmatter")

type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
}

func LoadSkills(skillDir string) ([]api.SkillRegistration, error) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return nil, nil
	}

	info, err := os.Stat(skillDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat skills dir %q: %w", skillDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills path is not a directory: %s", skillDir)
	}

	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir %q: %w", skillDir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	registrations := make([]api.SkillRegistration, 0, len(entries))
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(skillDir, entry.Name(), skillFileName)
		reg, skip, parseErr := parseSkillFile(skillPath)
		if parseErr != nil {
			return nil, parseErr
		}
		if skip {
			continue
		}

		if prevPath, exists := seen[reg.Definition.Name]; exists {
			return nil, fmt.Errorf("duplicate skill name %q in %s (already in %s)", reg.Definition.Name, skillPath, prevPath)
		}
		seen[reg.Definition.Name] = skillPath
		registrations = append(registrations, reg)
	}

	return registrations, nil
}

func parseSkillFile(path string) (api.SkillRegistration, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return api.SkillRegistration{}, true, nil
		}
		return api.SkillRegistration{}, false, fmt.Errorf("read skill %q: %w", path, err)
	}

	meta, body, err := parseFrontmatter(content)
	if err != nil {
		if errors.Is(err, errInvalidSkillYAML) {
			log.Printf("[skills] warning: skip invalid YAML skill %s: %v", path, err)
			return api.SkillRegistration{}, true, nil
		}
		return api.SkillRegistration{}, false, fmt.Errorf("parse skill %q: %w", path, err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		return api.SkillRegistration{}, false, fmt.Errorf("parse skill %q: missing name", path)
	}

	body = strings.TrimSpace(body)
	def := runtimeskills.Definition{
		Name:        strings.TrimSpace(meta.Name),
		Description: strings.TrimSpace(meta.Description),
	}

	keywords := sanitizeKeywords(meta.Keywords)
	if len(keywords) > 0 {
		def.Matchers = []runtimeskills.Matcher{
			runtimeskills.KeywordMatcher{Any: keywords},
		}
	}

	handler := runtimeskills.HandlerFunc(func(context.Context, runtimeskills.ActivationContext) (runtimeskills.Result, error) {
		return runtimeskills.Result{
			Skill:  def.Name,
			Output: body,
			Metadata: map[string]any{
				"system_prompt": body,
				"source_path":   path,
			},
		}, nil
	})

	return api.SkillRegistration{Definition: def, Handler: handler}, false, nil
}

func parseFrontmatter(content []byte) (skillFrontmatter, string, error) {
	text := strings.TrimPrefix(string(content), "\uFEFF")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return skillFrontmatter{}, "", errors.New("missing YAML frontmatter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return skillFrontmatter{}, "", errors.New("missing closing frontmatter separator")
	}

	frontmatter := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")

	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return skillFrontmatter{}, "", fmt.Errorf("%w: %v", errInvalidSkillYAML, err)
	}

	return meta, body, nil
}

func sanitizeKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keywords))
	out := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		normalized := strings.ToLower(strings.TrimSpace(keyword))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)

	return out
}

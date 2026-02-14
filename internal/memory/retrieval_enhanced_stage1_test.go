package memory

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellarlinkco/myclaw/internal/config"
)

type stubQueryExpander struct {
	calls  int
	result *QueryExpansion
	err    error
}

func (s *stubQueryExpander) Expand(query string) (*QueryExpansion, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.result == nil {
		return &QueryExpansion{}, nil
	}
	return s.result, nil
}

func TestStrongSignalShortCircuit(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{
		StrongSignalThreshold: 0,
		StrongSignalGap:       0,
	})

	expander := &stubQueryExpander{result: &QueryExpansion{Lexical: []string{"should-not-run"}}}
	e.SetQueryExpander(expander)

	seed := []FactEntry{
		{Content: "hotfix hotfix hotfix retrieval winner", Project: "myclaw", Topic: "retrieval", Category: "decision", Importance: 0.9},
		{Content: "hotfix rollout note", Project: "myclaw", Topic: "ops", Category: "event", Importance: 0.5},
	}
	for _, fact := range seed {
		if err := e.WriteTier2(fact); err != nil {
			t.Fatalf("WriteTier2 error: %v", err)
		}
	}

	results, err := e.Retrieve("hotfix 检索问题怎么修")
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected retrieval results")
	}
	if expander.calls != 0 {
		t.Fatalf("query expander should be skipped on strong signal, calls=%d", expander.calls)
	}
}

func TestStrongSignalSkipsExpansion(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{
		StrongSignalThreshold: 0,
		StrongSignalGap:       0,
	})

	expander := &stubQueryExpander{err: errors.New("must not be called")}
	e.SetQueryExpander(expander)

	if err := e.WriteTier2(FactEntry{Content: "router router router regression", Project: "myclaw", Topic: "network", Category: "decision", Importance: 0.9}); err != nil {
		t.Fatalf("WriteTier2 winner error: %v", err)
	}
	if err := e.WriteTier2(FactEntry{Content: "router fallback note", Project: "myclaw", Topic: "network", Category: "event", Importance: 0.5}); err != nil {
		t.Fatalf("WriteTier2 fallback error: %v", err)
	}

	if _, err := e.Retrieve("router 回归问题"); err != nil {
		t.Fatalf("Retrieve should not fail when strong signal short-circuits expansion: %v", err)
	}
	if expander.calls != 0 {
		t.Fatalf("query expander should be skipped on strong signal, calls=%d", expander.calls)
	}
}

func TestQueryExpansionSanitization(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetRetrievalConfig(config.RetrievalConfig{
		StrongSignalThreshold: 2,
		StrongSignalGap:       1,
	})

	expander := &stubQueryExpander{result: &QueryExpansion{Lexical: []string{
		`" OR *`,
		`sqlite) OR (fts`,
		`safe_token`,
		`NEAR`,
		`配置-项`,
	}}}
	e.SetQueryExpander(expander)

	if err := e.WriteTier2(FactEntry{Content: "safe_token sqlite fts setup", Project: "myclaw", Topic: "memory", Category: "solution", Importance: 0.8}); err != nil {
		t.Fatalf("WriteTier2 error: %v", err)
	}
	if err := e.WriteTier2(FactEntry{Content: "memory retrieval config guidance", Project: "myclaw", Topic: "memory", Category: "decision", Importance: 0.7}); err != nil {
		t.Fatalf("WriteTier2 secondary error: %v", err)
	}

	query := buildFTSMatchQuery(expander.result.Lexical)
	if strings.ContainsAny(query, "*():") {
		t.Fatalf("sanitized MATCH query still contains unsafe syntax: %q", query)
	}

	results, err := e.Retrieve("请帮我找 sqlite 的 memory 配置")
	if err != nil {
		t.Fatalf("Retrieve with expansion sanitization should not fail: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected retrieval results")
	}
	if expander.calls != 1 {
		t.Fatalf("expected query expander to run exactly once when strong signal is not met, calls=%d", expander.calls)
	}
}

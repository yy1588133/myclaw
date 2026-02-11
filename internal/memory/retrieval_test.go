package memory

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldRetrieve(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"我之前的 myclaw 配置是什么？", true},
		{"帮我写个排序函数", false},
		{"```go\nfunc main() {}\n```", false},
		{"好的", false},
		{"上次部署遇到的问题怎么解决的？", true},
		{"你记得我喜欢用什么编辑器吗？", true},
	}
	for _, tc := range cases {
		got := shouldRetrieve(tc.msg)
		if got != tc.want {
			t.Fatalf("shouldRetrieve(%q)=%v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	kw := extractKeywords("我之前在 myclaw 项目里配置了 telegram token，现在怎么改")
	if len(kw) == 0 {
		t.Fatal("expected non-empty keywords")
	}
}

func TestRelevanceScore(t *testing.T) {
	if got := relevanceScore(Memory{Category: "identity", Importance: 1.0}, 180); got != 1.0 {
		t.Fatalf("identity score=%v", got)
	}

	decision := relevanceScore(Memory{Category: "decision", Importance: 0.8}, 7)
	if math.Abs(decision-0.78) > 0.05 {
		t.Fatalf("decision score=%v, expected around 0.78", decision)
	}

	event := relevanceScore(Memory{Category: "event", Importance: 0.5}, 30)
	if math.Abs(event-0.25) > 0.1 {
		t.Fatalf("event score=%v, expected around 0.25", event)
	}

	temp := relevanceScore(Memory{Category: "temp", Importance: 0.3}, 30)
	if temp > 0.08 {
		t.Fatalf("temp score=%v should decay close to zero", temp)
	}
}

func TestRetrieve(t *testing.T) {
	e, err := NewEngine(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetKnownProjects([]string{"myclaw"})
	seed := []FactEntry{
		{Content: "myclaw uses telegram channel manager", Project: "myclaw", Topic: "architecture", Category: "decision", Importance: 0.9},
		{Content: "myclaw memory fallback is file-based", Project: "myclaw", Topic: "memory", Category: "solution", Importance: 0.8},
		{Content: "global preference: respond in Chinese", Project: "_global", Topic: "preferences", Category: "identity", Importance: 1.0},
	}
	for _, f := range seed {
		if err := e.WriteTier2(f); err != nil {
			t.Fatalf("WriteTier2 error: %v", err)
		}
	}

	results, err := e.Retrieve("我之前的 myclaw 记忆配置是什么？")
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected retrieval results")
	}

	refetched, err := e.QueryTier2("", "", 50)
	if err != nil {
		t.Fatalf("QueryTier2 refetch error: %v", err)
	}
	byID := make(map[int64]Memory, len(refetched))
	for _, m := range refetched {
		byID[m.ID] = m
	}

	for _, r := range results {
		if r.Project != "myclaw" && r.Project != "_global" {
			t.Fatalf("unexpected project in retrieval result: %s", r.Project)
		}
		got, ok := byID[r.ID]
		if !ok {
			t.Fatalf("result id not found in refetch: %d", r.ID)
		}
		if got.AccessCount < 1 {
			t.Fatalf("expected access count touched for id=%d", r.ID)
		}
		if daysSince(got.LastAccessed, time.Now().UTC()) > 1 {
			t.Fatalf("expected recent last_accessed for id=%d", r.ID)
		}
	}
}

func BenchmarkShouldRetrieve(b *testing.B) {
	msg := "我之前的 myclaw 配置是什么？"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shouldRetrieve(msg)
	}
}

func BenchmarkRelevanceScore(b *testing.B) {
	mem := Memory{Category: "decision", Importance: 0.8}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = relevanceScore(mem, 30)
	}
}

func BenchmarkRetrieve(b *testing.B) {
	e, err := NewEngine(filepath.Join(b.TempDir(), "memory.db"))
	if err != nil {
		b.Fatalf("NewEngine error: %v", err)
	}
	defer e.Close()

	e.SetKnownProjects([]string{"myclaw"})
	for i := 0; i < 2000; i++ {
		project := "myclaw"
		topic := "architecture"
		if i%3 == 0 {
			project = "_global"
			topic = "preferences"
		}
		if err := e.WriteTier2(FactEntry{Content: "benchmark memory entry", Project: project, Topic: topic, Category: "event", Importance: 0.6}); err != nil {
			b.Fatalf("WriteTier2 error: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Retrieve("我之前的 myclaw 配置是什么？")
		if err != nil {
			b.Fatalf("Retrieve error: %v", err)
		}
	}
}

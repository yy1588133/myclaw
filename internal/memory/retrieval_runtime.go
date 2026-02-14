package memory

import (
	"strings"
	"unicode"

	"github.com/stellarlinkco/myclaw/internal/config"
)

const maxFTSTokens = 16

type retrievalRuntimeConfig struct {
	Mode                  string
	StrongSignalThreshold float64
	StrongSignalGap       float64
	CandidateLimit        int
	RerankLimit           int
}

func defaultRetrievalRuntimeConfig() retrievalRuntimeConfig {
	return retrievalRuntimeConfig{
		Mode:                  config.DefaultMemoryRetrievalMode,
		StrongSignalThreshold: config.DefaultMemoryStrongSignalThreshold,
		StrongSignalGap:       config.DefaultMemoryStrongSignalGap,
		CandidateLimit:        config.DefaultMemoryRetrievalCandidateLimit,
		RerankLimit:           config.DefaultMemoryRetrievalRerankLimit,
	}
}

func normalizeRetrievalRuntimeConfig(cfg config.RetrievalConfig) retrievalRuntimeConfig {
	runtimeCfg := defaultRetrievalRuntimeConfig()
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case config.MemoryRetrievalModeEnhanced:
		runtimeCfg.Mode = config.MemoryRetrievalModeEnhanced
	default:
		runtimeCfg.Mode = config.MemoryRetrievalModeClassic
	}
	if cfg.StrongSignalThreshold >= 0 {
		runtimeCfg.StrongSignalThreshold = cfg.StrongSignalThreshold
	}
	if cfg.StrongSignalGap >= 0 {
		runtimeCfg.StrongSignalGap = cfg.StrongSignalGap
	}
	if cfg.CandidateLimit > 0 {
		runtimeCfg.CandidateLimit = cfg.CandidateLimit
	}
	if cfg.RerankLimit > 0 {
		runtimeCfg.RerankLimit = cfg.RerankLimit
	}
	return runtimeCfg
}

func buildFTSMatchQuery(tokens []string) string {
	safe := sanitizeFTSTokens(tokens)
	if len(safe) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(safe))
	for _, token := range safe {
		quoted = append(quoted, `"`+token+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func sanitizeFTSTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}

	reserved := map[string]struct{}{
		"and":  {},
		"or":   {},
		"not":  {},
		"near": {},
	}

	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		normalized := normalizeFTSToken(token)
		for _, part := range strings.Fields(normalized) {
			if _, blocked := reserved[part]; blocked {
				continue
			}
			if part == "" {
				continue
			}
			if _, exists := seen[part]; exists {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}

	if len(out) > maxFTSTokens {
		out = out[:maxFTSTokens]
	}

	return out
}

func normalizeFTSToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteByte(' ')
	}

	return b.String()
}

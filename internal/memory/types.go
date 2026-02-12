package memory

// Memory is a tier-1/tier-2 memory record.
type Memory struct {
	ID           int64
	Tier         int
	Project      string
	Topic        string
	Category     string
	Content      string
	Importance   float64
	Source       string
	CreatedAt    string
	UpdatedAt    string
	LastAccessed string
	AccessCount  int
	IsArchived   bool
}

// EventEntry is a tier-3 daily event row.
type EventEntry struct {
	ID           int64
	Date         string
	Channel      string
	SenderID     string
	Summary      string
	Tokens       int
	IsCompressed bool
	CreatedAt    string
}

// BufferMessage is a persisted extraction buffer message.
type BufferMessage struct {
	ID         int64
	Channel    string
	SenderID   string
	Role       string
	Content    string
	TokenCount int
	CreatedAt  string
}

// FactEntry is a normalized fact produced by extraction/compression.
type FactEntry struct {
	Content    string  `json:"content"`
	Project    string  `json:"project"`
	Topic      string  `json:"topic"`
	Category   string  `json:"category"`
	Importance float64 `json:"importance"`
}

// ExtractionResult is the LLM extraction output.
type ExtractionResult struct {
	Facts   []FactEntry `json:"facts"`
	Summary string      `json:"summary"`
}

// CompressionResult is the LLM compression output.
type CompressionResult struct {
	Facts []FactEntry `json:"facts"`
}

// ProfileEntry is one core-profile entry.
type ProfileEntry struct {
	Content  string `json:"content"`
	Category string `json:"category"`
}

// ProfileResult is the LLM profile-update output.
type ProfileResult struct {
	Entries []ProfileEntry `json:"entries"`
}

// MemoryStats is a compact snapshot used by status reporting.
type MemoryStats struct {
	Tier1Count       int
	Tier2ActiveCount int
	Tier2Archived    int
	EventPending     int
	EventCompressed  int
	BufferMessages   int
}

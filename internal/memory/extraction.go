package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/stellarlinkco/myclaw/internal/config"
)

type ExtractionService struct {
	engine     *Engine
	llm        LLMClient
	quietGap   time.Duration
	tokenCap   int
	dailyFlush string

	mu      sync.Mutex
	timer   *time.Timer
	stopCh  chan struct{}
	stopWg  sync.WaitGroup
	started bool
}

func NewExtractionService(engine *Engine, llm LLMClient, cfg config.ExtractionConfig) *ExtractionService {
	quietGap := 3 * time.Minute
	if d, err := time.ParseDuration(strings.TrimSpace(cfg.QuietGap)); err == nil && d > 0 {
		quietGap = d
	}
	budget := cfg.TokenBudget
	if budget <= 0 {
		budget = config.DefaultMemoryTokenBudget
	}
	tokenCap := int(10000 * budget)
	if tokenCap < 1000 {
		tokenCap = 1000
	}

	dailyFlush := strings.TrimSpace(cfg.DailyFlush)
	if dailyFlush == "" {
		dailyFlush = config.DefaultMemoryDailyFlush
	}

	return &ExtractionService{
		engine:     engine,
		llm:        llm,
		quietGap:   quietGap,
		tokenCap:   tokenCap,
		dailyFlush: dailyFlush,
		stopCh:     make(chan struct{}),
	}
}

func (s *ExtractionService) BufferMessage(channel, senderID, role, content string) {
	msg := BufferMessage{
		Channel:    channel,
		SenderID:   senderID,
		Role:       role,
		Content:    content,
		TokenCount: estimateTokens(content),
	}
	if err := s.engine.WriteBuffer(msg); err != nil {
		log.Printf("[memory] write buffer error: %v", err)
		return
	}

	s.resetQuietTimer()

	tokens, err := s.engine.BufferTokenCount()
	if err != nil {
		log.Printf("[memory] buffer token count error: %v", err)
		return
	}
	if tokens >= s.tokenCap {
		go s.flush()
	}
}

func (s *ExtractionService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	s.resetQuietTimer()

	s.stopWg.Add(1)
	go func() {
		defer s.stopWg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				if shouldFlushNow(time.Now(), s.dailyFlush) {
					go s.flush()
				}
			}
		}
	}()
}

func (s *ExtractionService) Stop() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.mu.Unlock()

	s.stopWg.Wait()
	s.flush()
}

func (s *ExtractionService) resetQuietTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.quietGap, func() {
		s.flush()
	})
}

func (s *ExtractionService) flush() {
	msgs, err := s.engine.DrainBuffer(500)
	if err != nil {
		log.Printf("[memory] drain buffer error: %v", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	conversation := formatConversation(msgs)
	extracted, err := s.llm.Extract(conversation)
	if err != nil {
		log.Printf("[memory] extraction error: %v", err)
		// Keep data recoverability by re-queueing drained messages.
		for _, m := range msgs {
			_ = s.engine.WriteBuffer(m)
		}
		return
	}

	for _, fact := range extracted.Facts {
		if err := s.engine.WriteTier2(fact); err != nil {
			log.Printf("[memory] write tier2 from extraction error: %v", err)
		}
	}

	event := EventEntry{
		Date:     time.Now().Format("2006-01-02"),
		Channel:  msgs[0].Channel,
		SenderID: msgs[0].SenderID,
		Summary:  extracted.Summary,
		Tokens:   totalTokens(msgs),
	}
	if err := s.engine.WriteTier3(event); err != nil {
		log.Printf("[memory] write tier3 from extraction error: %v", err)
	}
}

func formatConversation(msgs []BufferMessage) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%s][%s]: %s\n", m.Role, m.Channel, m.Content))
	}
	return strings.TrimSpace(sb.String())
}

func totalTokens(msgs []BufferMessage) int {
	total := 0
	for _, m := range msgs {
		total += m.TokenCount
	}
	return total
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	chineseChars := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseChars++
		}
	}
	words := len(strings.Fields(text))
	estimate := int(float64(chineseChars)*1.5 + float64(words)*0.75)
	if estimate < 1 {
		return 1
	}
	return estimate
}

func shouldFlushNow(now time.Time, hhmm string) bool {
	parts := strings.Split(hhmm, ":")
	if len(parts) != 2 {
		return false
	}
	hour, err1 := time.Parse("15", parts[0])
	minute, err2 := time.Parse("04", parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return now.Hour() == hour.Hour() && now.Minute() == minute.Minute()
}

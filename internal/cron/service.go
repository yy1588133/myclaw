package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	rcron "github.com/robfig/cron/v3"
)

type Service struct {
	storePath string
	mu        sync.Mutex
	jobs      []CronJob
	OnJob     func(job CronJob) (string, error)
	cron      *rcron.Cron
	entryMap  map[string]rcron.EntryID // job ID -> cron entry ID
	cancel    context.CancelFunc
	stopCh    chan struct{}
}

func NewService(storePath string) *Service {
	return &Service{
		storePath: storePath,
		entryMap:  make(map[string]rcron.EntryID),
	}
}

func (s *Service) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	stopCh := make(chan struct{})
	s.mu.Lock()
	s.cancel = cancel
	s.stopCh = stopCh
	s.mu.Unlock()

	if err := s.load(); err != nil {
		log.Printf("[cron] warning: failed to load jobs: %v", err)
	}

	s.cron = rcron.New(rcron.WithSeconds())

	s.mu.Lock()
	for i := range s.jobs {
		if s.jobs[i].Enabled && s.jobs[i].Schedule.Kind == "cron" {
			s.registerJob(&s.jobs[i])
		}
	}
	s.mu.Unlock()

	s.cron.Start()
	log.Printf("[cron] started with %d jobs", len(s.jobs))

	// Handle "every" and "at" jobs in a separate goroutine
	go s.tickLoop(runCtx)

	go func() {
		select {
		case <-ctx.Done():
			s.Stop()
		case <-stopCh:
			return
		}
	}()

	return nil
}

func (s *Service) registerJob(job *CronJob) {
	jobCopy := *job
	id, err := s.cron.AddFunc(job.Schedule.Expr, func() {
		s.executeJob(jobCopy)
	})
	if err != nil {
		log.Printf("[cron] failed to register job %s (%s): %v", job.Name, job.Schedule.Expr, err)
		return
	}
	s.entryMap[job.ID] = id
}

func (s *Service) executeJob(job CronJob) {
	log.Printf("[cron] executing job %s (%s)", job.Name, job.ID)

	if s.OnJob == nil {
		log.Printf("[cron] no OnJob handler set")
		return
	}

	result, err := s.OnJob(job)

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.jobs {
		if s.jobs[i].ID == job.ID {
			jobID := s.jobs[i].ID
			s.jobs[i].State.LastRunAtMs = time.Now().UnixMilli()
			if err != nil {
				s.jobs[i].State.LastStatus = "error"
				s.jobs[i].State.LastError = err.Error()
				log.Printf("[cron] job %s error: %v", job.Name, err)
			} else {
				s.jobs[i].State.LastStatus = "ok"
				s.jobs[i].State.LastError = ""
				log.Printf("[cron] job %s result: %s", job.Name, truncate(result, 100))
			}

			if s.jobs[i].DeleteAfterRun {
				if entryID, ok := s.entryMap[jobID]; ok && s.cron != nil {
					s.cron.Remove(entryID)
					delete(s.entryMap, jobID)
				}
				s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			}
			break
		}
	}

	_ = s.save()
}

func (s *Service) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now().UnixMilli()
			s.mu.Lock()
			for i := range s.jobs {
				job := &s.jobs[i]
				if !job.Enabled {
					continue
				}
				switch job.Schedule.Kind {
				case "every":
					if job.Schedule.EveryMs > 0 {
						nextRun := job.State.LastRunAtMs + job.Schedule.EveryMs
						if now >= nextRun {
							jobCopy := *job
							s.mu.Unlock()
							s.executeJob(jobCopy)
							s.mu.Lock()
						}
					}
				case "at":
					if job.Schedule.AtMs > 0 && now >= job.Schedule.AtMs {
						jobCopy := *job
						job.Enabled = false
						s.mu.Unlock()
						s.executeJob(jobCopy)
						s.mu.Lock()
					}
				}
			}
			s.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	stopCh := s.stopCh
	s.cancel = nil
	s.stopCh = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stopCh != nil {
		close(stopCh)
	}

	if s.cron != nil {
		stopCtx := s.cron.Stop()
		select {
		case <-stopCtx.Done():
		case <-time.After(5 * time.Second):
			log.Printf("[cron] stop timeout waiting for running jobs")
		}
	}
	log.Printf("[cron] stopped")
}

func (s *Service) AddJob(name string, schedule Schedule, payload Payload) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := NewCronJob(name, schedule, payload)
	s.jobs = append(s.jobs, job)

	if job.Schedule.Kind == "cron" && s.cron != nil {
		s.registerJob(&s.jobs[len(s.jobs)-1])
	}

	if err := s.save(); err != nil {
		return nil, fmt.Errorf("save jobs: %w", err)
	}

	return &job, nil
}

func (s *Service) RemoveJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, job := range s.jobs {
		if job.ID == id {
			if entryID, ok := s.entryMap[id]; ok {
				s.cron.Remove(entryID)
				delete(s.entryMap, id)
			}
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			_ = s.save()
			return true
		}
	}
	return false
}

func (s *Service) ListJobs() []CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]CronJob, len(s.jobs))
	copy(result, s.jobs)
	return result
}

func (s *Service) EnableJob(id string, enabled bool) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs[i].Enabled = enabled
			if s.jobs[i].Schedule.Kind == "cron" && s.cron != nil {
				if enabled {
					if _, ok := s.entryMap[id]; !ok {
						s.registerJob(&s.jobs[i])
					}
				} else {
					if entryID, ok := s.entryMap[id]; ok {
						s.cron.Remove(entryID)
						delete(s.entryMap, id)
					}
				}
			}
			_ = s.save()
			job := s.jobs[i]
			return &job, nil
		}
	}
	return nil, fmt.Errorf("job %s not found", id)
}

func (s *Service) load() error {
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.jobs)
}

func (s *Service) save() error {
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.storePath, data, 0644)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

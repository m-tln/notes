package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Note struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type EmailTask struct {
	Note    Note
	Type    string
	NoteID  string
}

type EmailService struct {
	emailAddr    string
	storage      map[string]Note
	mu           sync.RWMutex
	taskQueue    chan EmailTask
	workerCount  int
	maxQueueSize int
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewEmailService(emailAddr string, workerCount, maxQueueSize int) *EmailService {
	ctx, cancel := context.WithCancel(context.Background())
	
	service := &EmailService{
		emailAddr:    emailAddr,
		storage:      make(map[string]Note),
		taskQueue:    make(chan EmailTask, maxQueueSize),
		workerCount:  workerCount,
		maxQueueSize: maxQueueSize,
		ctx:          ctx,
		cancel:       cancel,
	}

	for i := range workerCount {
		service.wg.Add(1)
		go service.worker(i + 1)
	}

	log.Printf("[EMAIL] Started %d workers with queue size %d", workerCount, maxQueueSize)
	return service
}

func (s *EmailService) worker(id int) {
	defer s.wg.Done()
	
	log.Printf("[EMAIL-WORKER-%d] Worker started", id)
	
	for {
		select {
		case <-s.ctx.Done():
			log.Printf("[EMAIL-WORKER-%d] Worker stopped", id)
			return
		case task := <-s.taskQueue:
			s.processTask(task, id)
		}
	}
}

func (s *EmailService) processTask(task EmailTask, workerID int) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	switch task.Type {
	case "store":
		s.mu.Lock()
		s.storage[task.Note.ID] = task.Note
		s.mu.Unlock()
		log.Printf("[EMAIL-WORKER-%d] Stored note: %s (Title: %s)", 
			workerID, task.Note.ID, task.Note.Title)
		
	case "send":
		s.mu.RLock()
		note, exists := s.storage[task.NoteID]
		s.mu.RUnlock()
		
		if !exists {
			log.Printf("[EMAIL-WORKER-%d] Note not found for sending: %s", 
				workerID, task.NoteID)
			return
		}
		
		select {
		case <-ctx.Done():
			log.Printf("[EMAIL-WORKER-%d] Send task cancelled: %s", 
				workerID, task.NoteID)
			return
		case <-time.After(100 * time.Millisecond):
			log.Printf("[EMAIL-WORKER-%d] Sent email to %s: ID=%s, Title=%s", 
				workerID, s.emailAddr, note.ID, note.Title)
		}
	}
}

func (s *EmailService) ExtractNote(ctx context.Context, noteID string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	s.mu.RLock()
	note, exists := s.storage[noteID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("note not found")
	}

	task := EmailTask{
		Type:   "send",
		NoteID: noteID,
		Note:   note,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.taskQueue <- task:
		log.Printf("[EMAIL] Extraction task queued: %s", noteID)
		return nil
	default:
		return fmt.Errorf("email queue is full, try again later")
	}
}

func (s *EmailService) StoreNote(ctx context.Context, note Note) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	task := EmailTask{
		Type: "store",
		Note: note,
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.taskQueue <- task:
		log.Printf("[EMAIL] Store task queued: %s", note.ID)
		return nil
	default:
		return fmt.Errorf("email queue is full, try again later")
	}
}

func (s *EmailService) GetQueueStats() (int, int) {
	return len(s.taskQueue), cap(s.taskQueue)
}

func (s *EmailService) GetStorageStats() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.storage)
}

func (s *EmailService) Shutdown() {
	log.Println("[EMAIL] Shutting down email service...")
	s.cancel()
	
	s.wg.Wait()
	close(s.taskQueue)
	
	log.Println("[EMAIL] Email service stopped gracefully")
}

func main() {
	emailAddr := os.Getenv("EMAIL_ADDR")
	if emailAddr == "" {
		emailAddr = "admin@example.com"
	}

	workerCount := 3
	if wc := os.Getenv("EMAIL_WORKERS"); wc != "" {
		if n, err := fmt.Sscanf(wc, "%d", &workerCount); n != 1 || err != nil {
			workerCount = 3
		}
	}

	queueSize := 100
	if qs := os.Getenv("EMAIL_QUEUE_SIZE"); qs != "" {
		if n, err := fmt.Sscanf(qs, "%d", &queueSize); n != 1 || err != nil {
			queueSize = 100
		}
	}

	service := NewEmailService(emailAddr, workerCount, queueSize)
	defer service.Shutdown()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	stop := make(chan os.Signal, 1)

	http.HandleFunc("/email/extract", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			NoteID string `json:"note_id"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.NoteID == "" {
			http.Error(w, "note_id is required", http.StatusBadRequest)
			return
		}

		if err := service.ExtractNote(r.Context(), req.NoteID); err != nil {
			log.Printf("[EMAIL] Extraction failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "extraction_queued",
			"to":     service.emailAddr,
			"note_id": req.NoteID,
		})
	})

	http.HandleFunc("/email/store", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var note Note
		if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if note.ID == "" {
			http.Error(w, "note.id is required", http.StatusBadRequest)
			return
		}

		if err := service.StoreNote(r.Context(), note); err != nil {
			log.Printf("[EMAIL] Storage failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "storage_queued",
			"id":     note.ID,
		})
	})

	http.HandleFunc("/email/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		queueLen, queueCap := service.GetQueueStats()
		storageCount := service.GetStorageStats()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"queue_size":      queueLen,
			"queue_capacity":  queueCap,
			"queue_usage":     fmt.Sprintf("%.1f%%", float64(queueLen)/float64(queueCap)*100),
			"storage_count":   storageCount,
			"workers":         service.workerCount,
			"email_address":   service.emailAddr,
			"status":          "operational",
		})
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		queueLen, queueCap := service.GetQueueStats()
		if float64(queueLen)/float64(queueCap) > 0.9 {
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "degraded",
				"reason": "queue_full",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
		})
	})

	server := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-stop
		log.Println("[EMAIL] Received shutdown signal")
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("[EMAIL] Error during shutdown: %v", err)
		}
	}()

	log.Printf("[EMAIL] Email service starting on port %s", port)
	log.Printf("[EMAIL] Config: %d workers, queue size %d", workerCount, queueSize)
	
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[EMAIL] Server error: %v", err)
	}
	
	log.Println("[EMAIL] Server stopped")
}
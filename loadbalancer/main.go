package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Backend struct {
	URL          *url.URL
	Alive        bool
	mux          sync.RWMutex
	ReverseProxy *httputil.ReverseProxy
	FailureCount int
	LastCheck    time.Time
}

func (b *Backend) SetAlive(alive bool) {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.Alive = alive
	if !alive {
		b.FailureCount++
	} else {
		b.FailureCount = 0
	}
	b.LastCheck = time.Now()
}

func (b *Backend) IsAlive() bool {
	b.mux.RLock()
	defer b.mux.RUnlock()

	if b.FailureCount > 3 && time.Since(b.LastCheck) < 30*time.Second {
		return false
	}

	return b.Alive
}

type ServerPool struct {
	backends []*Backend
	current  uint64
}

func (s *ServerPool) AddBackend(backend *Backend) {
	s.backends = append(s.backends, backend)
}

func (s *ServerPool) NextIndex() int {
	return int((atomic.LoadUint64(&s.current) + 1) % uint64(len(s.backends)))
}

func (s *ServerPool) MarkBackendStatus(backendUrl *url.URL, alive bool) {
	for _, b := range s.backends {
		if b.URL.String() == backendUrl.String() {
			b.SetAlive(alive)
			break
		}
	}
}

func (s *ServerPool) GetNextPeer() *Backend {
	next := s.NextIndex()
	l := len(s.backends) + next

	for i := next; i < l; i++ {
		idx := i % len(s.backends)
		if s.backends[idx].IsAlive() {
			atomic.StoreUint64(&s.current, uint64(idx))
			return s.backends[idx]
		}
	}
	return nil
}

func (s *ServerPool) HealthCheck() {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	client := http.Client{
		Timeout:   2 * time.Second,
		Transport: transport,
	}

	for _, b := range s.backends {
		if !b.IsAlive() && b.FailureCount > 3 && time.Since(b.LastCheck) < 30*time.Second {
			log.Printf("Backend %s is in circuit breaker state (failures: %d)", b.URL.String(), b.FailureCount)
			continue
		}

		status := b.IsAlive()

		resp, err := client.Get(b.URL.String() + "/health")
		if err != nil {
			log.Printf("Backend %s is down: %v", b.URL.String(), err)
			b.SetAlive(false)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("Backend %s returned non-200: %d", b.URL.String(), resp.StatusCode)
			b.SetAlive(false)
			continue
		}

		if !status {
			log.Printf("Backend %s is back up (was down for %v)", b.URL.String(), time.Since(b.LastCheck))
		}
		b.SetAlive(true)
	}
}

func loadBalancer(w http.ResponseWriter, r *http.Request) {
	peer := serverPool.GetNextPeer()
	if peer != nil {
		log.Printf("Routing request to: %s", peer.URL.String())
		peer.ReverseProxy.ServeHTTP(w, r)
		return
	}
	log.Printf("No healthy backends available")
	http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
}

var serverPool ServerPool

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	var backends []string

	if envBackends := os.Getenv("BACKENDS"); envBackends != "" {
		log.Printf("Parsing backends from environment variable: %s", envBackends)
		backends = parseBackendsFromEnv(envBackends)
	} else {
		backends = []string{
			"http://app1:8080",
			"http://app2:8080",
			"http://app3:8080",
		}
		log.Printf("Using default backends: %v", backends)
	}

	if len(backends) == 0 {
		log.Fatal("No backends configured. Set BACKENDS environment variable with comma-separated URLs")
	}

	log.Printf("Initializing load balancer with %d backends", len(backends))

	for _, b := range backends {
		backendUrl, err := url.Parse(b)
		if err != nil {
			log.Fatalf("Failed to parse backend URL %s: %v", b, err)
		}

		proxy := httputil.NewSingleHostReverseProxy(backendUrl)

		proxy.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			ResponseHeaderTimeout: 2 * time.Second,
			IdleConnTimeout:       2 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
		}

		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("Error proxying to %s: %v", backendUrl.String(), err)
			serverPool.MarkBackendStatus(backendUrl, false)

			peer := serverPool.GetNextPeer()
			if peer != nil && peer.URL.String() != backendUrl.String() {
				log.Printf("Retrying request with: %s", peer.URL.String())
				peer.ReverseProxy.ServeHTTP(w, r)
				return
			}

			log.Printf("No healthy backends available for retry")
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		}

		proxy.Director = func(req *http.Request) {
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Real-IP", req.RemoteAddr)
			req.URL.Scheme = backendUrl.Scheme
			req.URL.Host = backendUrl.Host
			req.Host = backendUrl.Host
		}

		serverPool.AddBackend(&Backend{
			URL:          backendUrl,
			Alive:        true,
			ReverseProxy: proxy,
			LastCheck:    time.Now(),
		})

		log.Printf("Configured backend: %s", backendUrl.String())
	}

	go func() {
		time.Sleep(5 * time.Second)
		log.Println("Performing initial health check...")
		serverPool.HealthCheck()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			log.Println("Starting periodic health check...")
			serverPool.HealthCheck()
			healthyCount := countHealthyBackends()
			log.Printf("Health check completed. Healthy backends: %d/%d",
				healthyCount, len(serverPool.backends))

			if os.Getenv("DEBUG") == "true" {
				for i, b := range serverPool.backends {
					status := "up"
					if !b.IsAlive() {
						status = "down"
					}
					log.Printf("  Backend %d: %s [%s] failures: %d",
						i, b.URL.String(), status, b.FailureCount)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", loadBalancer)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/status", statusHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "443"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
		TLSConfig: &tls.Config{
            MinVersion: tls.VersionTLS12,
        },
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("Load balancer server starting on port %s (HTTPS)", port)
        log.Printf("Monitoring %d backends", len(serverPool.backends))
        
        certFile := os.Getenv("TLS_CERT")
        keyFile := os.Getenv("TLS_KEY")
        
        if certFile != "" && keyFile != "" {
            if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
                log.Fatalf("Server error: %v", err)
            }
        } else {
            log.Fatal("TLS_CERT and TLS_KEY environment variables are required for HTTPS")
        }
	}()

	<-stop
	log.Println("Shutdown signal received...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Forced shutdown: %v", err)
	}

	log.Println("Load balancer stopped gracefully")
}

func parseBackendsFromEnv(envString string) []string {
	var backends []string

	parts := strings.SplitSeq(envString, ",")

	for part := range parts {
		backend := strings.TrimSpace(part)
		if backend != "" {
			if !strings.HasPrefix(backend, "http://") && !strings.HasPrefix(backend, "https://") {
				backend = "http://" + backend
				log.Printf("Added http:// prefix to backend: %s", backend)
			}
			backends = append(backends, backend)
		}
	}

	return backends
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type BackendStatus struct {
		URL          string `json:"url"`
		Alive        bool   `json:"alive"`
		FailureCount int    `json:"failure_count"`
		LastCheck    string `json:"last_check"`
	}

	type StatusResponse struct {
		Status          string          `json:"status"`
		TotalBackends   int             `json:"total_backends"`
		HealthyBackends int             `json:"healthy_backends"`
		CurrentIndex    int             `json:"current_index"`
		Backends        []BackendStatus `json:"backends"`
	}

	response := StatusResponse{
		Status:          "operational",
		TotalBackends:   len(serverPool.backends),
		HealthyBackends: countHealthyBackends(),
		CurrentIndex:    int(atomic.LoadUint64(&serverPool.current)),
	}

	for _, b := range serverPool.backends {
		b.mux.RLock()
		backendStatus := BackendStatus{
			URL:          b.URL.String(),
			Alive:        b.Alive,
			FailureCount: b.FailureCount,
			LastCheck:    b.LastCheck.Format(time.RFC3339),
		}
		b.mux.RUnlock()

		if b.FailureCount > 3 && time.Since(b.LastCheck) < 30*time.Second {
			backendStatus.Alive = false
		}

		response.Backends = append(response.Backends, backendStatus)
	}

	if response.HealthyBackends == 0 {
		response.Status = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	healthyCount := countHealthyBackends()

	if healthyCount == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "UNHEALTHY: No healthy backends available")
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "HEALTHY: %d/%d backends available",
		healthyCount, len(serverPool.backends))
}

func countHealthyBackends() int {
	count := 0
	for _, b := range serverPool.backends {
		if b.IsAlive() {
			count++
		}
	}
	return count
}

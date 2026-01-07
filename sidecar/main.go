package main

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

type SidecarProxy struct {
	upstreamURL string
	proxy       *httputil.ReverseProxy
	certFile    string
	keyFile     string
}

func NewSidecarProxy(upstreamURL, certFile, keyFile string) (*SidecarProxy, error) {
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM([]byte(os.Getenv("CA_CERT")))

	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: caCertPool,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	return &SidecarProxy{
		upstreamURL: upstreamURL,
		proxy:       proxy,
		certFile:    certFile,
		keyFile:     keyFile,
	}, nil
}

func (s *SidecarProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[SIDECAR] %s %s -> %s", r.Method, r.URL.Path, s.upstreamURL)

	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Port", "443")
	r.Header.Set("X-Service-Mesh", "sidecar-proxy")

	s.proxy.ServeHTTP(w, r)
}

func main() {
	upstream := os.Getenv("UPSTREAM_SERVICE")
	if upstream == "" {
		log.Fatal("UPSTREAM_SERVICE environment variable is required")
	}

	port := os.Getenv("SIDECAR_PORT")
	if port == "" {
		port = "8443"
	}

	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")

	if certFile == "" || keyFile == "" {
		log.Fatal("TLS_CERT and TLS_KEY environment variables are required")
	}

	proxy, err := NewSidecarProxy(upstream, certFile, keyFile)
	if err != nil {
		log.Fatalf("Failed to create sidecar proxy: %v", err)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(strings.Replace(upstream, "https", "http", 1) + "/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Upstream unavailable"))
			return
		}
		defer resp.Body.Close()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/", proxy.ServeHTTP)

	log.Printf("Sidecar proxy listening on :%s for upstream: %s", port, upstream)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("Failed to load certificates: %v", err)
	}

	server := &http.Server{
		Addr: ":" + port,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Fatal(server.ListenAndServeTLS("", ""))
}

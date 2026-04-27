package subproxy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/keli-123456/kelinode/conf"
	log "github.com/sirupsen/logrus"
)

const (
	defaultHTTPSListen      = "0.0.0.0:443"
	defaultMaxResponseBytes = int64(10 * 1024 * 1024)
	defaultChallengeDir     = "/etc/v2node/subproxy/challenges"
)

type Status struct {
	Status            string    `json:"status"`
	Enabled           bool      `json:"enabled"`
	Running           bool      `json:"running"`
	Mode              string    `json:"mode"`
	HTTPSListen       string    `json:"https_listen"`
	Profiles          int       `json:"profiles"`
	CertificateDomain string    `json:"certificate_domain,omitempty"`
	CertificateID     string    `json:"certificate_id,omitempty"`
	NeedCertificate   bool      `json:"need_certificate,omitempty"`
	CSRPem            string    `json:"csr_pem,omitempty"`
	ValidationReady   bool      `json:"validation_ready,omitempty"`
	CertNotAfter      string    `json:"cert_not_after,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Manager struct {
	mu          sync.Mutex
	server      *http.Server
	httpServer  *http.Server
	fingerprint string
	status      Status
	client      *http.Client
}

func NewManager() *Manager {
	return &Manager{
		status: Status{UpdatedAt: time.Now()},
		client: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   15 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (m *Manager) Apply(cfg conf.SubscriptionProxyConfig) error {
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		m.closeWithStatus(Status{
			Status:    "error",
			Enabled:   normalized.Enabled,
			Running:   false,
			LastError: err.Error(),
			UpdatedAt: time.Now(),
		})
		return err
	}
	if !normalized.Enabled || len(normalized.Profiles) == 0 {
		m.closeWithStatus(Status{
			Status:    "disabled",
			Enabled:   false,
			Running:   false,
			Mode:      "disabled",
			UpdatedAt: time.Now(),
		})
		return nil
	}

	nextFingerprint := fingerprint(normalized)
	m.mu.Lock()
	if m.server != nil && m.fingerprint == nextFingerprint {
		m.mu.Unlock()
		return nil
	}
	old := m.server
	oldHTTP := m.httpServer
	m.server = nil
	m.httpServer = nil
	m.fingerprint = ""
	m.mu.Unlock()
	if old != nil {
		shutdownServer(old)
	}
	if oldHTTP != nil {
		shutdownServer(oldHTTP)
	}

	certStatus := m.prepareCertificate(&normalized)
	httpSrv := m.newHTTPServer(normalized)

	profiles := make(map[string]conf.SubscriptionProxyProfile, len(normalized.Profiles))
	for _, profile := range normalized.Profiles {
		profiles[strings.ToLower(profile.SiteID)] = profile
	}

	srv := &http.Server{
		Addr:              normalized.HTTPSListen,
		Handler:           m.handler(profiles, normalized.MaxResponseBytes),
		ReadHeaderTimeout: 10 * time.Second,
	}

	mode := "https"
	serve := func() error {
		return srv.ListenAndServeTLS(normalized.CertFile, normalized.KeyFile)
	}
	if !fileReadable(normalized.CertFile) || !fileReadable(normalized.KeyFile) {
		if !normalized.AllowHTTPFallback {
			err := fmt.Errorf("subscription proxy certificate files are not readable: cert=%s key=%s", normalized.CertFile, normalized.KeyFile)
			m.mu.Lock()
			m.httpServer = httpSrv
			m.fingerprint = nextFingerprint
			m.status = Status{
				Status:      "error",
				Enabled:     true,
				Running:     false,
				Mode:        "error",
				HTTPSListen: normalized.HTTPSListen,
				Profiles:    len(normalized.Profiles),
				LastError:   err.Error(),
				UpdatedAt:   time.Now(),
			}
			m.mergeStatusLocked(certStatus)
			m.mu.Unlock()
			startHTTPServer(httpSrv, m)
			return err
		}
		mode = "http"
		serve = srv.ListenAndServe
	}

	m.mu.Lock()
	m.server = srv
	m.httpServer = httpSrv
	m.fingerprint = nextFingerprint
	m.status = Status{
		Status:      "running",
		Enabled:     true,
		Running:     true,
		Mode:        mode,
		HTTPSListen: normalized.HTTPSListen,
		Profiles:    len(normalized.Profiles),
		UpdatedAt:   time.Now(),
	}
	m.mergeStatusLocked(certStatus)
	m.mu.Unlock()

	go func() {
		log.WithFields(log.Fields{
			"listen":   normalized.HTTPSListen,
			"mode":     mode,
			"profiles": len(normalized.Profiles),
		}).Info("Subscription proxy started")
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.setError(err)
			log.WithField("err", err).Error("Subscription proxy stopped unexpectedly")
		}
	}()
	startHTTPServer(httpSrv, m)

	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	srv := m.server
	httpSrv := m.httpServer
	m.server = nil
	m.httpServer = nil
	m.fingerprint = ""
	m.status.Running = false
	if m.status.Status == "running" {
		m.status.Status = "stopped"
	}
	m.status.UpdatedAt = time.Now()
	m.mu.Unlock()
	if srv == nil {
		if httpSrv != nil {
			return shutdownServer(httpSrv)
		}
		return nil
	}
	err := shutdownServer(srv)
	if httpSrv != nil {
		if httpErr := shutdownServer(httpSrv); err == nil {
			err = httpErr
		}
	}
	return err
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) closeWithStatus(status Status) {
	m.mu.Lock()
	srv := m.server
	httpSrv := m.httpServer
	m.server = nil
	m.httpServer = nil
	m.fingerprint = ""
	m.status = status
	m.mu.Unlock()
	if srv != nil {
		shutdownServer(srv)
	}
	if httpSrv != nil {
		shutdownServer(httpSrv)
	}
}

func shutdownServer(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func (m *Manager) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = nil
	m.httpServer = nil
	m.fingerprint = ""
	m.status.Running = false
	m.status.Status = "error"
	m.status.LastError = err.Error()
	m.status.UpdatedAt = time.Now()
}

func startHTTPServer(srv *http.Server, manager *Manager) {
	if srv == nil {
		return
	}
	go func() {
		log.WithField("listen", srv.Addr).Info("Subscription proxy HTTP challenge server started")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if manager != nil {
				manager.setHTTPError(srv, err)
			}
			log.WithField("err", err).Error("Subscription proxy HTTP challenge server stopped unexpectedly")
		}
	}()
}

func (m *Manager) setHTTPError(srv *http.Server, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if srv != nil && m.httpServer == srv {
		m.httpServer = nil
	}
	if m.status.Status != "running" {
		m.status.Running = false
		m.status.Status = "error"
	}
	m.status.LastError = err.Error()
	m.status.UpdatedAt = time.Now()
}

func (m *Manager) handler(profiles map[string]conf.SubscriptionProxyProfile, maxBytes int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/sub/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.EscapedPath(), "/sub/")
		siteID, tokenPart, ok := strings.Cut(rest, "/")
		if !ok || siteID == "" || tokenPart == "" {
			http.NotFound(w, r)
			return
		}
		profile, exists := profiles[strings.ToLower(siteID)]
		if !exists {
			http.NotFound(w, r)
			return
		}
		token, err := url.PathUnescape(tokenPart)
		if err != nil || strings.TrimSpace(token) == "" {
			http.NotFound(w, r)
			return
		}
		m.proxySubscription(w, r, profile, token, maxBytes)
	})
	return mux
}

func (m *Manager) newHTTPServer(cfg conf.SubscriptionProxyConfig) *http.Server {
	if strings.TrimSpace(cfg.HTTPListen) == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	challengePrefix := "/.well-known/pki-validation/"
	mux.HandleFunc(challengePrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, challengePrefix)
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(cfg.ChallengeDir, name))
	})
	return &http.Server{
		Addr:              cfg.HTTPListen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (m *Manager) proxySubscription(w http.ResponseWriter, r *http.Request, profile conf.SubscriptionProxyProfile, token string, maxBytes int64) {
	upstreamURL, err := buildUpstreamURL(profile, token, r.URL.RawQuery)
	if err != nil {
		http.Error(w, "bad upstream", http.StatusBadGateway)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "bad upstream", http.StatusBadGateway)
		return
	}
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("X-Forwarded-Host", r.Host)
	if ip := clientIP(r); ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxBytes {
		http.Error(w, "upstream response too large", http.StatusBadGateway)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		http.Error(w, "upstream read failed", http.StatusBadGateway)
		return
	}
	if int64(len(body)) > maxBytes {
		http.Error(w, "upstream response too large", http.StatusBadGateway)
		return
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (m *Manager) prepareCertificate(cfg *conf.SubscriptionProxyConfig) Status {
	status := Status{
		CertificateDomain: strings.TrimSpace(cfg.CertificateDomain),
		CertificateID:     strings.TrimSpace(cfg.ZeroSSL.CertificateID),
		CertNotAfter:      certificateNotAfter(cfg.CertFile),
	}
	if strings.TrimSpace(cfg.ZeroSSL.ValidationPath) != "" && cfg.ZeroSSL.ValidationContent != nil {
		if err := writeValidationFile(cfg.ChallengeDir, cfg.ZeroSSL.ValidationPath, cfg.ZeroSSL.ValidationContent); err != nil {
			status.LastError = err.Error()
		} else {
			status.ValidationReady = true
		}
	}
	if strings.TrimSpace(cfg.ZeroSSL.CertificatePEM) != "" {
		if err := writeCertificateFile(cfg.CertFile, cfg.ZeroSSL.CertificatePEM, cfg.ZeroSSL.CABundlePEM); err != nil {
			status.LastError = err.Error()
		} else {
			status.CertNotAfter = certificateNotAfter(cfg.CertFile)
		}
	}
	if strings.TrimSpace(cfg.CertificateDomain) == "" {
		return status
	}

	csr, err := ensureKeyAndCSR(cfg.KeyFile, cfg.CertificateDomain)
	if err != nil {
		status.LastError = err.Error()
		return status
	}
	status.CSRPem = csr
	if !fileReadable(cfg.CertFile) || !fileReadable(cfg.KeyFile) {
		status.NeedCertificate = true
	}
	return status
}

func (m *Manager) mergeStatusLocked(next Status) {
	if next.CertificateDomain != "" {
		m.status.CertificateDomain = next.CertificateDomain
	}
	if next.CertificateID != "" {
		m.status.CertificateID = next.CertificateID
	}
	m.status.NeedCertificate = next.NeedCertificate
	m.status.CSRPem = next.CSRPem
	m.status.ValidationReady = next.ValidationReady
	if next.CertNotAfter != "" {
		m.status.CertNotAfter = next.CertNotAfter
	}
	if next.LastError != "" {
		m.status.LastError = next.LastError
	}
}

func normalizeConfig(cfg conf.SubscriptionProxyConfig) (conf.SubscriptionProxyConfig, error) {
	cfg.HTTPSListen = strings.TrimSpace(cfg.HTTPSListen)
	if cfg.HTTPSListen == "" {
		cfg.HTTPSListen = defaultHTTPSListen
	}
	cfg.HTTPListen = strings.TrimSpace(cfg.HTTPListen)
	cfg.CertFile = strings.TrimSpace(cfg.CertFile)
	cfg.KeyFile = strings.TrimSpace(cfg.KeyFile)
	cfg.CertificateDomain = strings.TrimSpace(cfg.CertificateDomain)
	cfg.ChallengeDir = strings.TrimSpace(cfg.ChallengeDir)
	if cfg.ChallengeDir == "" {
		cfg.ChallengeDir = defaultChallengeDir
	}
	cfg.SiteID = strings.TrimSpace(cfg.SiteID)
	cfg.UpstreamBaseURL = strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURL), "/")
	cfg.SubscribePath = strings.Trim(strings.TrimSpace(cfg.SubscribePath), "/")
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	if cfg.SiteID != "" || cfg.UpstreamBaseURL != "" {
		cfg.Profiles = append(cfg.Profiles, conf.SubscriptionProxyProfile{
			SiteID:          cfg.SiteID,
			UpstreamBaseURL: cfg.UpstreamBaseURL,
			SubscribePath:   cfg.SubscribePath,
		})
	}

	profiles := make([]conf.SubscriptionProxyProfile, 0, len(cfg.Profiles))
	seen := map[string]struct{}{}
	for _, profile := range cfg.Profiles {
		profile.SiteID = strings.TrimSpace(profile.SiteID)
		profile.UpstreamBaseURL = strings.TrimRight(strings.TrimSpace(profile.UpstreamBaseURL), "/")
		profile.SubscribePath = strings.Trim(strings.TrimSpace(profile.SubscribePath), "/")
		if profile.SubscribePath == "" {
			profile.SubscribePath = "s"
		}
		if profile.SiteID == "" || profile.UpstreamBaseURL == "" {
			continue
		}
		parsed, err := url.Parse(profile.UpstreamBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			if err != nil {
				return cfg, fmt.Errorf("invalid subscription proxy upstream for site %s: %w", profile.SiteID, err)
			}
			return cfg, fmt.Errorf("invalid subscription proxy upstream for site %s", profile.SiteID)
		}
		key := strings.ToLower(profile.SiteID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		profiles = append(profiles, profile)
	}
	cfg.Profiles = profiles
	if cfg.Enabled && len(cfg.Profiles) == 0 {
		return cfg, fmt.Errorf("subscription proxy enabled without profiles")
	}
	return cfg, nil
}

func buildUpstreamURL(profile conf.SubscriptionProxyProfile, token string, rawQuery string) (string, error) {
	base, err := url.Parse(profile.UpstreamBaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("invalid base url")
	}
	path := "/" + strings.Trim(profile.SubscribePath, "/") + "/" + url.PathEscape(token)
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = rawQuery
	return base.String(), nil
}

func fingerprint(cfg conf.SubscriptionProxyConfig) string {
	parts := []string{
		cfg.HTTPSListen,
		cfg.HTTPListen,
		cfg.CertFile,
		cfg.KeyFile,
		cfg.CertificateDomain,
		cfg.ChallengeDir,
		cfg.ZeroSSL.CertificateID,
		cfg.ZeroSSL.ValidationPath,
		validationContentString(cfg.ZeroSSL.ValidationContent),
		cfg.ZeroSSL.CertificatePEM,
		cfg.ZeroSSL.CABundlePEM,
		fmt.Sprintf("%t", cfg.AllowHTTPFallback),
		fmt.Sprintf("%d", cfg.MaxResponseBytes),
	}
	profiles := append([]conf.SubscriptionProxyProfile(nil), cfg.Profiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].SiteID < profiles[j].SiteID })
	for _, profile := range profiles {
		parts = append(parts, profile.SiteID, profile.UpstreamBaseURL, profile.SubscribePath)
	}
	return strings.Join(parts, "\x00")
}

func ensureKeyAndCSR(keyPath string, domain string) (string, error) {
	key, err := loadOrCreatePrivateKey(keyPath)
	if err != nil {
		return "", err
	}
	template := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: domain},
	}
	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{domain}
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &template, key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), nil
}

func loadOrCreatePrivateKey(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, fmt.Errorf("subscription proxy key file is empty")
	}
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("decode private key failed: %s", path)
		}
		if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			return key, nil
		}
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		key, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA: %s", path)
		}
		return key, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func writeValidationFile(root string, validationPath string, content any) error {
	name := filepath.Base(validationPath)
	if name == "." || name == "/" || name == "" {
		return fmt.Errorf("invalid validation path: %s", validationPath)
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, name), []byte(validationContentString(content)), 0644)
}

func writeCertificateFile(path string, certificate string, caBundle string) error {
	if path == "" {
		return fmt.Errorf("subscription proxy cert file is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	fullchain := strings.TrimSpace(certificate)
	if strings.TrimSpace(caBundle) != "" {
		fullchain += "\n" + strings.TrimSpace(caBundle)
	}
	fullchain += "\n"
	return os.WriteFile(path, []byte(fullchain), 0644)
}

func validationContentString(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v) + "\n"
	case []string:
		return strings.Join(v, "\n") + "\n"
	case []any:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, strings.TrimSpace(fmt.Sprint(item)))
		}
		return strings.Join(lines, "\n") + "\n"
	default:
		return strings.TrimSpace(fmt.Sprint(v)) + "\n"
	}
}

func certificateNotAfter(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return ""
	}
	return cert.NotAfter.Format(time.RFC3339)
}

func fileReadable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyRequestHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) || strings.EqualFold(key, "Host") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

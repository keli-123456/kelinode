package subproxy

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keli-123456/kelinode/conf"
)

func TestProxySubscriptionForwardsToProfileSubscribePath(t *testing.T) {
	manager := NewManager()
	manager.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/answer/land/token-123" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("flag"); got != "sing-box" {
			t.Fatalf("unexpected query flag: %s", got)
		}
		if got := r.Header.Get("User-Agent"); got != "Hiddify" {
			t.Fatalf("unexpected user agent: %s", got)
		}
		if got := r.Header.Get("CF-Connecting-IP"); got != "203.0.113.9" {
			t.Fatalf("unexpected cf connecting ip: %s", got)
		}
		if got := r.Header.Get("X-Forwarded-For"); got != "203.0.113.9, 198.51.100.8" {
			t.Fatalf("unexpected x-forwarded-for: %s", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Subscription-Userinfo": []string{"upload=0; download=1"},
			},
			Body: io.NopCloser(strings.NewReader("ok")),
		}, nil
	})}

	handler := manager.handler(map[string]conf.SubscriptionProxyProfile{
		"site-a": {
			SiteID:          "site-a",
			UpstreamBaseURL: "https://panel.example.test",
			SubscribePath:   "answer/land",
		},
	}, defaultMaxResponseBytes)

	req := httptest.NewRequest(http.MethodGet, "/sub/site-a/token-123?flag=sing-box", nil)
	req.Header.Set("User-Agent", "Hiddify")
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.RemoteAddr = "198.51.100.8:51234"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("unexpected body: %s", got)
	}
	if got := rec.Header().Get("Subscription-Userinfo"); got != "upload=0; download=1" {
		t.Fatalf("missing response header: %s", got)
	}
}

func TestNormalizeConfigUsesDetectedIPv4ForIPv6CertificateDomain(t *testing.T) {
	oldDetect := detectPublicIPv4Address
	detectPublicIPv4Address = func(context.Context) (string, error) {
		return "203.0.113.10", nil
	}
	defer func() {
		detectPublicIPv4Address = oldDetect
	}()

	cfg, err := normalizeConfig(conf.SubscriptionProxyConfig{
		Enabled:           true,
		CertificateDomain: "2400:8901::2000:49ff:fe93:6d50",
		SiteID:            "site-a",
		UpstreamBaseURL:   "https://panel.example.com",
	})
	if err != nil {
		t.Fatalf("normalize config failed: %v", err)
	}
	if cfg.CertificateDomain != "203.0.113.10" {
		t.Fatalf("unexpected certificate domain: %s", cfg.CertificateDomain)
	}
}

func TestNormalizeConfigKeepsConfiguredIPv4CertificateDomain(t *testing.T) {
	oldDetect := detectPublicIPv4Address
	detectPublicIPv4Address = func(context.Context) (string, error) {
		t.Fatal("public IPv4 detection should not be called for configured IPv4")
		return "", nil
	}
	defer func() {
		detectPublicIPv4Address = oldDetect
	}()

	cfg, err := normalizeConfig(conf.SubscriptionProxyConfig{
		Enabled:           true,
		CertificateDomain: "152.53.135.140",
		SiteID:            "site-a",
		UpstreamBaseURL:   "https://panel.example.com",
	})
	if err != nil {
		t.Fatalf("normalize config failed: %v", err)
	}
	if cfg.CertificateDomain != "152.53.135.140" {
		t.Fatalf("unexpected certificate domain: %s", cfg.CertificateDomain)
	}
}

func TestNormalizeConfigKeepsConfiguredHostnameCertificateDomain(t *testing.T) {
	oldDetect := detectPublicIPv4Address
	detectPublicIPv4Address = func(context.Context) (string, error) {
		t.Fatal("public IPv4 detection should not be called for configured hostname")
		return "", nil
	}
	defer func() {
		detectPublicIPv4Address = oldDetect
	}()

	cfg, err := normalizeConfig(conf.SubscriptionProxyConfig{
		Enabled:           true,
		CertificateDomain: "sub.example.com",
		SiteID:            "site-a",
		UpstreamBaseURL:   "https://panel.example.com",
	})
	if err != nil {
		t.Fatalf("normalize config failed: %v", err)
	}
	if cfg.CertificateDomain != "sub.example.com" {
		t.Fatalf("unexpected certificate domain: %s", cfg.CertificateDomain)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNormalizeConfigBuildsSingleProfileFromPanelShape(t *testing.T) {
	cfg, err := normalizeConfig(conf.SubscriptionProxyConfig{
		Enabled:         true,
		SiteID:          "site-a",
		UpstreamBaseURL: "https://panel.example.com/",
		SubscribePath:   "/s/",
	})
	if err != nil {
		t.Fatalf("normalize config failed: %v", err)
	}
	if cfg.HTTPSListen != defaultHTTPSListen {
		t.Fatalf("unexpected default listen: %s", cfg.HTTPSListen)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("unexpected profiles: %+v", cfg.Profiles)
	}
	if got := cfg.Profiles[0]; got.SiteID != "site-a" || got.UpstreamBaseURL != "https://panel.example.com" || got.SubscribePath != "s" {
		t.Fatalf("unexpected profile: %+v", got)
	}
}

func TestSubscriptionProxyCertificateOwnerSiteIDUsesFirstProfile(t *testing.T) {
	owner := subscriptionProxyCertificateOwnerSiteID([]conf.SubscriptionProxyProfile{
		{SiteID: "site-a"},
		{SiteID: "site-b"},
	})
	if owner != "site-a" {
		t.Fatalf("unexpected owner site id: %s", owner)
	}
}

func TestLoadSubscriptionProxyCertificateChainIncludesIntermediatesAndTLS12(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "fullchain.pem")
	keyPath := filepath.Join(dir, "private.key")
	leafPEM, caPEM, keyPEM := testCertificateChain(t)
	if err := os.WriteFile(certPath, append(leafPEM, caPEM...), 0644); err != nil {
		t.Fatalf("write cert failed: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key failed: %v", err)
	}

	cert, err := loadSubscriptionProxyCertificateChain(certPath, keyPath)
	if err != nil {
		t.Fatalf("load certificate chain failed: %v", err)
	}
	if len(cert.Certificate) != 2 {
		t.Fatalf("expected full chain with leaf and intermediate, got %d certificate(s)", len(cert.Certificate))
	}

	tlsConfig := subscriptionProxyTLSConfig(cert)
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("unexpected minimum TLS version: %d", tlsConfig.MinVersion)
	}
	if len(tlsConfig.Certificates) != 1 || len(tlsConfig.Certificates[0].Certificate) != 2 {
		t.Fatalf("TLS config did not retain full certificate chain: %+v", tlsConfig.Certificates)
	}
}

func TestWriteCertificateFileMergesLeafAndCABundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fullchain.pem")
	leaf := "-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----"
	caBundle := "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----"

	if err := writeCertificateFile(path, leaf, caBundle); err != nil {
		t.Fatalf("write certificate file failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read certificate file failed: %v", err)
	}
	want := leaf + "\n" + caBundle + "\n"
	if string(data) != want {
		t.Fatalf("unexpected fullchain content:\n%s", string(data))
	}
}

func TestWriteCertificateFileAppendsSectigoR46CompatibilityChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fullchain.pem")
	leafPEM, intermediatePEM := testSectigoR46IssuedChain(t)

	if err := writeCertificateFile(path, string(leafPEM), string(intermediatePEM)); err != nil {
		t.Fatalf("write certificate file failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read certificate file failed: %v", err)
	}
	if got := strings.Count(string(data), "BEGIN CERTIFICATE"); got != 3 {
		t.Fatalf("expected leaf, intermediate, and compatibility certificate, got %d certificate(s)", got)
	}
	certs, err := parsePEMCertificates(data)
	if err != nil {
		t.Fatalf("parse fullchain failed: %v", err)
	}
	if got := certs[1].Subject.CommonName; got != "ZeroSSL RSA DV SSL CA 2" {
		t.Fatalf("unexpected intermediate subject: %s", got)
	}
	if got := certs[2].Subject.CommonName; got != "Sectigo Public Server Authentication Root R46" {
		t.Fatalf("unexpected compatibility subject: %s", got)
	}
	if got := certs[2].Issuer.CommonName; got != "USERTrust RSA Certification Authority" {
		t.Fatalf("unexpected compatibility issuer: %s", got)
	}

	pathWithCompat := filepath.Join(t.TempDir(), "fullchain.pem")
	if err := writeCertificateFile(pathWithCompat, string(leafPEM), string(intermediatePEM)+"\n"+sectigoR46UserTrustCrossSignedPEM); err != nil {
		t.Fatalf("write certificate file with compatibility certificate failed: %v", err)
	}
	dataWithCompat, err := os.ReadFile(pathWithCompat)
	if err != nil {
		t.Fatalf("read certificate file with compatibility certificate failed: %v", err)
	}
	if got := strings.Count(string(dataWithCompat), "BEGIN CERTIFICATE"); got != 3 {
		t.Fatalf("compatibility certificate was duplicated, got %d certificate(s)", got)
	}
}

func testCertificateChain(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key failed: %v", err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key failed: %v", err)
	}

	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ZeroSSL Test Intermediate"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca certificate failed: %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "203.0.113.10"},
		IPAddresses:           []net.IP{net.ParseIP("203.0.113.10")},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate failed: %v", err)
	}

	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
	return leafPEM, caPEM, keyPEM
}

func testSectigoR46IssuedChain(t *testing.T) ([]byte, []byte) {
	t.Helper()
	r46Key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate r46 key failed: %v", err)
	}
	zeroSSLKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate zerossl key failed: %v", err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate leaf key failed: %v", err)
	}

	now := time.Now()
	r46Template := &x509.Certificate{
		SerialNumber:          big.NewInt(10),
		Subject:               pkix.Name{CommonName: "Sectigo Public Server Authentication Root R46"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	zeroSSLTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(11),
		Subject:               pkix.Name{CommonName: "ZeroSSL RSA DV SSL CA 2"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	zeroSSLDER, err := x509.CreateCertificate(rand.Reader, zeroSSLTemplate, r46Template, &zeroSSLKey.PublicKey, r46Key)
	if err != nil {
		t.Fatalf("create zerossl certificate failed: %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(12),
		Subject:               pkix.Name{CommonName: "103.14.76.98"},
		IPAddresses:           []net.IP{net.ParseIP("103.14.76.98")},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, zeroSSLTemplate, &leafKey.PublicKey, zeroSSLKey)
	if err != nil {
		t.Fatalf("create leaf certificate failed: %v", err)
	}

	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	intermediatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: zeroSSLDER})
	return leafPEM, intermediatePEM
}

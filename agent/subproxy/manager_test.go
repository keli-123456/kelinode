package subproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

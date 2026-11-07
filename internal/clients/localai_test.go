package clients

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"

	laiclient "github.com/rossigee/localai-client/client"
)

func TestNewLocalAIHTTPClientRejectsMissingAuth(t *testing.T) {
	if _, err := NewLocalAIHTTPClient(context.Background(), laiclient.Config{BaseURL: "https://localai.example"}); err == nil {
		t.Fatal("expected an error when neither an API key nor OAuth2 credentials are configured")
	}
}

func TestNewLocalAIHTTPClientRejectsBadCA(t *testing.T) {
	cfg := laiclient.Config{APIKey: "k", RootCACertificate: "not a certificate"}
	if _, err := NewLocalAIHTTPClient(context.Background(), cfg); err == nil {
		t.Fatal("expected an error for a CA bundle with no PEM certificates")
	}
}

func TestNewLocalAIHTTPClientAPIKeyKeepsVerificationOn(t *testing.T) {
	hc, err := NewLocalAIHTTPClient(context.Background(), laiclient.Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewLocalAIHTTPClient: %v", err)
	}
	tr, ok := hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", hc.Transport)
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", tr.TLSClientConfig.MinVersion)
	}
	if tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("certificate verification must never be skipped")
	}
}

func TestNewLocalAIHTTPClientOAuthReturnsClient(t *testing.T) {
	cfg := laiclient.Config{TokenURL: "https://sso.example/token", ClientID: "id", ClientSecret: "secret"}
	hc, err := NewLocalAIHTTPClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewLocalAIHTTPClient: %v", err)
	}
	if hc == nil {
		t.Fatal("expected a non-nil client for OAuth2 credentials")
	}
}

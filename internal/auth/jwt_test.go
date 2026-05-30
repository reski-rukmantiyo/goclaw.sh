package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWTManager_IssueAndValidate(t *testing.T) {
	mgr, err := NewJWTManager("goclaw", "goclaw-api", 8*time.Hour)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())

	token, err := mgr.IssueAccessToken(userID, "user@test.com", tenantID, "member")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}

	claims, err := mgr.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Subject != userID.String() {
		t.Errorf("sub: got %q, want %q", claims.Subject, userID.String())
	}
	if claims.Email != "user@test.com" {
		t.Errorf("email: got %q, want %q", claims.Email, "user@test.com")
	}
	if claims.TID != tenantID.String() {
		t.Errorf("tid: got %q, want %q", claims.TID, tenantID.String())
	}
	if claims.Role != "member" {
		t.Errorf("role: got %q, want %q", claims.Role, "member")
	}
	if claims.Issuer != "goclaw" {
		t.Errorf("iss: got %q, want %q", claims.Issuer, "goclaw")
	}
}

func TestJWTManager_ExpiredToken(t *testing.T) {
	mgr, err := NewJWTManager("goclaw", "goclaw-api", -1*time.Second)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())

	token, err := mgr.IssueAccessToken(userID, "user@test.com", tenantID, "member")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	_, err = mgr.ValidateToken(token)
	if err == nil {
		t.Fatal("should reject expired token")
	}
}

func TestJWTManager_InvalidSignature(t *testing.T) {
	mgr1, _ := NewJWTManager("goclaw", "goclaw-api", 8*time.Hour)
	mgr2, _ := NewJWTManager("goclaw", "goclaw-api", 8*time.Hour)

	userID := uuid.Must(uuid.NewV7())
	tenantID := uuid.Must(uuid.NewV7())

	token, _ := mgr1.IssueAccessToken(userID, "user@test.com", tenantID, "member")

	_, err := mgr2.ValidateToken(token)
	if err == nil {
		t.Fatal("should reject token signed by different key")
	}
}

func TestJWTManager_PublicKey(t *testing.T) {
	mgr, _ := NewJWTManager("goclaw", "goclaw-api", 8*time.Hour)
	pub := mgr.PublicKey()
	if pub == nil {
		t.Fatal("PublicKey should not be nil")
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	p := NewEntraIDProvider("client123", "secret", "https://app.example.com/callback")
	url := BuildAuthorizeURL(p, "random-state")

	if url == "" {
		t.Fatal("authorize URL should not be empty")
	}
	if !contains(url, "client_id=client123") {
		t.Error("should contain client_id")
	}
	if !contains(url, "state=random-state") {
		t.Error("should contain state")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

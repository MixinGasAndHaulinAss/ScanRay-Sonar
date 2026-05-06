package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestApiKeyAllowsSite_CrossSiteReject verifies that an API key bound to one
// site is denied access to a different site, that an unrestricted key passes,
// and that a JWT session (no scope in context) passes.
func TestApiKeyAllowsSite_CrossSiteReject(t *testing.T) {
	siteA := uuid.New()
	siteB := uuid.New()

	scoped := &apiKeySiteScope{
		Restrict: true,
		Sites:    map[uuid.UUID]struct{}{siteA: {}},
	}
	scopedCtx := context.WithValue(context.Background(), ctxAPIKeySiteScope, scoped)

	if !apiKeyAllowsSite(scopedCtx, siteA) {
		t.Fatalf("scoped key should allow its own site")
	}
	if apiKeyAllowsSite(scopedCtx, siteB) {
		t.Fatalf("scoped key must reject a different site")
	}

	unrestricted := &apiKeySiteScope{Restrict: false}
	uctx := context.WithValue(context.Background(), ctxAPIKeySiteScope, unrestricted)
	if !apiKeyAllowsSite(uctx, siteB) {
		t.Fatalf("unrestricted key should allow any site")
	}

	// JWT path: no scope value in context.
	if !apiKeyAllowsSite(context.Background(), siteB) {
		t.Fatalf("JWT session (no scope) should allow any site")
	}
}

func TestAppendAPISiteFilter_RestrictedKeyAddsFilter(t *testing.T) {
	siteA := uuid.New()
	scope := &apiKeySiteScope{
		Restrict: true,
		Sites:    map[uuid.UUID]struct{}{siteA: {}},
	}
	ctx := context.WithValue(context.Background(), ctxAPIKeySiteScope, scope)
	args := []any{}
	q := appendAPISiteFilter("SELECT * FROM t WHERE 1=1", &args, ctx, "site_id")
	if len(args) != 1 {
		t.Fatalf("expected 1 arg appended, got %d", len(args))
	}
	if q == "SELECT * FROM t WHERE 1=1" {
		t.Fatalf("expected query to be modified")
	}
}

func TestAppendAPISiteFilter_EmptyScopedSetReturnsFalse(t *testing.T) {
	scope := &apiKeySiteScope{
		Restrict: true,
		Sites:    map[uuid.UUID]struct{}{},
	}
	ctx := context.WithValue(context.Background(), ctxAPIKeySiteScope, scope)
	args := []any{}
	q := appendAPISiteFilter("SELECT * FROM t WHERE 1=1", &args, ctx, "site_id")
	if q != "SELECT * FROM t WHERE 1=1 AND FALSE" {
		t.Fatalf("expected AND FALSE for empty scoped key, got %q", q)
	}
	if len(args) != 0 {
		t.Fatalf("expected 0 args for AND FALSE, got %d", len(args))
	}
}

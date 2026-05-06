package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
)

type apiKeySiteScope struct {
	Restrict bool
	Sites    map[uuid.UUID]struct{}
}

func apiKeySiteScopeFromCtx(ctx context.Context) *apiKeySiteScope {
	v, _ := ctx.Value(ctxAPIKeySiteScope).(*apiKeySiteScope)
	return v
}

// apiKeyAllowsSite returns true for JWT sessions (no scope) and for API keys
// when the site is unrestricted or explicitly allowed.
func apiKeyAllowsSite(ctx context.Context, site uuid.UUID) bool {
	sc := apiKeySiteScopeFromCtx(ctx)
	if sc == nil {
		return true
	}
	if !sc.Restrict {
		return true
	}
	_, ok := sc.Sites[site]
	return ok
}

// appendAPISiteFilter adds AND site column ∈ allowed API-key sites when restricted.
func appendAPISiteFilter(q string, args *[]any, ctx context.Context, siteColumn string) string {
	sc := apiKeySiteScopeFromCtx(ctx)
	if sc == nil || !sc.Restrict {
		return q
	}
	if len(sc.Sites) == 0 {
		return q + " AND FALSE"
	}
	ids := make([]string, 0, len(sc.Sites))
	for id := range sc.Sites {
		ids = append(ids, id.String())
	}
	n := len(*args) + 1
	*args = append(*args, ids)
	return fmt.Sprintf("%s AND %s::text = ANY($%s::text[])", q, siteColumn, strconv.Itoa(n))
}

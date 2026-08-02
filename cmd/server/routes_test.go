package main

import (
	"net/http"
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestProductionHTMLRouteManifestRegistersWithoutConflict(t *testing.T) {
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)
	h.BasePath = "/dashboard"

	outer := http.NewServeMux()
	h.RegisterAuthRoutes(outer, func(next http.Handler) http.Handler { return next })
	h.RegisterPublicRoutes(outer, func(next http.Handler) http.Handler { return next })

	protected := http.NewServeMux()
	h.RegisterProtectedRoutes(protected)
}

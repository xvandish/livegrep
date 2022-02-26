package middleware

import (
	"fmt"
	"net/http"

	"golang.org/x/net/context"
	// "google.golang.org/api/idtoken"

	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/log"
)

type iapHandler struct {
	inner http.Handler
	cfg   *config.GoogleIAPConfig
}

func ShouldEnableGoogleIAP(cfg config.GoogleIAPConfig) bool {
	ctx := context.Background()
	if cfg.ProjectNumber == "" {
		return false
	}

	if cfg.BackendServiceID == "" && cfg.ProjectID == "" {
		log.Printf(ctx, "GoogleIAPConfig: ProjectNumber provided but no BackendServiceID or ProjectID found. Not enabling.")
		return false
	}

	if cfg.BackendServiceID != "" && cfg.ProjectID != "" {
		log.Printf(ctx, "GoogleIAPConfig: BackendServiceID and ProjectID are mutually exclusive. Not enabling.")
		return false
	}

	return true
}

func (h *iapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	// GKE and GCE health checks don't use JWT headers, so skip validation
	if r.URL.Path == "/healthz" {
		h.inner.ServeHTTP(w, r)
		return
	}

	iapJWT := r.Header.Get("x-goog-iap-jwt-assertion")
	var aud string
	if h.cfg.BackendServiceID != "" { // GKE or GCE
		aud = fmt.Sprintf("/projects/%s/global/backendServices/%s", h.cfg.ProjectNumber, h.cfg.BackendServiceID)
	} else { // GAE
		aud = fmt.Sprintf("/projects/%s/apps/%s", h.cfg.ProjectNumber, h.cfg.ProjectID)
	}

	// _, err := idtoken.Validate(ctx, iapJWT, aud)
	log.Printf(ctx, "got aud: %s and iapJWT: %s\n", aud, iapJWT)

	// if err != nil {
	// 	log.Errorf("idtoken.Validate: %v", err)
	// 	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	// 	return
	// }

	h.inner.ServeHTTP(w, r)
}

func WrapWithIAP(h http.Handler, cfg config.GoogleIAPConfig) http.Handler {
	return &iapHandler{h, &cfg}
}

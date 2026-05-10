package v1

import (
	"io/fs"
	"net/http"

	latasyaerp "github.com/naufal/latasya-erp"
)

func ServeOpenAPI(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(latasyaerp.OpenAPIFS, "api/openapi.yaml")
	if err != nil {
		http.Error(w, "openapi spec not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

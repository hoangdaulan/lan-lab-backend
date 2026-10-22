package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSwaggerServesUIAssetsAndSpecification(t *testing.T) {
	r := NewRouter(nil) // Documentation does not require a database connection.
	for _, path := range []string{"/swagger/index.html", "/swagger/swagger-ui.css", "/swagger/swagger-ui-bundle.js", "/swagger/doc.json"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("GET %s: status=%d size=%d", path, w.Code, w.Body.Len())
		}
		if strings.HasSuffix(path, "doc.json") {
			var spec struct {
				Host  string                                `json:"host"`
				Paths map[string]map[string]json.RawMessage `json:"paths"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
				t.Fatal(err)
			}
			if spec.Host != "" {
				t.Fatal("Swagger must use the current LAN host")
			}
			count := 0
			for _, methods := range spec.Paths {
				count += len(methods)
			}
			if count != 32 {
				t.Fatalf("expected 32 implemented operations, got %d", count)
			}
			if !strings.Contains(string(spec.Paths["/api/v1/students/import"]["post"]), "multipart/form-data") {
				t.Fatal("CSV upload must use multipart form data")
			}
			if _, exists := spec.Paths["/api/v1/ws"]; exists {
				t.Fatal("unimplemented websocket must not be advertised")
			}
		}
	}
}

package student

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestListHandlerUsesMesgoPaginationAndQuery(t *testing.T) {
	svc := NewService(newFakeRepo(), fakeHasher{})
	for _, code := range []string{"SV01", "SV02", "SV03", "XX01"} {
		if _, err := svc.Create(context.Background(), code, "Student "+code, testDOB); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	h.list(w, httptest.NewRequest("GET", "/students?page=2&pageSize=2&query=SV", nil))
	var got struct {
		Data       []studentResponse `json:"data"`
		Page       int               `json:"page"`
		PageSize   int               `json:"pageSize"`
		TotalPages int               `json:"totalPages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.Page != 2 || got.PageSize != 2 || got.TotalPages != 2 || len(got.Data) != 1 || got.Data[0].Code != "SV03" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "limit", "offset", "total"} {
		if _, exists := raw[key]; exists {
			t.Fatalf("legacy field %q present", key)
		}
	}
}

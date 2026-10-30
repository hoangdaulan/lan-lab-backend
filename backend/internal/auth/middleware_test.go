package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticateAndRequireRole(t *testing.T) {
	now := time.Now()
	signer := NewJWTSigner(testSecret, 15*time.Minute)
	studentTok, _, _ := signer.Issue(Principal{ID: 7, Role: RoleStudent}, now)
	teacherTok, _, _ := signer.Issue(Principal{ID: 1, Role: RoleTeacher}, now)
	svc := NewService(newFakeRepo(), signer, time.Hour)

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, found := PrincipalFrom(r.Context()); !found {
			t.Error("principal missing in handler")
		}
		w.WriteHeader(http.StatusOK)
	})
	teacherOnly := Authenticate(svc)(RequireRole(RoleTeacher)(ok))

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"not bearer", "Basic abc", http.StatusUnauthorized},
		{"garbage token", "Bearer abc.def.ghi", http.StatusUnauthorized},
		{"student on teacher route", "Bearer " + studentTok, http.StatusForbidden},
		{"teacher", "Bearer " + teacherTok, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			teacherOnly.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

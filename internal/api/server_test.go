package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRootRejectsNonGet(t *testing.T) {
	s := &Server{}
	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, APIPrefix+"/config/text", nil)
			rec := httptest.NewRecorder()
			s.handleRoot(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
				t.Fatalf("allow = %q", allow)
			}
		})
	}
}

func TestHandleRootServesAdmin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	(&Server{}).handleRoot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("admin page is empty")
	}
}

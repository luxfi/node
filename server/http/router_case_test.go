package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A chain answers to either spelling of its alias.
func TestRouteFoldsCase(t *testing.T) {
	r := newRouter()
	hit := 0
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit++ })
	if err := r.AddRouter(Chain("", "C"), "/rpc", h); err != nil {
		t.Fatal(err)
	}
	upper := Chain("", "C") + "/rpc"
	for _, path := range []string{upper, strings.ToLower(upper), strings.ToUpper(upper)} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", path, w.Code)
		}
	}
	if hit != 3 {
		t.Errorf("handler ran %d times, want 3", hit)
	}
	// A path that names no route is still a 404.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, Chain("", "D")+"/rpc", nil))
	if w.Code == http.StatusOK {
		t.Error("an unregistered chain answered 200; case folding must not invent routes")
	}
}

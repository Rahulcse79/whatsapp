package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInitExposesMetrics(t *testing.T) {
	tel, err := Init(context.Background(), Config{ServiceName: "test-svc", ServiceVersion: "0.0.0", Env: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()

	hm, err := NewHTTPMetrics(tel.Meter)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise the RED middleware so the instruments carry data + labels.
	h := hm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "http_request_duration") {
		t.Fatalf("/metrics missing the RED histogram:\n%s", body)
	}
}

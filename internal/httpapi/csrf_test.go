package httpapi

import (
	"net/http"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	sameOrigin, err := SameOrigin("https://tasks.example.com")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := Register(mux, Server{}, discardLogger, sameOrigin); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		allowed bool
	}{
		{"same origin per Sec-Fetch-Site", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"cross site per Sec-Fetch-Site", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		// Another subdomain of the same site is still another origin.
		{"same site per Sec-Fetch-Site", http.MethodDelete, map[string]string{"Sec-Fetch-Site": "same-site"}, false},
		{"other origin, older browser", http.MethodDelete, map[string]string{"Origin": "https://evil.example.com"}, false},
		{"public origin, older browser", http.MethodDelete, map[string]string{"Origin": "https://tasks.example.com"}, true},
		{"no browser headers (curl)", http.MethodDelete, nil, true},
		{"cross-site GET is safe", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Both operations are still stubs, so an allowed request gets 501.
			path := "https://tasks.example.com/api/v1/lists"
			if tt.method == http.MethodDelete {
				path = "https://tasks.example.com/api/v1/tasks/" + someID
			}
			req := newRequest(t, tt.method, path, "", "")
			for name, value := range tt.headers {
				req.Header.Set(name, value)
			}
			rec := serve(t, mux, req)
			if tt.allowed {
				decodeProblem(t, rec, http.StatusNotImplemented, ProblemCodeNotImplemented)
			} else {
				decodeProblem(t, rec, http.StatusForbidden, ProblemCodeForbidden)
			}
		})
	}
}

func TestSameOriginRejectsBadPublicURL(t *testing.T) {
	if _, err := SameOrigin("not a url"); err == nil {
		t.Error("SameOrigin accepted an invalid origin")
	}
}

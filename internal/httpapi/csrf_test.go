package httpapi

import (
	"context"
	"net/http"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	sameOrigin, err := SameOrigin("https://tasks.example.com")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := Register(mux, stubServer{}, discardLogger, sameOrigin); err != nil {
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
			// stubServer answers both operations with 501, so an allowed
			// request gets 501 and a refused one 403.
			path := "https://tasks.example.com/api/v1/push/subscriptions"
			if tt.method == http.MethodDelete {
				path = "https://tasks.example.com/api/v1/push/subscriptions/" + someID
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

// stubServer answers the two operations TestSameOrigin calls with a fixed
// 501 problem, without a database. It embeds Server for the rest of the
// interface (Go "embedding": the methods of the field are promoted).
type stubServer struct {
	Server
}

func (stubServer) ListPushSubscriptions(context.Context, ListPushSubscriptionsRequestObject) (ListPushSubscriptionsResponseObject, error) {
	return ListPushSubscriptionsdefaultApplicationProblemPlusJSONResponse(stubProblem()), nil
}

func (stubServer) DeletePushSubscription(context.Context, DeletePushSubscriptionRequestObject) (DeletePushSubscriptionResponseObject, error) {
	return DeletePushSubscriptiondefaultApplicationProblemPlusJSONResponse(stubProblem()), nil
}

// stubProblem is a recognizable answer for handlers that only need to be
// reached.
func stubProblem() problemResponse {
	return toResponse(newProblem(http.StatusNotImplemented, ProblemCodeNotImplemented, ""))
}

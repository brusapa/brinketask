package webpush_test

// Reproduction of finding H2 in REVIEW.md. It fails until the finding is
// fixed.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/webpush"
)

// H2 (SPEC section 10, SSRF): the server posts to endpoints users chose, so
// a push service's redirect must not take the request elsewhere, e.g. to
// an internal address that the endpoint check would have refused. The
// client is built as cmd/brinketask/main.go builds it.
func TestReviewH2SendDoesNotFollowRedirects(t *testing.T) {
	_, service := newSender(t)
	sub := service.NewDevice(t, "phone") // only for valid subscription keys

	var reached atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
	}))
	defer internal.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusFound)
	}))
	defer redirector.Close()

	public, private, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := webpush.ParseKeys(public, private)
	if err != nil {
		t.Fatal(err)
	}
	sender := webpush.NewSender(keys, "mailto:ops@example.com", &http.Client{Timeout: 30 * time.Second}, clock.NewFixed(time.Now()))
	sub.Endpoint = redirector.URL + "/phone"
	_ = sender.Send(context.Background(), sub, []byte("x"))
	if reached.Load() != 0 {
		t.Error("the sender followed the push service's redirect to another host")
	}
}

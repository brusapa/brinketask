package webpush_test

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

// senderWith is a sender that posts through client, with keys of its own;
// the servers in these tests check no signature.
func senderWith(t *testing.T, client *http.Client) *webpush.Sender {
	t.Helper()
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := webpush.ParseKeys(public, private)
	if err != nil {
		t.Fatal(err)
	}
	return webpush.NewSender(keys, "mailto:ops@example.com", client, clock.NewFixed(time.Now()))
}

// counter is a server that counts the requests it receives.
func counter(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if handler != nil {
			handler(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &n
}

// SPEC section 10 (SSRF): a push service's redirect does not take the
// request elsewhere; the attempt fails instead.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	_, service := newSender(t)
	sub := service.NewDevice(t, "phone") // for valid subscription keys
	internal, reached := counter(t, nil)
	redirector, _ := counter(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/admin", http.StatusFound)
	})

	// Loopback allowed: the test servers listen on 127.0.0.1.
	sender := senderWith(t, webpush.NewClient(5*time.Second, true))
	sub.Endpoint = redirector.URL + "/phone"
	if err := sender.Send(context.Background(), sub, []byte("x")); err == nil {
		t.Error("a redirect counted as a success")
	}
	if reached.Load() != 0 {
		t.Error("the redirect was followed")
	}
}

// SPEC section 10 (SSRF): the client does not connect to this machine,
// however the endpoint names it, unless the end-to-end test allows it
// (D-73).
func TestClientRefusesLoopback(t *testing.T) {
	_, service := newSender(t)
	sub := service.NewDevice(t, "phone")
	server, reached := counter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	sub.Endpoint = server.URL + "/phone"

	if err := senderWith(t, webpush.NewClient(5*time.Second, false)).Send(context.Background(), sub, []byte("x")); err == nil {
		t.Error("sent to a loopback address")
	}
	if reached.Load() != 0 {
		t.Error("the loopback server was reached")
	}
	if err := senderWith(t, webpush.NewClient(5*time.Second, true)).Send(context.Background(), sub, []byte("x")); err != nil {
		t.Errorf("with loopback allowed: %v", err)
	}
}

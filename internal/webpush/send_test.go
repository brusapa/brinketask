// An external test package (webpush_test) can use webpushtest, which
// itself imports webpush; package webpush could not without a cycle.
package webpush_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/brusapa/brinketask/internal/clock"
	"github.com/brusapa/brinketask/internal/webpush"
	"github.com/brusapa/brinketask/internal/webpush/webpushtest"
)

func newSender(t *testing.T) (*webpush.Sender, *webpushtest.Service) {
	t.Helper()
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := webpush.ParseKeys(public, private)
	if err != nil {
		t.Fatal(err)
	}
	service := webpushtest.New(t, public)
	return webpush.NewSender(keys, "mailto:ops@example.com", http.DefaultClient, clock.NewFixed(time.Now())), service
}

// A message reaches the device signed (RFC 8292) and readable (RFC 8291).
func TestSendIsSignedAndDecryptable(t *testing.T) {
	sender, service := newSender(t)
	sub := service.NewDevice(t, "phone")
	if err := sender.Send(context.Background(), sub, []byte(`{"title":"Call mum"}`)); err != nil {
		t.Fatal(err)
	}
	messages := service.Messages()
	if len(messages) != 1 || string(messages[0].Payload) != `{"title":"Call mum"}` || messages[0].Device != "phone" {
		t.Errorf("messages = %+v", messages)
	}
	if messages[0].TTL != "86400" {
		t.Errorf("TTL = %q", messages[0].TTL)
	}
	if f := service.Failures(); len(f) != 0 {
		t.Errorf("failures: %v", f)
	}
}

// SPEC section 6: 404 and 410 mean the subscription is gone; anything
// else that fails is worth a retry.
func TestSendStatuses(t *testing.T) {
	sender, service := newSender(t)
	sub := service.NewDevice(t, "phone")
	ctx := context.Background()
	for _, tt := range []struct {
		status int
		gone   bool
	}{
		{http.StatusGone, true},
		{http.StatusNotFound, true},
		{http.StatusInternalServerError, false},
		{http.StatusTooManyRequests, false},
	} {
		service.Answer("phone", tt.status)
		err := sender.Send(ctx, sub, []byte("x"))
		if err == nil || errors.Is(err, webpush.ErrGone) != tt.gone {
			t.Errorf("status %d: err = %v, gone %v", tt.status, err, tt.gone)
		}
	}
	// An unknown device is gone too.
	other := sub
	other.Endpoint += "-unknown"
	if err := sender.Send(ctx, other, []byte("x")); !errors.Is(err, webpush.ErrGone) {
		t.Errorf("unknown device: %v", err)
	}
}

// The fake service refuses a message signed with another server's keys.
func TestFakeServiceChecksTheSignature(t *testing.T) {
	_, service := newSender(t)
	stranger, _ := newSender(t)
	sub := service.NewDevice(t, "phone")
	if err := stranger.Send(context.Background(), sub, []byte("x")); err == nil {
		t.Error("a message signed with other keys was accepted")
	}
	if len(service.Failures()) != 1 {
		t.Errorf("failures = %v", service.Failures())
	}
}

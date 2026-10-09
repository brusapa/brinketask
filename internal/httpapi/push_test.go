package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/brusapa/brinketask/internal/webpush"
)

func deviceBody(id uuid.UUID, sub webpush.Subscription, label string) string {
	return fmt.Sprintf(`{"id":%q,"channel":"webpush","endpoint":%q,"keys":{"p256dh":%q,"auth":%q},"label":%q}`,
		id, sub.Endpoint, sub.P256dh, sub.Auth, label)
}

func (a *testApp) register(t *testing.T, u user, id uuid.UUID, sub webpush.Subscription, status int) PushSubscription {
	t.Helper()
	return decode[PushSubscription](t, a.call(t, u, http.MethodPost, "/push/subscriptions", deviceBody(id, sub, "Chrome on Linux")), status)
}

func TestVapidPublicKey(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	got := decode[GetVapidPublicKey200JSONResponse](t, a.call(t, alice, http.MethodGet, "/push/vapid-public-key", ""), http.StatusOK)
	if len(got.PublicKey) != 87 {
		t.Errorf("public key = %q", got.PublicKey)
	}
}

// D-32 and the contract: idempotent on id and on endpoint; an endpoint
// registered by another user moves to the caller.
func TestRegisterDevices(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	phone := a.push.NewDevice(t, "phone")

	id := newID(t)
	created := a.register(t, alice, id, phone, http.StatusCreated)
	if created.Id != id || created.Channel != PushChannelWebpush || created.Label.MustGet() != "Chrome on Linux" {
		t.Errorf("created = %+v", created)
	}
	a.register(t, alice, id, phone, http.StatusOK)
	// Same endpoint, new id (the browser lost its local state): the same
	// device, keys refreshed.
	again := a.register(t, alice, newID(t), phone, http.StatusOK)
	if again.Id != id {
		t.Errorf("same endpoint gave device %s, want %s", again.Id, id)
	}
	// Bob signs in on the same browser: the device is his now.
	moved := a.register(t, bob, newID(t), phone, http.StatusOK)
	if moved.Id != id {
		t.Errorf("moved device id = %s", moved.Id)
	}
	if list := decode[ListPushSubscriptions200JSONResponse](t, a.call(t, alice, http.MethodGet, "/push/subscriptions", ""), http.StatusOK); len(list.Items) != 0 {
		t.Errorf("alice still lists %d devices", len(list.Items))
	}
	if list := decode[ListPushSubscriptions200JSONResponse](t, a.call(t, bob, http.MethodGet, "/push/subscriptions", ""), http.StatusOK); len(list.Items) != 1 {
		t.Errorf("bob lists %d devices", len(list.Items))
	}
	// D-23: bob's device id on another endpoint is a conflict for alice.
	laptop := a.push.NewDevice(t, "laptop")
	wantProblem(t, a.call(t, alice, http.MethodPost, "/push/subscriptions", deviceBody(id, laptop, "x")), http.StatusConflict, ProblemCodeConflict)
}

func TestRegisterDeviceValidation(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	phone := a.push.NewDevice(t, "phone")
	bad := phone
	bad.P256dh = "AAAA"
	problem := wantProblem(t, a.call(t, alice, http.MethodPost, "/push/subscriptions", deviceBody(newID(t), bad, "x")),
		http.StatusUnprocessableEntity, ProblemCodeValidationFailed)
	if fields := fieldsOf(problem); len(fields) != 1 || fields[0] != "/keys" {
		t.Errorf("fields = %v", fields)
	}
}

// Deleting is final (D-32); someone else's device is a 404.
func TestDeleteDevice(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	bob := a.signUp(t, "bob")
	device := a.register(t, alice, newID(t), a.push.NewDevice(t, "phone"), http.StatusCreated)
	path := "/push/subscriptions/" + device.Id.String()

	wantProblem(t, a.call(t, bob, http.MethodDelete, path, ""), http.StatusNotFound, ProblemCodeNotFound)
	wantProblem(t, a.call(t, bob, http.MethodPost, path+"/test", ""), http.StatusNotFound, ProblemCodeNotFound)
	if rec := a.call(t, alice, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	wantProblem(t, a.call(t, alice, http.MethodDelete, path, ""), http.StatusNotFound, ProblemCodeNotFound)
}

// D-32: the test notification goes straight to the device.
func TestTestNotification(t *testing.T) {
	a := newTestApp(t)
	alice := a.signUp(t, "alice")
	device := a.register(t, alice, newID(t), a.push.NewDevice(t, "phone"), http.StatusCreated)
	if rec := a.call(t, alice, http.MethodPost, "/push/subscriptions/"+device.Id.String()+"/test", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("test = %d", rec.Code)
	}
	a.devices.Wait()
	messages := a.push.Messages()
	if len(messages) != 1 || messages[0].Device != "phone" {
		t.Fatalf("messages = %+v, failures %v", messages, a.push.Failures())
	}
	var payload map[string]any
	if err := json.Unmarshal(messages[0].Payload, &payload); err != nil || payload["type"] != "test" {
		t.Errorf("payload = %s", messages[0].Payload)
	}
	var n int
	if err := a.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_deliveries").Scan(&n); err != nil || n != 0 {
		t.Errorf("deliveries = %d (%v), want none", n, err)
	}
}

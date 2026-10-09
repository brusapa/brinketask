// Package notify delivers reminders to devices (SPEC section 6): it keeps
// the users' Web Push subscriptions and runs the scheduler that turns due
// reminders into notifications.
package notify

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Kinds of message the service worker receives.
const (
	kindReminder = "reminder"
	kindTest     = "test"
)

// payload is what a push message carries: data, never text (D-65). The
// service worker writes the notification with the web client's message
// catalog. Field names are part of the contract with web/src/sw.
type payload struct {
	Type       string     `json:"type"`
	TaskID     *uuid.UUID `json:"task_id,omitempty"`
	ReminderID *uuid.UUID `json:"reminder_id,omitempty"`
	Title      string     `json:"title,omitempty"`
	ListName   string     `json:"list_name,omitempty"`
	IsInbox    bool       `json:"is_inbox,omitempty"`
	// DueDate is "YYYY-MM-DD", DueTime "HH:MM"; the service worker shows
	// them in the user's zone, Timezone.
	DueDate  string     `json:"due_date,omitempty"`
	DueTime  string     `json:"due_time,omitempty"`
	DueTz    string     `json:"due_tz,omitempty"`
	Timezone string     `json:"timezone,omitempty"`
	FireAt   *time.Time `json:"fire_at,omitempty"`
}

// testPayload is the message of POST /push/subscriptions/{id}/test.
func testPayload() []byte {
	data, _ := json.Marshal(payload{Type: kindTest}) // cannot fail: plain fields
	return data
}

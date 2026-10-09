package notify

import "testing"

// The server posts to the endpoint a user registered, so it accepts only
// https push-service host names (SSRF).
func TestCheckEndpoint(t *testing.T) {
	d := &Devices{}
	for endpoint, ok := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":            true,
		"https://updates.push.services.mozilla.com/wpush/v2": true,
		"https://web.push.apple.com/QGuQyavXutnMHIWnCv":      true,
		"http://fcm.googleapis.com/fcm/send/abc":             false,
		"https://localhost/push":                             false,
		"https://127.0.0.1/push":                             false,
		"https://10.0.0.5/push":                              false,
		"https://[::1]/push":                                 false,
		"https://169.254.169.254/latest/meta-data":           false,
		"not a url": false,
		"/relative": false,
	} {
		if err := d.checkEndpoint(endpoint); (err == nil) != ok {
			t.Errorf("%s: err = %v, want ok %v", endpoint, err, ok)
		}
	}

	// D-73: the end-to-end test's prefix, and nothing else, is let through.
	d.AllowEndpointPrefix("http://localhost:18091/")
	for endpoint, ok := range map[string]bool{
		"http://localhost:18091/e2e-phone":   true,
		"http://localhost:18091.example/x":   false,
		"http://localhost:18092/e2e-phone":   false,
		"http://127.0.0.1:18091/e2e-phone":   false,
		"https://fcm.googleapis.com/fcm/abc": true,
	} {
		if err := d.checkEndpoint(endpoint); (err == nil) != ok {
			t.Errorf("with the test prefix, %s: err = %v, want ok %v", endpoint, err, ok)
		}
	}

	d.AllowLocalEndpoints()
	if err := d.checkEndpoint("http://127.0.0.1:4000/phone"); err != nil {
		t.Errorf("tests' local endpoint refused: %v", err)
	}
}

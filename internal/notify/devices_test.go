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
	d.AllowLocalEndpoints()
	if err := d.checkEndpoint("http://127.0.0.1:4000/phone"); err != nil {
		t.Errorf("tests' local endpoint refused: %v", err)
	}
}

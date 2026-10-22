package notify

// Reproduction of finding H2 in REVIEW.md (the endpoint check). It fails
// until the finding is fixed.

import "testing"

// H2 (SPEC section 10, SSRF): names of this machine are refused however
// they are spelled; DNS names are case-insensitive and may end in a dot.
func TestReviewH2CheckEndpointRefusesLocalhostSpellings(t *testing.T) {
	d := &Devices{}
	for _, endpoint := range []string{
		"https://LOCALHOST/push",
		"https://localhost./push",
	} {
		if err := d.checkEndpoint(endpoint); err == nil {
			t.Errorf("%s accepted", endpoint)
		}
	}
}

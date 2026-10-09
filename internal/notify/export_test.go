package notify

// SenderOf lets the external tests build a second scheduler with the same
// sender (an export_test.go file is compiled only with the tests).
func SenderOf(s *Scheduler) Sender {
	return s.sender
}

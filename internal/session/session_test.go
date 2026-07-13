package session

import "testing"

func TestStatusClassification(t *testing.T) {
	for _, status := range []Status{StatusStarting, StatusRunning, StatusIdle, StatusWaiting} {
		if !status.Live() {
			t.Errorf("%q should be live", status)
		}
		if status.Terminal() {
			t.Errorf("%q should not be terminal", status)
		}
	}
	for _, status := range []Status{StatusCompleted, StatusFailed, StatusCancelled} {
		if status.Live() {
			t.Errorf("%q should not be live", status)
		}
		if !status.Terminal() {
			t.Errorf("%q should be terminal", status)
		}
	}
}

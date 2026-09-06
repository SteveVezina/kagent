package v1alpha3

import "testing"

func TestPiHarnessSelector(t *testing.T) {
	spec := HarnessSpec{Pi: &PiHarness{}}
	if spec.Pi == nil {
		t.Fatal("Pi Harness selector was not preserved")
	}
	if spec.Kagent != nil || spec.Codex != nil || spec.Claude != nil || spec.BYO != nil {
		t.Fatalf("Pi Harness selector unexpectedly set another runtime: %#v", spec)
	}
}

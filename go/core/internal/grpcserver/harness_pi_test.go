package grpcserver

import (
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

func TestHarnessRuntimePi(t *testing.T) {
	harness := &v1alpha3.Harness{Spec: v1alpha3.HarnessSpec{Pi: &v1alpha3.PiHarness{}}}
	if got := harnessRuntime(harness); got != "pi" {
		t.Fatalf("harnessRuntime() = %q, want pi", got)
	}
}

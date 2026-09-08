package e2e_test

import "testing"

// TestE2EPiMockActiveTaskCancellation mirrors the native Claude runtime's
// cancellation coverage. It proves that a public A2A task cancellation reaches
// the Pi driver while its model request is blocked, closes all task streams at
// the single CANCELED terminal boundary, and persists that terminal state.
func TestE2EPiMockActiveTaskCancellation(t *testing.T) {
	target := interactionTarget(t)
	modelURL, started := startBlockingInteractionMock(t)
	template := createPiMockTemplate(t, modelURL)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, piE2EHarness, template)
	testActiveTaskCancellation(t, fixture, started)
}

package e2e

import (
	"testing"

	o "github.com/onsi/gomega"
)

// NOTE: This test is also available in the OTE framework (test/e2e/ha_mode.go).
// This dual implementation allows tests to run both as standard Go tests (via go test)
// and through the Ginkgo/OTE framework (for OpenShift CI integration).
//
// The actual test logic is in ha_mode.go's standalone functions, which are called
// by both this standard Go test and the Ginkgo specs.

// TestHAMode tests the High Availability mode functionality
func TestHAMode(t *testing.T) {
	o.RegisterTestingT(t)

	t.Run("HA Mode Toggle", func(t *testing.T) {
		ctx, cancelFnc, kubeClient, err := setupOperator(t)
		if err != nil {
			t.Fatalf("Failed to setup operator: %v", err)
		}
		defer cancelFnc()

		testHAModeToggle(t, ctx, kubeClient)
	})
}

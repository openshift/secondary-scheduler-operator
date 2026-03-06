package e2e

import (
	"testing"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

// TestExtended runs the operator tests using standard Go testing.
func TestExtended(t *testing.T) {
	// Register Gomega with the testing framework for standard Go test mode
	o.RegisterTestingT(t)

	t.Run("SecondaryScheduler Operator", func(t *testing.T) {
		// Setup operator and wait for it to be ready
		ctx, cancelFnc, kubeClient, err := setupOperator(t)
		if err != nil {
			t.Fatalf("Failed to setup operator: %v", err)
		}
		defer cancelFnc()

		t.Run("Scheduling a pod", func(t *testing.T) {
			testNamespace := testScheduling(g.GinkgoTB(), ctx, kubeClient)
			defer cleanupTestNamespace(t, ctx, kubeClient, testNamespace)
		})
	})
}

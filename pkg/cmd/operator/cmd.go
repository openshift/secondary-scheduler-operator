package operator

import (
	"github.com/spf13/cobra"
	"k8s.io/utils/clock"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator"
	"github.com/openshift/secondary-scheduler-operator/pkg/version"
)

func NewOperator() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("openshift-secondary-scheduler-operator", version.Get(), operator.RunOperator, clock.RealClock{}).
		NewCommand()
	cmd.Use = "operator"
	cmd.Short = "Start the Cluster secondary-scheduler Operator"

	return cmd
}

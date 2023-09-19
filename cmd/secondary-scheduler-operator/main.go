package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift/secondary-scheduler-operator/pkg/cmd/operator"
)

func main() {
	command := NewSecondarySchedulerOperatorCommand()
	if err := command.Execute(); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "%v\n", err)
		if err != nil {
			fmt.Printf("Unable to print err to stderr: %v", err)
		}
		os.Exit(1)
	}
}

func NewSecondarySchedulerOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secondary-scheduler-operator",
		Short: "OpenShift cluster secondary-scheduler operator",
		Run: func(cmd *cobra.Command, args []string) {
			err := cmd.Help()
			if err != nil {
				fmt.Printf("Unable to print help: %v", err)
			}
			os.Exit(1)
		},
	}

	cmd.AddCommand(operator.NewOperator())
	return cmd
}

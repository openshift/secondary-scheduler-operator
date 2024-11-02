/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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

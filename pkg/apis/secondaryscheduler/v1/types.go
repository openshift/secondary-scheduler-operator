package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SecondaryScheduler is the Schema for the secondaryscheduler API
// +k8s:openapi-gen=true
// +genclient
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
type SecondaryScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec holds user settable values for configuration
	// +required
	Spec SecondarySchedulerSpec `json:"spec"`
	// status holds observed values from the cluster. They may not be overridden.
	// +optional
	Status SecondarySchedulerStatus `json:"status"`
}

// SecondarySchedulerSpec defines the desired state of SecondaryScheduler
type SecondarySchedulerSpec struct {
	operatorv1.OperatorSpec `json:",inline"`

	// SchedulerConfig allows configuring the customized scheduler plugin configuration for the secondaryscheduler.
	SchedulerConfig string `json:"schedulerConfig"`

	// SchedulerImage sets the container image url to be pulled for the custom scheduler that's deployed
	SchedulerImage string `json:"schedulerImage"`

	// topology defines scheduling constraints for the secondary scheduler instances
	// +optional
	Topology Topology `json:"topology,omitempty"`
}

// TopologyMode defines the topology mode for the secondary scheduler instances.
// +kubebuilder:validation:Enum="";SingleReplica;HighlyAvailable
type TopologyMode string

const (
	// "HighlyAvailable" is for secondary scheduler instances to configure high-availability as much as possible.
	HighlyAvailableMode TopologyMode = "HighlyAvailable"

	// "SingleReplica" is for a single secondary scheduler instances to avoid spending resources for high-availability purpose.
	SingleReplicaMode TopologyMode = "SingleReplica"
)

// Topology allows to configure the secondary schedulers to run in an HA mode
// +kubebuilder:validation:XValidation:rule="self.mode == 'HighlyAvailable' || !has(self.highlyAvailableTopology)",message="highlyAvailableTopology can only be set when mode is HighlyAvailable"
// +kubebuilder:validation:XValidation:rule="self.mode != 'HighlyAvailable' || !has(self.highlyAvailableTopology) || !has(self.highlyAvailableTopology.maxReplicas) || self.highlyAvailableTopology.maxReplicas >= 3",message="maxReplicas must be at least 3 for HighlyAvailable mode"
type Topology struct {
	// mode defines the topology mode for the secondary scheduler instances.
	// If unspecified, mode defaults to SingleReplica. The default is subject to change over time.
	// +kubebuilder:default=SingleReplica
	// +unionDiscriminator
	Mode TopologyMode `json:"mode,omitempty"`

	// highlyAvailableTopology provides configuration for HA mode
	// If empty, defaults will be applied. The defaults are subject to change over time.
	// +optional
	HighlyAvailableTopology *HighlyAvailableTopology `json:"highlyAvailableTopology,omitempty"`
}

type HighlyAvailableTopology struct {
	// If specified, the node selector for the target nodes.
	// If unspecified all nodes are considered (default when no node selector is provided).
	// The default is subject to change over time.
	NodeSelector *map[string]string `json:"nodeSelector,omitempty"`

	// If specified, the pod's tolerations.
	// If unspecified no taint is tolerated (default when no toleration is provided).
	// The default is subject to change over time.
	Tolerations *[]corev1.Toleration `json:"tolerations,omitempty"`

	// maxReplicas defines the maximum number of replicas for the secondary scheduler.
	// In HA mode, the actual number of replicas is determined by the number of nodes
	// matching the nodeSelector, but will not exceed maxReplicas if specified.
	// If unspecified, maxReplicas defaults to 3.
	// The default is subject to change over time.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas uint32 `json:"maxReplicas,omitempty"`
}

// SecondarySchedulerStatus defines the observed state of SecondaryScheduler
type SecondarySchedulerStatus struct {
	operatorv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SecondarySchedulerList contains a list of SecondaryScheduler
type SecondarySchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecondaryScheduler `json:"items"`
}

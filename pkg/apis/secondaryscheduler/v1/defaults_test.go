package v1

import (
	"testing"
)

func TestSetDefaults_Topology_HighlyAvailable(t *testing.T) {
	topology := &Topology{
		Mode: HighlyAvailableMode,
	}

	SetDefaults_Topology(topology)

	if topology.HighlyAvailableTopology == nil {
		t.Errorf("Expected HighlyAvailableTopology to be initialized, got nil")
	}

	if topology.HighlyAvailableTopology.MaxReplicas != 3 {
		t.Errorf("Expected MaxReplicas to default to 3, got %d", topology.HighlyAvailableTopology.MaxReplicas)
	}
}

func TestSetDefaults_Topology_SingleReplica(t *testing.T) {
	topology := &Topology{
		Mode: SingleReplicaMode,
	}

	SetDefaults_Topology(topology)

	if topology.HighlyAvailableTopology != nil {
		t.Errorf("Expected HighlyAvailableTopology to be nil for SingleReplica mode, got %+v", topology.HighlyAvailableTopology)
	}
}

func TestSetDefaults_Topology_HighlyAvailable_WithExisting(t *testing.T) {
	maxReplicas := uint32(5)
	topology := &Topology{
		Mode: HighlyAvailableMode,
		HighlyAvailableTopology: &HighlyAvailableTopology{
			MaxReplicas: maxReplicas,
		},
	}

	SetDefaults_Topology(topology)

	// Should not override existing values
	if topology.HighlyAvailableTopology.MaxReplicas != maxReplicas {
		t.Errorf("Expected MaxReplicas to remain %d, got %d", maxReplicas, topology.HighlyAvailableTopology.MaxReplicas)
	}
}

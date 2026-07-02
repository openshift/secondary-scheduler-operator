package v1

// SetDefaults_SecondaryScheduler sets default values for SecondaryScheduler
func SetDefaults_SecondaryScheduler(obj *SecondaryScheduler) {
	SetDefaults_Topology(&obj.Spec.Topology)
}

// SetDefaults_Topology sets default values for Topology based on the mode
func SetDefaults_Topology(obj *Topology) {
	// When mode is HighlyAvailable and highlyAvailableTopology is not set,
	// initialize it with default values
	if obj.Mode == HighlyAvailableMode && obj.HighlyAvailableTopology == nil {
		obj.HighlyAvailableTopology = &HighlyAvailableTopology{
			MaxReplicas: 3,
		}
	}
}

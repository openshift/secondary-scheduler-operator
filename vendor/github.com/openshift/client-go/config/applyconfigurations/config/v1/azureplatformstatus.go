// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	configv1 "github.com/openshift/api/config/v1"
)

// AzurePlatformStatusApplyConfiguration represents a declarative configuration of the AzurePlatformStatus type for use
// with apply.
type AzurePlatformStatusApplyConfiguration struct {
	ResourceGroupName        *string                              `json:"resourceGroupName,omitempty"`
	NetworkResourceGroupName *string                              `json:"networkResourceGroupName,omitempty"`
	CloudName                *configv1.AzureCloudEnvironment      `json:"cloudName,omitempty"`
	ARMEndpoint              *string                              `json:"armEndpoint,omitempty"`
	ResourceTags             []AzureResourceTagApplyConfiguration `json:"resourceTags,omitempty"`
}

// AzurePlatformStatusApplyConfiguration constructs a declarative configuration of the AzurePlatformStatus type for use with
// apply.
func AzurePlatformStatus() *AzurePlatformStatusApplyConfiguration {
	return &AzurePlatformStatusApplyConfiguration{}
}

// WithResourceGroupName sets the ResourceGroupName field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ResourceGroupName field is set to the value of the last call.
func (b *AzurePlatformStatusApplyConfiguration) WithResourceGroupName(value string) *AzurePlatformStatusApplyConfiguration {
	b.ResourceGroupName = &value
	return b
}

// WithNetworkResourceGroupName sets the NetworkResourceGroupName field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the NetworkResourceGroupName field is set to the value of the last call.
func (b *AzurePlatformStatusApplyConfiguration) WithNetworkResourceGroupName(value string) *AzurePlatformStatusApplyConfiguration {
	b.NetworkResourceGroupName = &value
	return b
}

// WithCloudName sets the CloudName field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the CloudName field is set to the value of the last call.
func (b *AzurePlatformStatusApplyConfiguration) WithCloudName(value configv1.AzureCloudEnvironment) *AzurePlatformStatusApplyConfiguration {
	b.CloudName = &value
	return b
}

// WithARMEndpoint sets the ARMEndpoint field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ARMEndpoint field is set to the value of the last call.
func (b *AzurePlatformStatusApplyConfiguration) WithARMEndpoint(value string) *AzurePlatformStatusApplyConfiguration {
	b.ARMEndpoint = &value
	return b
}

// WithResourceTags adds the given value to the ResourceTags field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the ResourceTags field.
func (b *AzurePlatformStatusApplyConfiguration) WithResourceTags(values ...*AzureResourceTagApplyConfiguration) *AzurePlatformStatusApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithResourceTags")
		}
		b.ResourceTags = append(b.ResourceTags, *values[i])
	}
	return b
}

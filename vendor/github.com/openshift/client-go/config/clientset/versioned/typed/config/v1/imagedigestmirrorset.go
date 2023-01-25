// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	json "encoding/json"
	"fmt"
	"time"

	v1 "github.com/openshift/api/config/v1"
	configv1 "github.com/openshift/client-go/config/applyconfigurations/config/v1"
	scheme "github.com/openshift/client-go/config/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ImageDigestMirrorSetsGetter has a method to return a ImageDigestMirrorSetInterface.
// A group's client should implement this interface.
type ImageDigestMirrorSetsGetter interface {
	ImageDigestMirrorSets() ImageDigestMirrorSetInterface
}

// ImageDigestMirrorSetInterface has methods to work with ImageDigestMirrorSet resources.
type ImageDigestMirrorSetInterface interface {
	Create(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.CreateOptions) (*v1.ImageDigestMirrorSet, error)
	Update(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.UpdateOptions) (*v1.ImageDigestMirrorSet, error)
	UpdateStatus(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.UpdateOptions) (*v1.ImageDigestMirrorSet, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.ImageDigestMirrorSet, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.ImageDigestMirrorSetList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ImageDigestMirrorSet, err error)
	Apply(ctx context.Context, imageDigestMirrorSet *configv1.ImageDigestMirrorSetApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImageDigestMirrorSet, err error)
	ApplyStatus(ctx context.Context, imageDigestMirrorSet *configv1.ImageDigestMirrorSetApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImageDigestMirrorSet, err error)
	ImageDigestMirrorSetExpansion
}

// imageDigestMirrorSets implements ImageDigestMirrorSetInterface
type imageDigestMirrorSets struct {
	client rest.Interface
}

// newImageDigestMirrorSets returns a ImageDigestMirrorSets
func newImageDigestMirrorSets(c *ConfigV1Client) *imageDigestMirrorSets {
	return &imageDigestMirrorSets{
		client: c.RESTClient(),
	}
}

// Get takes name of the imageDigestMirrorSet, and returns the corresponding imageDigestMirrorSet object, and an error if there is any.
func (c *imageDigestMirrorSets) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.ImageDigestMirrorSet, err error) {
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Get().
		Resource("imagedigestmirrorsets").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ImageDigestMirrorSets that match those selectors.
func (c *imageDigestMirrorSets) List(ctx context.Context, opts metav1.ListOptions) (result *v1.ImageDigestMirrorSetList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.ImageDigestMirrorSetList{}
	err = c.client.Get().
		Resource("imagedigestmirrorsets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested imageDigestMirrorSets.
func (c *imageDigestMirrorSets) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("imagedigestmirrorsets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a imageDigestMirrorSet and creates it.  Returns the server's representation of the imageDigestMirrorSet, and an error, if there is any.
func (c *imageDigestMirrorSets) Create(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.CreateOptions) (result *v1.ImageDigestMirrorSet, err error) {
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Post().
		Resource("imagedigestmirrorsets").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imageDigestMirrorSet).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a imageDigestMirrorSet and updates it. Returns the server's representation of the imageDigestMirrorSet, and an error, if there is any.
func (c *imageDigestMirrorSets) Update(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.UpdateOptions) (result *v1.ImageDigestMirrorSet, err error) {
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Put().
		Resource("imagedigestmirrorsets").
		Name(imageDigestMirrorSet.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imageDigestMirrorSet).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *imageDigestMirrorSets) UpdateStatus(ctx context.Context, imageDigestMirrorSet *v1.ImageDigestMirrorSet, opts metav1.UpdateOptions) (result *v1.ImageDigestMirrorSet, err error) {
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Put().
		Resource("imagedigestmirrorsets").
		Name(imageDigestMirrorSet.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imageDigestMirrorSet).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the imageDigestMirrorSet and deletes it. Returns an error if one occurs.
func (c *imageDigestMirrorSets) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("imagedigestmirrorsets").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *imageDigestMirrorSets) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("imagedigestmirrorsets").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched imageDigestMirrorSet.
func (c *imageDigestMirrorSets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ImageDigestMirrorSet, err error) {
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Patch(pt).
		Resource("imagedigestmirrorsets").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}

// Apply takes the given apply declarative configuration, applies it and returns the applied imageDigestMirrorSet.
func (c *imageDigestMirrorSets) Apply(ctx context.Context, imageDigestMirrorSet *configv1.ImageDigestMirrorSetApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImageDigestMirrorSet, err error) {
	if imageDigestMirrorSet == nil {
		return nil, fmt.Errorf("imageDigestMirrorSet provided to Apply must not be nil")
	}
	patchOpts := opts.ToPatchOptions()
	data, err := json.Marshal(imageDigestMirrorSet)
	if err != nil {
		return nil, err
	}
	name := imageDigestMirrorSet.Name
	if name == nil {
		return nil, fmt.Errorf("imageDigestMirrorSet.Name must be provided to Apply")
	}
	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Patch(types.ApplyPatchType).
		Resource("imagedigestmirrorsets").
		Name(*name).
		VersionedParams(&patchOpts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}

// ApplyStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating ApplyStatus().
func (c *imageDigestMirrorSets) ApplyStatus(ctx context.Context, imageDigestMirrorSet *configv1.ImageDigestMirrorSetApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImageDigestMirrorSet, err error) {
	if imageDigestMirrorSet == nil {
		return nil, fmt.Errorf("imageDigestMirrorSet provided to Apply must not be nil")
	}
	patchOpts := opts.ToPatchOptions()
	data, err := json.Marshal(imageDigestMirrorSet)
	if err != nil {
		return nil, err
	}

	name := imageDigestMirrorSet.Name
	if name == nil {
		return nil, fmt.Errorf("imageDigestMirrorSet.Name must be provided to Apply")
	}

	result = &v1.ImageDigestMirrorSet{}
	err = c.client.Patch(types.ApplyPatchType).
		Resource("imagedigestmirrorsets").
		Name(*name).
		SubResource("status").
		VersionedParams(&patchOpts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}

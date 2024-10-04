// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	v1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	scheme "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// SecondarySchedulersGetter has a method to return a SecondarySchedulerInterface.
// A group's client should implement this interface.
type SecondarySchedulersGetter interface {
	SecondarySchedulers(namespace string) SecondarySchedulerInterface
}

// SecondarySchedulerInterface has methods to work with SecondaryScheduler resources.
type SecondarySchedulerInterface interface {
	Create(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.CreateOptions) (*v1.SecondaryScheduler, error)
	Update(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.UpdateOptions) (*v1.SecondaryScheduler, error)
	UpdateStatus(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.UpdateOptions) (*v1.SecondaryScheduler, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.SecondaryScheduler, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.SecondarySchedulerList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.SecondaryScheduler, err error)
	SecondarySchedulerExpansion
}

// secondarySchedulers implements SecondarySchedulerInterface
type secondarySchedulers struct {
	client rest.Interface
	ns     string
}

// newSecondarySchedulers returns a SecondarySchedulers
func newSecondarySchedulers(c *SecondaryschedulersV1Client, namespace string) *secondarySchedulers {
	return &secondarySchedulers{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the secondaryScheduler, and returns the corresponding secondaryScheduler object, and an error if there is any.
func (c *secondarySchedulers) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.SecondaryScheduler, err error) {
	result = &v1.SecondaryScheduler{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of SecondarySchedulers that match those selectors.
func (c *secondarySchedulers) List(ctx context.Context, opts metav1.ListOptions) (result *v1.SecondarySchedulerList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.SecondarySchedulerList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested secondarySchedulers.
func (c *secondarySchedulers) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a secondaryScheduler and creates it.  Returns the server's representation of the secondaryScheduler, and an error, if there is any.
func (c *secondarySchedulers) Create(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.CreateOptions) (result *v1.SecondaryScheduler, err error) {
	result = &v1.SecondaryScheduler{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(secondaryScheduler).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a secondaryScheduler and updates it. Returns the server's representation of the secondaryScheduler, and an error, if there is any.
func (c *secondarySchedulers) Update(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.UpdateOptions) (result *v1.SecondaryScheduler, err error) {
	result = &v1.SecondaryScheduler{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		Name(secondaryScheduler.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(secondaryScheduler).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *secondarySchedulers) UpdateStatus(ctx context.Context, secondaryScheduler *v1.SecondaryScheduler, opts metav1.UpdateOptions) (result *v1.SecondaryScheduler, err error) {
	result = &v1.SecondaryScheduler{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		Name(secondaryScheduler.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(secondaryScheduler).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the secondaryScheduler and deletes it. Returns an error if one occurs.
func (c *secondarySchedulers) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *secondarySchedulers) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("secondaryschedulers").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched secondaryScheduler.
func (c *secondarySchedulers) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.SecondaryScheduler, err error) {
	result = &v1.SecondaryScheduler{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("secondaryschedulers").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
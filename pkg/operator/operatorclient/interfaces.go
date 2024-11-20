package operatorclient

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	applyconfiguration "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	secondaryschedulerapplyconfiguration "github.com/openshift/secondary-scheduler-operator/pkg/generated/applyconfiguration/secondaryscheduler/v1"
	operatorconfigclientv1 "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/typed/secondaryscheduler/v1"
)

const OperatorNamespace = "openshift-secondary-scheduler-operator"
const OperatorConfigName = "cluster"
const OperandName = "secondary-scheduler"

var _ v1helpers.OperatorClient = &SecondarySchedulerClient{}

type SecondarySchedulerClient struct {
	Ctx            context.Context
	SharedInformer cache.SharedIndexInformer
	OperatorClient operatorconfigclientv1.SecondaryschedulersV1Interface
}

func (c SecondarySchedulerClient) Informer() cache.SharedIndexInformer {
	return c.SharedInformer
}

func (c SecondarySchedulerClient) GetOperatorState() (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	instance, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(c.Ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func (c SecondarySchedulerClient) GetOperatorStateWithQuorum(ctx context.Context) (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	return c.GetOperatorState()
}

func (c *SecondarySchedulerClient) UpdateOperatorSpec(ctx context.Context, resourceVersion string, spec *operatorv1.OperatorSpec) (out *operatorv1.OperatorSpec, newResourceVersion string, err error) {
	original, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Update(ctx, copy, v1.UpdateOptions{})
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}

func (c *SecondarySchedulerClient) UpdateOperatorStatus(ctx context.Context, resourceVersion string, status *operatorv1.OperatorStatus) (out *operatorv1.OperatorStatus, err error) {
	original, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	ret, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).UpdateStatus(ctx, copy, v1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return &ret.Status.OperatorStatus, nil
}

func (c *SecondarySchedulerClient) GetObjectMeta() (meta *metav1.ObjectMeta, err error) {
	instance, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(c.Ctx, OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &instance.ObjectMeta, nil
}

func (c *SecondarySchedulerClient) ApplyOperatorSpec(ctx context.Context, fieldManager string, desiredConfiguration *applyconfiguration.OperatorSpecApplyConfiguration) error {
	if desiredConfiguration == nil {
		return fmt.Errorf("applyConfiguration must have a value")
	}

	desiredSpec := &secondaryschedulerapplyconfiguration.SecondarySchedulerSpecApplyConfiguration{
		OperatorSpecApplyConfiguration: *desiredConfiguration,
	}
	desired := secondaryschedulerapplyconfiguration.SecondaryScheduler(OperatorConfigName, OperatorNamespace)
	desired.WithSpec(desiredSpec)

	instance, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
	// do nothing and proceed with the apply
	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		original, err := secondaryschedulerapplyconfiguration.ExtractSecondaryScheduler(instance, fieldManager)
		if err != nil {
			return fmt.Errorf("unable to extract operator configuration from spec: %w", err)
		}
		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
	}

	_, err = c.OperatorClient.SecondarySchedulers(OperatorNamespace).Apply(ctx, desired, v1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to Apply for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

func (c *SecondarySchedulerClient) ApplyOperatorStatus(ctx context.Context, fieldManager string, desiredConfiguration *applyconfiguration.OperatorStatusApplyConfiguration) error {
	if desiredConfiguration == nil {
		return fmt.Errorf("applyConfiguration must have a value")
	}

	desiredStatus := &secondaryschedulerapplyconfiguration.SecondarySchedulerStatusApplyConfiguration{
		OperatorStatusApplyConfiguration: *desiredConfiguration,
	}
	desired := secondaryschedulerapplyconfiguration.SecondaryScheduler(OperatorConfigName, OperatorNamespace)
	desired.WithStatus(desiredStatus)

	instance, err := c.OperatorClient.SecondarySchedulers(OperatorNamespace).Get(ctx, OperatorConfigName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// do nothing and proceed with the apply
		v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, nil)
	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		original, err := secondaryschedulerapplyconfiguration.ExtractSecondarySchedulerStatus(instance, fieldManager)
		if err != nil {
			return fmt.Errorf("unable to extract operator configuration from status: %w", err)
		}
		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
		if original.Status != nil {
			v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, original.Status.Conditions)
		} else {
			v1helpers.SetApplyConditionsLastTransitionTime(clock.RealClock{}, &desired.Status.Conditions, nil)
		}
	}

	_, err = c.OperatorClient.SecondarySchedulers(OperatorNamespace).ApplyStatus(ctx, desired, v1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to ApplyStatus for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

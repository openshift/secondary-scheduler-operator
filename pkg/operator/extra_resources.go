package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	"github.com/openshift/secondary-scheduler-operator/bindata"
	secondaryschedulersv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
)

const (
	labelExtraResource = "app"
)

type K8sObject interface {
	metav1.Object
	runtime.Object
}

func (c *TargetConfigReconciler) manageExtraResourcesDeployment(secondaryScheduler *secondaryschedulersv1.SecondaryScheduler, forceDeployment bool) (*appsv1.Deployment, bool, error) {
	required := resourceread.ReadDeploymentV1OrDie(bindata.MustAsset("assets/secondary-scheduler/deployment-extra.yaml"))
	required.Name = fmt.Sprintf("%s-extra-resources", secondaryScheduler.Name)
	required.Namespace = secondaryScheduler.Namespace
	required.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "SecondaryScheduler",
			Name:       secondaryScheduler.Name,
			UID:        secondaryScheduler.UID,
		},
	}

	images := map[string]string{
		"${IMAGE}": secondaryScheduler.Spec.SchedulerImage,
	}
	for i := range required.Spec.Template.Spec.Containers {
		for pat, img := range images {
			if required.Spec.Template.Spec.Containers[i].Image == pat {
				required.Spec.Template.Spec.Containers[i].Image = img
				break
			}
		}
	}
	var replicas int32 = 1
	required.Spec.Replicas = &replicas

	// FIXME: this method will disappear in 4.6 so we need to fix this ASAP
	return resourceapply.ApplyDeploymentWithForce(
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, secondaryScheduler.Status.Generations),
		forceDeployment)
}

func (c *TargetConfigReconciler) manageExtraResourcesObjects(dp *appsv1.Deployment) ([]K8sObject, error) {
	extraResPod, err := findExtraResourcesPod(c.kubeClient, dp)
	if err != nil {
		return nil, err
	}
	return getExtraResourcesObjects(c.kubeClient, extraResPod)
}

func findExtraResourcesPod(kubeClient kubernetes.Interface, dp *appsv1.Deployment) (*v1.Pod, error) {
	val, ok := dp.Spec.Template.ObjectMeta.Labels[labelExtraResource]
	if !ok {
		return nil, fmt.Errorf("label %q not found", labelExtraResource)
	}

	sel, err := labels.Parse(fmt.Sprintf("%s=%s", labelExtraResource, val))
	if err != nil {
		return nil, err
	}

	pods, err := kubeClient.CoreV1().Pods(dp.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, resourceNotReady
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("expected 1 pod in deployment %s/%s found %d", dp.Namespace, dp.Name, len(pods.Items))
	}
	return &pods.Items[0], nil
}

func getExtraResourcesObjects(kubeClient kubernetes.Interface, pod *v1.Pod) ([]K8sObject, error) {
	rc, err := kubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{}).Stream(context.TODO())
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(rc)

	decoder := scheme.Codecs.UniversalDeserializer()
	var extraObjs []K8sObject
	for _, resourceYAML := range strings.Split(buf.String(), "---") {
		if len(resourceYAML) == 0 {
			continue
		}
		obj, gvk, err := decoder.Decode([]byte(resourceYAML), nil, nil)
		if err != nil {
			return nil, err
		}

		// if objects are supported, coerce them in the namespace managed by the operator.
		if gvk.Group == "" && gvk.Version == "v1" && gvk.Kind == "ConfigMap" {
			cm := obj.(*v1.ConfigMap)
			cm.Namespace = pod.Namespace
			extraObjs = append(extraObjs, cm)
		} else if gvk.Group == "rbac.authorization.k8s.io" && gvk.Version == "v1" && gvk.Kind == "ClusterRole" {
			cr := obj.(*rbacv1.ClusterRole)
			cr.Namespace = pod.Namespace
			extraObjs = append(extraObjs, cr)
		} else if gvk.Group == "rbac.authorization.k8s.io" && gvk.Version == "v1" && gvk.Kind == "ClusterRoleBinding" {
			crb := obj.(*rbacv1.ClusterRoleBinding)
			if err := validateClusterRoleBinding(crb); err != nil {
				return nil, err
			}
			crb.Namespace = pod.Namespace
			extraObjs = append(extraObjs, obj.(*rbacv1.ClusterRoleBinding))
		} else {
			return nil, fmt.Errorf("unsupported object %T %s/%s %s", obj, gvk.Group, gvk.Version, gvk.Kind)
		}
	}
	return extraObjs, nil
}

func validateClusterRoleBinding(crb *rbacv1.ClusterRoleBinding) error {
	if len(crb.Subjects) > 1 {
		return fmt.Errorf("unsupported ClusterRoleBinding with more than 1 subject")
	}
	if crb.Subjects[0].Kind != "ServiceAccount" {
		return fmt.Errorf("unsupported subject kind for ClusterRoleBinding: %q", crb.Subjects[0].Kind)
	}
	return nil
}

func applyExtraObjects(kubeClient kubernetes.Interface, eventRecorder events.Recorder, serviceAccount *v1.ServiceAccount, extraObjs []K8sObject) error {
	for _, extraObj := range extraObjs {
		switch extraObj.(type) {
		case *v1.ConfigMap:
			if _, _, err := resourceapply.ApplyConfigMap(kubeClient.CoreV1(), eventRecorder, extraObj.(*v1.ConfigMap)); err != nil {
				return err
			}
		case *rbacv1.ClusterRole:
			if _, _, err := resourceapply.ApplyClusterRole(kubeClient.RbacV1(), eventRecorder, extraObj.(*rbacv1.ClusterRole)); err != nil {
				return err
			}
		case *rbacv1.ClusterRoleBinding:
			crb := extraObj.(*rbacv1.ClusterRoleBinding)
			crb.Subjects[0].Name = serviceAccount.Name
			crb.Subjects[0].Namespace = serviceAccount.Namespace
			if _, _, err := resourceapply.ApplyClusterRoleBinding(kubeClient.RbacV1(), eventRecorder, crb); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported extra object %T", extraObj)
		}
	}
	return nil
}

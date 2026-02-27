package operator

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/utils/clock"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configfake "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	secondaryschedulersv1 "github.com/openshift/secondary-scheduler-operator/pkg/apis/secondaryscheduler/v1"
	operatorclientfake "github.com/openshift/secondary-scheduler-operator/pkg/generated/clientset/versioned/fake"
	operatorclientinformers "github.com/openshift/secondary-scheduler-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/configobservation/configobservercontroller"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/operatorclient"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
)

func setupFakeClients(t *testing.T, apiServer *configv1.APIServer) (
	*operatorclient.SecondarySchedulerClient,
	kubernetes.Interface,
	v1helpers.KubeInformersForNamespaces,
	configinformers.SharedInformerFactory,
	dynamic.Interface,
) {
	// Create SecondaryScheduler CR
	secondaryScheduler := &secondaryschedulersv1.SecondaryScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorclient.OperatorConfigName,
			Namespace: operatorclient.OperatorNamespace,
		},
		Spec: secondaryschedulersv1.SecondarySchedulerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
			SchedulerImage:  "test-image",
			SchedulerConfig: "test-config",
		},
	}

	// Create required ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-config",
			Namespace:       operatorclient.OperatorNamespace,
			ResourceVersion: "1",
		},
		Data: map[string]string{
			"config.yaml": "{}",
		},
	}

	// Setup kube client with required resources
	fakeKubeClient := kubefake.NewSimpleClientset(configMap)
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(
		fakeKubeClient,
		"",
		operatorclient.OperatorNamespace,
	)

	// Add configmap to informer cache
	kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Core().V1().ConfigMaps().Informer().GetIndexer().Add(configMap)

	// Build list of objects to pre-populate the fake config client
	configObjects := []runtime.Object{}
	if apiServer != nil {
		configObjects = append(configObjects, apiServer)
	}
	fakeConfigClient := configfake.NewSimpleClientset(configObjects...)
	configInformers := configinformers.NewSharedInformerFactory(fakeConfigClient, 0)

	// Populate required informer caches
	if apiServer != nil {
		configInformers.Config().V1().APIServers().Informer().GetIndexer().Add(apiServer)
	}

	// Create fake dynamic client
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	// Create fake operator client
	fakeOperatorConfigClient := operatorclientfake.NewSimpleClientset(secondaryScheduler)
	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(fakeOperatorConfigClient, 10*time.Minute)

	// Add SecondaryScheduler to informer cache
	operatorConfigInformers.Secondaryschedulers().V1().SecondarySchedulers().Informer().GetIndexer().Add(secondaryScheduler)

	secondarySchedulerClient := &operatorclient.SecondarySchedulerClient{
		Ctx:            context.TODO(),
		SharedInformer: operatorConfigInformers.Secondaryschedulers().V1().SecondarySchedulers().Informer(),
		OperatorClient: fakeOperatorConfigClient.SecondaryschedulersV1(),
	}

	return secondarySchedulerClient, fakeKubeClient, kubeInformersForNamespaces, configInformers, dynamicClient
}

func TestManageDeployment_TLSConfiguration(t *testing.T) {
	// Get the default Intermediate TLS profile
	intermediateProfile := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	intermediateCiphers := crypto.OpenSSLToIANACipherSuites(intermediateProfile.Ciphers)

	tests := []struct {
		name                 string
		apiServer            *configv1.APIServer
		expectedCipherSuites string
		expectedMinTLSVer    string
	}{
		{
			name:                 "no APIServer config",
			apiServer:            nil,
			expectedCipherSuites: fmt.Sprintf("--tls-cipher-suites=%s", strings.Join(intermediateCiphers, ",")),
			expectedMinTLSVer:    fmt.Sprintf("--tls-min-version=%s", intermediateProfile.MinTLSVersion),
		},
		{
			name: "APIServer with TLS security profile",
			apiServer: &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
						Custom: &configv1.CustomTLSProfile{
							TLSProfileSpec: configv1.TLSProfileSpec{
								Ciphers: []string{
									"ECDHE-ECDSA-AES128-GCM-SHA256",
									"ECDHE-RSA-AES128-GCM-SHA256",
								},
								MinTLSVersion: configv1.VersionTLS12,
							},
						},
					},
				},
			},
			expectedCipherSuites: "--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			expectedMinTLSVer:    "--tls-min-version=VersionTLS12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			// Setup fake clients with all required resources
			fakeOperatorClient, fakeKubeClient, kubeInformersForNamespaces, configInformers, dynamicClient := setupFakeClients(t, tt.apiServer)

			// Create event recorder
			eventRecorder := events.NewInMemoryRecorder("", clock.RealClock{})

			// Create config observer - this registers event handlers with informers
			configObserver := configobservercontroller.NewConfigObserver(
				fakeOperatorClient,
				configInformers,
				resourcesynccontroller.NewResourceSyncController(
					"SecondarySchedulerOperator",
					fakeOperatorClient,
					kubeInformersForNamespaces,
					fakeKubeClient.CoreV1(),
					fakeKubeClient.CoreV1(),
					eventRecorder,
				),
				eventRecorder,
			)

			// Create target config reconciler - this registers event handlers with informers
			targetConfigReconciler, err := NewTargetConfigReconciler(
				ctx,
				fakeOperatorClient.OperatorClient,
				operatorclientinformers.NewSharedInformerFactory(operatorclientfake.NewSimpleClientset(), 10*time.Minute).Secondaryschedulers().V1().SecondarySchedulers(),
				kubeInformersForNamespaces,
				fakeOperatorClient,
				fakeKubeClient,
				nil, // osrClient not needed for this test
				dynamicClient,
				eventRecorder,
			)
			if err != nil {
				t.Fatalf("Failed to create target config reconciler: %v", err)
			}

			// Start informers after controllers have registered their event handlers
			kubeInformersForNamespaces.Start(ctx.Done())
			configInformers.Start(ctx.Done())

			// Validate that operator spec doesn't have observed config before running config observer
			if specBefore, err := fakeOperatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{}); err != nil {
				t.Fatalf("failed to get secondary scheduler before config observer sync: %v", err)
			} else if len(specBefore.Spec.ObservedConfig.Raw) > 0 || specBefore.Spec.ObservedConfig.Object != nil {
				t.Fatalf("operator spec should not have ObservedConfig before config observer sync, got Raw=%v Object=%v",
					len(specBefore.Spec.ObservedConfig.Raw), specBefore.Spec.ObservedConfig.Object)
			}

			// Run config observer sync to update observed config in operator spec
			// This will call apiserver.ObserveTLSSecurityProfile internally
			if err := configObserver.Sync(ctx, &fakeSyncContext{recorder: eventRecorder}); err != nil {
				t.Logf("WARNING: config observer sync returned error: %v", err)
			}

			// Validate that observed config was injected by config observer
			if specAfter, err := fakeOperatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{}); err != nil {
				t.Fatalf("failed to get secondary scheduler after config observer sync: %v", err)
			} else if len(specAfter.Spec.ObservedConfig.Raw) == 0 {
				t.Fatalf("operator spec should have ObservedConfig.Raw populated after config observer sync")
			}

			// Run target config reconciler sync to trigger the full production code path
			if err := targetConfigReconciler.sync(queueItem{kind: "secondaryscheduler"}); err != nil {
				t.Fatalf("targetConfigReconciler.sync failed: %v", err)
			}

			// Read the generated Deployment from the fake kube client
			actualDeployment, err := fakeKubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get deployment: %v", err)
			}

			// Check container args for TLS settings
			foundCipherSuites := false
			foundMinTLSVersion := false

			for _, arg := range actualDeployment.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "--tls-cipher-suites=") {
					foundCipherSuites = true
					if arg != tt.expectedCipherSuites {
						t.Errorf("Expected cipher suites arg %q, got %q", tt.expectedCipherSuites, arg)
					}
				}
				if strings.HasPrefix(arg, "--tls-min-version=") {
					foundMinTLSVersion = true
					if arg != tt.expectedMinTLSVer {
						t.Errorf("Expected min TLS version arg %q, got %q", tt.expectedMinTLSVer, arg)
					}
				}
			}

			if !foundCipherSuites {
				t.Errorf("Expected to find --tls-cipher-suites arg but didn't")
			}
			if !foundMinTLSVersion {
				t.Errorf("Expected to find --tls-min-version arg but didn't")
			}
		})
	}
}

// fakeSyncContext implements factory.SyncContext for testing
type fakeSyncContext struct {
	recorder events.Recorder
}

func (f *fakeSyncContext) Queue() workqueue.RateLimitingInterface {
	return nil
}

func (f *fakeSyncContext) QueueKey() string {
	return ""
}

func (f *fakeSyncContext) Recorder() events.Recorder {
	return f.recorder
}

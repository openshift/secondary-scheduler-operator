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

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	secondarySchedulerUID                 = "a1b2c3d4-e5f6-7a8b-9c0d-e1f2a3b4c5d6"
	kubeSchedulerClusterRoleBindingName   = "secondary-scheduler-system-kube-scheduler"
	volumeSchedulerClusterRoleBindingName = "secondary-scheduler-system-volume-scheduler"
	schedulerConfigMapName                = "test-config"
)

func newOwnerReference() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion: "operator.openshift.io/v1",
			Kind:       "SecondaryScheduler",
			Name:       operatorclient.OperatorConfigName,
			UID:        secondarySchedulerUID,
		},
	}
}

func newSchedulerConfigMap(apply func(cm *corev1.ConfigMap)) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            schedulerConfigMapName,
			Namespace:       operatorclient.OperatorNamespace,
			ResourceVersion: "1",
		},
		Data: map[string]string{
			"config.yaml": "{}",
		},
	}
	if apply != nil {
		apply(cm)
	}
	return cm
}

func newSecondaryScheduler(apply func(ss *secondaryschedulersv1.SecondaryScheduler)) *secondaryschedulersv1.SecondaryScheduler {
	obj := &secondaryschedulersv1.SecondaryScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorclient.OperatorConfigName,
			Namespace: operatorclient.OperatorNamespace,
			UID:       secondarySchedulerUID,
		},
		Spec: secondaryschedulersv1.SecondarySchedulerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
			SchedulerImage:  "test-image",
			SchedulerConfig: schedulerConfigMapName,
		},
		Status: secondaryschedulersv1.SecondarySchedulerStatus{
			OperatorStatus: operatorv1.OperatorStatus{
				Generations: []operatorv1.GenerationStatus{
					{
						Group:          "apps",
						Resource:       "deployments",
						Namespace:      operatorclient.OperatorNamespace,
						Name:           operatorclient.OperandName,
						LastGeneration: 0,
					},
				},
			},
		},
	}
	if apply != nil {
		apply(obj)
	}
	return obj
}

func setupFakeClients(t *testing.T, apiServer *configv1.APIServer, secondaryScheduler *secondaryschedulersv1.SecondaryScheduler, coreObjects []runtime.Object) (
	*operatorclient.SecondarySchedulerClient,
	kubernetes.Interface,
	v1helpers.KubeInformersForNamespaces,
	configinformers.SharedInformerFactory,
	operatorclientinformers.SharedInformerFactory,
	dynamic.Interface,
) {

	// Setup kube client with required resources
	fakeKubeClient := kubefake.NewSimpleClientset(coreObjects...)
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(
		fakeKubeClient,
		"",
		operatorclient.OperatorNamespace,
	)

	// Add all core objects to informer cache
	for _, obj := range coreObjects {
		switch v := obj.(type) {
		case *corev1.ConfigMap:
			kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Core().V1().ConfigMaps().Informer().GetIndexer().Add(v)
		case *corev1.ServiceAccount:
			kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Core().V1().ServiceAccounts().Informer().GetIndexer().Add(v)
		case *appsv1.Deployment:
			kubeInformersForNamespaces.InformersFor(operatorclient.OperatorNamespace).Apps().V1().Deployments().Informer().GetIndexer().Add(v)
		}
	}

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

	// Create fake dynamic client for unstructured resources (e.g., ServiceMonitor)
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add corev1 to scheme: %v", err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add rbacv1 to scheme: %v", err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)

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

	return secondarySchedulerClient, fakeKubeClient, kubeInformersForNamespaces, configInformers, operatorConfigInformers, dynamicClient
}

// testSetup holds all the components needed for testing the reconciler
type testSetup struct {
	reconciler              *TargetConfigReconciler
	operatorClient          *operatorclient.SecondarySchedulerClient
	kubeClient              kubernetes.Interface
	dynamicClient           dynamic.Interface
	kubeInformers           v1helpers.KubeInformersForNamespaces
	configInformers         configinformers.SharedInformerFactory
	operatorConfigInformers operatorclientinformers.SharedInformerFactory
	eventRecorder           events.Recorder
	configObserver          *configobservercontroller.ConfigObserver
}

// setupTestReconciler creates and initializes a TargetConfigReconciler for testing
func setupTestReconciler(
	t *testing.T,
	ctx context.Context,
	apiServer *configv1.APIServer,
	secondaryScheduler *secondaryschedulersv1.SecondaryScheduler,
	coreObjects []runtime.Object,
) *testSetup {
	// Setup fake clients
	fakeOperatorClient, fakeKubeClient, kubeInformersForNamespaces, configInformers, operatorConfigInformers, dynamicClient := setupFakeClients(t, apiServer, secondaryScheduler, coreObjects)

	// Create event recorder
	eventRecorder := events.NewInMemoryRecorder("", clock.RealClock{})

	// Create target config reconciler
	targetConfigReconciler, err := NewTargetConfigReconciler(
		ctx,
		fakeOperatorClient.OperatorClient,
		operatorConfigInformers.Secondaryschedulers().V1().SecondarySchedulers(),
		kubeInformersForNamespaces,
		fakeOperatorClient,
		fakeKubeClient,
		nil, // osrClient not needed for tests
		dynamicClient,
		eventRecorder,
	)
	if err != nil {
		t.Fatalf("Failed to create target config reconciler: %v", err)
	}

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

	return &testSetup{
		reconciler:              targetConfigReconciler,
		operatorClient:          fakeOperatorClient,
		kubeClient:              fakeKubeClient,
		dynamicClient:           dynamicClient,
		kubeInformers:           kubeInformersForNamespaces,
		configInformers:         configInformers,
		operatorConfigInformers: operatorConfigInformers,
		eventRecorder:           eventRecorder,
		configObserver:          configObserver,
	}
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

			setup := setupTestReconciler(t, ctx, tt.apiServer, newSecondaryScheduler(nil), []runtime.Object{newSchedulerConfigMap(nil)})

			// Start informers after controllers have registered their event handlers
			setup.kubeInformers.Start(ctx.Done())
			setup.configInformers.Start(ctx.Done())
			setup.operatorConfigInformers.Start(ctx.Done())

			// Validate that operator spec doesn't have observed config before running config observer
			if specBefore, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{}); err != nil {
				t.Fatalf("failed to get secondary scheduler before config observer sync: %v", err)
			} else if len(specBefore.Spec.ObservedConfig.Raw) > 0 || specBefore.Spec.ObservedConfig.Object != nil {
				t.Fatalf("operator spec should not have ObservedConfig before config observer sync, got Raw=%v Object=%v",
					len(specBefore.Spec.ObservedConfig.Raw), specBefore.Spec.ObservedConfig.Object)
			}

			// Run config observer sync to update observed config in operator spec
			// This will call apiserver.ObserveTLSSecurityProfile internally
			if err := setup.configObserver.Sync(ctx, &fakeSyncContext{recorder: setup.eventRecorder}); err != nil {
				t.Logf("WARNING: config observer sync returned error: %v", err)
			}

			// Validate that observed config was injected by config observer
			if specAfter, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{}); err != nil {
				t.Fatalf("failed to get secondary scheduler after config observer sync: %v", err)
			} else if len(specAfter.Spec.ObservedConfig.Raw) == 0 {
				t.Fatalf("operator spec should have ObservedConfig.Raw populated after config observer sync")
			}

			// Run target config reconciler sync to trigger the full production code path
			if err := setup.reconciler.sync(queueItem{kind: "secondaryscheduler"}); err != nil {
				t.Fatalf("targetConfigReconciler.sync failed: %v", err)
			}

			// Read the generated Deployment from the fake kube client
			actualDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
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

func TestManageDeployment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(nil), []runtime.Object{newSchedulerConfigMap(nil)})

	setup.kubeInformers.Start(ctx.Done())
	setup.configInformers.Start(ctx.Done())
	setup.operatorConfigInformers.Start(ctx.Done())

	secondaryScheduler, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get SecondaryScheduler: %v", err)
	}

	t.Run("Phase 1: no Deployment exists initially", func(t *testing.T) {
		_, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("Expected Deployment to not exist (NotFound error), got: %v", err)
		}
	})

	t.Run("Phase 2: manageDeployment creates Deployment", func(t *testing.T) {
		deployment, modified, err := setup.reconciler.manageDeployment(secondaryScheduler, nil)
		if err != nil {
			t.Fatalf("manageDeployment failed: %v", err)
		}

		if !modified {
			t.Error("Expected modified=true when creating Deployment, got false")
		}

		actualDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Deployment after creation: %v", err)
		}

		// Verify the returned Deployment matches what we got from the client
		if deployment.Name != actualDeployment.Name {
			t.Errorf("Returned Deployment name %q doesn't match actual %q", deployment.Name, actualDeployment.Name)
		}
		if deployment.ResourceVersion != actualDeployment.ResourceVersion {
			t.Errorf("Returned Deployment ResourceVersion %q doesn't match actual %q", deployment.ResourceVersion, actualDeployment.ResourceVersion)
		}

		verifyName(t, actualDeployment)
		verifyNamespace(t, actualDeployment)
		verifyOwnerReference(t, actualDeployment)
	})

	t.Run("Phase 3: manageDeployment with no changes returns modified=false", func(t *testing.T) {
		_, modified, err := setup.reconciler.manageDeployment(secondaryScheduler, nil)
		if err != nil {
			t.Fatalf("manageDeployment failed: %v", err)
		}

		if modified {
			t.Error("Expected modified=false when no changes needed, got true")
		}

		actualDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Deployment: %v", err)
		}
		verifyOwnerReference(t, actualDeployment)
	})

	t.Run("Phase 4: manageDeployment restores changed field", func(t *testing.T) {
		currentDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Deployment: %v", err)
		}

		origImage := currentDeployment.Spec.Template.Spec.Containers[0].Image
		currentDeployment.Spec.Template.Spec.Containers[0].Image = "wrong-image"
		currentDeployment.ObjectMeta.Generation = 1
		if currentDeployment.Spec.Template.Spec.Containers[0].Image == origImage {
			t.Fatalf("Container image has not changed")
		}
		_, err = setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Update(ctx, currentDeployment, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("Failed to update Deployment with wrong image: %v", err)
		}

		_, modified, err := setup.reconciler.manageDeployment(secondaryScheduler, nil)
		if err != nil {
			t.Fatalf("manageDeployment failed: %v", err)
		}

		if !modified {
			t.Error("Expected modified=true when restoring changed field, got false")
		}

		restoredDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Deployment after restoration: %v", err)
		}

		if restoredDeployment.Spec.Template.Spec.Containers[0].Image != origImage {
			t.Errorf("Expected image to be restored to %q, got %q", origImage, restoredDeployment.Spec.Template.Spec.Containers[0].Image)
		}

		verifyOwnerReference(t, restoredDeployment)
	})
}

// verifyOwnerReference checks that the owner reference matches the expected value using cmp.Diff
func TestManageDeployment_Replacements(t *testing.T) {
	tests := []struct {
		name                    string
		setupSecondaryScheduler func(*secondaryschedulersv1.SecondaryScheduler)
		verify                  func(*testing.T, *appsv1.Deployment)
	}{
		{
			name: "Image replacement",
			setupSecondaryScheduler: func(obj *secondaryschedulersv1.SecondaryScheduler) {
				obj.Spec.SchedulerImage = "quay.io/openshift/custom-scheduler:v1.0"
			},
			verify: func(t *testing.T, deployment *appsv1.Deployment) {
				if len(deployment.Spec.Template.Spec.Containers) == 0 {
					t.Fatal("Expected at least one container")
				}
				actualImage := deployment.Spec.Template.Spec.Containers[0].Image
				expectedImage := "quay.io/openshift/custom-scheduler:v1.0"
				if actualImage != expectedImage {
					t.Errorf("Expected container image %q, got %q", expectedImage, actualImage)
				}
			},
		},
		{
			name: "ConfigMap replacement",
			setupSecondaryScheduler: func(obj *secondaryschedulersv1.SecondaryScheduler) {
				obj.Spec.SchedulerConfig = "custom-scheduler-config"
			},
			verify: func(t *testing.T, deployment *appsv1.Deployment) {
				if len(deployment.Spec.Template.Spec.Volumes) == 0 {
					t.Fatal("Expected at least one volume")
				}
				if deployment.Spec.Template.Spec.Volumes[0].ConfigMap == nil {
					t.Fatal("Expected first volume to be a ConfigMap")
				}
				actualConfigMapName := deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name
				expectedConfigMapName := "custom-scheduler-config"
				if actualConfigMapName != expectedConfigMapName {
					t.Errorf("Expected ConfigMap name %q, got %q", expectedConfigMapName, actualConfigMapName)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			// Setup fake clients with custom SecondaryScheduler
			setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(tt.setupSecondaryScheduler), []runtime.Object{newSchedulerConfigMap(nil)})

			// Start informers
			setup.kubeInformers.Start(ctx.Done())
			setup.configInformers.Start(ctx.Done())
			setup.operatorConfigInformers.Start(ctx.Done())

			// Get the SecondaryScheduler object
			secondaryScheduler, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get SecondaryScheduler: %v", err)
			}

			// Call manageDeployment
			_, _, err = setup.reconciler.manageDeployment(secondaryScheduler, nil)
			if err != nil {
				t.Fatalf("manageDeployment failed: %v", err)
			}

			// Verify the Deployment was created
			actualDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get Deployment: %v", err)
			}

			// Run test-specific verification
			tt.verify(t, actualDeployment)
		})
	}
}

func TestManageDeployment_LogLevels(t *testing.T) {
	tests := []struct {
		name             string
		logLevel         operatorv1.LogLevel
		expectedLogLevel string
	}{
		{
			name:             "Normal log level",
			logLevel:         operatorv1.Normal,
			expectedLogLevel: "-v=2",
		},
		{
			name:             "Debug log level",
			logLevel:         operatorv1.Debug,
			expectedLogLevel: "-v=4",
		},
		{
			name:             "Trace log level",
			logLevel:         operatorv1.Trace,
			expectedLogLevel: "-v=6",
		},
		{
			name:             "TraceAll log level",
			logLevel:         operatorv1.TraceAll,
			expectedLogLevel: "-v=8",
		},
		{
			name:             "Default log level (empty)",
			logLevel:         "",
			expectedLogLevel: "-v=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			// Setup fake clients with custom SecondaryScheduler
			setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(func(obj *secondaryschedulersv1.SecondaryScheduler) {
				obj.Spec.LogLevel = tt.logLevel
			}), []runtime.Object{newSchedulerConfigMap(nil)})

			// Start informers
			setup.kubeInformers.Start(ctx.Done())
			setup.configInformers.Start(ctx.Done())
			setup.operatorConfigInformers.Start(ctx.Done())

			// Get the SecondaryScheduler object
			secondaryScheduler, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get SecondaryScheduler: %v", err)
			}

			// Call manageDeployment
			_, _, err = setup.reconciler.manageDeployment(secondaryScheduler, nil)
			if err != nil {
				t.Fatalf("manageDeployment failed: %v", err)
			}

			// Verify the Deployment was created with the correct log level
			actualDeployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get Deployment: %v", err)
			}

			// Check that the log level arg is present and correct
			if len(actualDeployment.Spec.Template.Spec.Containers) == 0 {
				t.Fatal("Expected at least one container")
			}

			foundLogLevel := false
			for _, arg := range actualDeployment.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "-v=") {
					foundLogLevel = true
					if arg != tt.expectedLogLevel {
						t.Errorf("Expected log level arg %q, got %q", tt.expectedLogLevel, arg)
					}
					break
				}
			}

			if !foundLogLevel {
				t.Errorf("Expected to find log level arg %q in container args, but didn't find any -v= arg", tt.expectedLogLevel)
			}
		})
	}
}

func TestManageResources(t *testing.T) {
	testCases := []struct {
		resourceType        string
		resourceName        string
		manageFunc          func(*TargetConfigReconciler, *secondaryschedulersv1.SecondaryScheduler) (metav1.Object, bool, error)
		getResource         func(context.Context, *testSetup, string) (metav1.Object, error)
		updateResource      func(context.Context, *testSetup, metav1.Object) error
		modifyResource      func(*testing.T, metav1.Object)
		verifyRestore       func(*testing.T, metav1.Object)
		skipNamespaceVerify bool // For cluster-scoped resources
		skipModified        bool // For resources that don't restore fields (e.g., ServiceAccount)
	}{
		{
			resourceType: "ServiceAccount",
			resourceName: "secondary-scheduler",
			skipModified: true, // ApplyServiceAccount only merges metadata
			manageFunc:   (*TargetConfigReconciler).manageServiceAccount,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Update(ctx, obj.(*corev1.ServiceAccount), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {},
			verifyRestore:  func(t *testing.T, obj metav1.Object) {},
		},
		{
			resourceType: "Service",
			resourceName: "metrics",
			manageFunc:   (*TargetConfigReconciler).manageService,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Update(ctx, obj.(*corev1.Service), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				obj.(*corev1.Service).Spec.Selector = map[string]string{"app": "wrong-app"}
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				if diff := cmp.Diff(map[string]string{"app": "secondary-scheduler"}, obj.(*corev1.Service).Spec.Selector); diff != "" {
					t.Errorf("Expected selector to be restored, diff (-want +got):\n%s", diff)
				}
			},
		},
		{
			resourceType: "Role",
			resourceName: "prometheus-k8s",
			manageFunc:   (*TargetConfigReconciler).manageRole,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.RbacV1().Roles(operatorclient.OperatorNamespace).Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.RbacV1().Roles(operatorclient.OperatorNamespace).Update(ctx, obj.(*rbacv1.Role), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				obj.(*rbacv1.Role).Rules[0].Verbs = []string{"create", "delete"}
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				if diff := cmp.Diff([]string{"get", "list", "watch"}, obj.(*rbacv1.Role).Rules[0].Verbs); diff != "" {
					t.Errorf("Expected verbs to be restored, diff (-want +got):\n%s", diff)
				}
			},
		},
		{
			resourceType: "RoleBinding",
			resourceName: "prometheus-k8s",
			manageFunc:   (*TargetConfigReconciler).manageRoleBinding,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.RbacV1().RoleBindings(operatorclient.OperatorNamespace).Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.RbacV1().RoleBindings(operatorclient.OperatorNamespace).Update(ctx, obj.(*rbacv1.RoleBinding), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				obj.(*rbacv1.RoleBinding).Subjects[0].Namespace = "wrong-namespace"
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				if obj.(*rbacv1.RoleBinding).Subjects[0].Namespace != "openshift-monitoring" {
					t.Errorf("Expected namespace to be restored to %q, got %q", "openshift-monitoring", obj.(*rbacv1.RoleBinding).Subjects[0].Namespace)
				}
			},
		},
		{
			resourceType:        "ClusterRoleBinding",
			resourceName:        kubeSchedulerClusterRoleBindingName,
			skipNamespaceVerify: true,
			manageFunc:          (*TargetConfigReconciler).manageKubeSchedulerClusterRoleBinding,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Update(ctx, obj.(*rbacv1.ClusterRoleBinding), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				crb := obj.(*rbacv1.ClusterRoleBinding)
				if len(crb.Subjects) == 0 {
					t.Fatalf("Expected ClusterRoleBinding to have at least one subject")
				}
				crb.Subjects[0].Namespace = "wrong-namespace"
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				crb := obj.(*rbacv1.ClusterRoleBinding)
				if len(crb.Subjects) == 0 {
					t.Fatalf("Expected ClusterRoleBinding to have at least one subject")
				}
				if crb.Subjects[0].Namespace != operatorclient.OperatorNamespace {
					t.Errorf("Expected subject namespace to be restored to %q, got %q", operatorclient.OperatorNamespace, crb.Subjects[0].Namespace)
				}
			},
		},
		{
			resourceType: "ServiceMonitor",
			resourceName: "secondary-scheduler",
			manageFunc:   (*TargetConfigReconciler).manageServiceMonitor,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
				return setup.dynamicClient.Resource(gvr).Namespace(operatorclient.OperatorNamespace).Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
				_, err := setup.dynamicClient.Resource(gvr).Namespace(operatorclient.OperatorNamespace).Update(ctx, obj.(*unstructured.Unstructured), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				sm := obj.(*unstructured.Unstructured)
				endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				if err != nil || !found || len(endpoints) == 0 {
					return
				}
				endpoint := endpoints[0].(map[string]interface{})
				endpoint["interval"] = "99s"
				endpoints[0] = endpoint
				_ = unstructured.SetNestedSlice(sm.Object, endpoints, "spec", "endpoints")
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				sm := obj.(*unstructured.Unstructured)
				restoredEndpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				if err != nil || !found || len(restoredEndpoints) == 0 {
					t.Fatalf("Failed to get endpoints from restored ServiceMonitor: found=%v, err=%v", found, err)
				}
				restoredEndpoint := restoredEndpoints[0].(map[string]interface{})
				restoredInterval := restoredEndpoint["interval"]
				expectedInterval := "30s"
				if restoredInterval != expectedInterval {
					t.Errorf("Expected interval to be restored to %q, got %q", expectedInterval, restoredInterval)
				}
			},
		},
		{
			resourceType:        "ClusterRoleBinding",
			resourceName:        volumeSchedulerClusterRoleBindingName,
			skipNamespaceVerify: true,
			manageFunc:          (*TargetConfigReconciler).manageVolumeSchedulerClusterRoleBinding,
			getResource: func(ctx context.Context, setup *testSetup, name string) (metav1.Object, error) {
				return setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
			},
			updateResource: func(ctx context.Context, setup *testSetup, obj metav1.Object) error {
				_, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Update(ctx, obj.(*rbacv1.ClusterRoleBinding), metav1.UpdateOptions{})
				return err
			},
			modifyResource: func(t *testing.T, obj metav1.Object) {
				crb := obj.(*rbacv1.ClusterRoleBinding)
				if len(crb.Subjects) == 0 {
					t.Fatalf("Expected ClusterRoleBinding to have at least one subject")
				}
				crb.Subjects[0].Namespace = "wrong-namespace"
			},
			verifyRestore: func(t *testing.T, obj metav1.Object) {
				crb := obj.(*rbacv1.ClusterRoleBinding)
				if len(crb.Subjects) == 0 {
					t.Fatalf("Expected ClusterRoleBinding to have at least one subject")
				}
				if crb.Subjects[0].Namespace != operatorclient.OperatorNamespace {
					t.Errorf("Expected subject namespace to be restored to %q, got %q", operatorclient.OperatorNamespace, crb.Subjects[0].Namespace)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.resourceType+"/"+tc.resourceName, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(nil), []runtime.Object{newSchedulerConfigMap(nil)})

			setup.kubeInformers.Start(ctx.Done())
			setup.configInformers.Start(ctx.Done())
			setup.operatorConfigInformers.Start(ctx.Done())

			secondaryScheduler, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get SecondaryScheduler: %v", err)
			}

			t.Run("Phase 1: no resource exists initially", func(t *testing.T) {
				_, err := tc.getResource(ctx, setup, tc.resourceName)
				if !apierrors.IsNotFound(err) {
					t.Fatalf("Expected %s to not exist (NotFound error), got: %v", tc.resourceType, err)
				}
			})

			t.Run("Phase 2: creates resource", func(t *testing.T) {
				obj, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("manage function failed: %v", err)
				}

				if !modified {
					t.Errorf("Expected modified=true when creating %s, got false", tc.resourceType)
				}

				actual, err := tc.getResource(ctx, setup, tc.resourceName)
				if err != nil {
					t.Fatalf("Failed to get %s after creation: %v", tc.resourceType, err)
				}

				if obj.GetName() != actual.GetName() {
					t.Errorf("Returned %s name %q doesn't match actual %q", tc.resourceType, obj.GetName(), actual.GetName())
				}
				if obj.GetResourceVersion() != actual.GetResourceVersion() {
					t.Errorf("Returned %s ResourceVersion %q doesn't match actual %q", tc.resourceType, obj.GetResourceVersion(), actual.GetResourceVersion())
				}

				if !tc.skipNamespaceVerify {
					verifyNamespace(t, actual)
				}
				verifyOwnerReference(t, actual)
			})

			t.Run("Phase 3: no changes returns modified=false", func(t *testing.T) {
				_, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("manage function failed: %v", err)
				}

				if modified {
					t.Errorf("Expected modified=false when no changes needed, got true")
				}

				actual, err := tc.getResource(ctx, setup, tc.resourceName)
				if err != nil {
					t.Fatalf("Failed to get %s: %v", tc.resourceType, err)
				}
				if !tc.skipNamespaceVerify {
					verifyNamespace(t, actual)
				}
				verifyOwnerReference(t, actual)
			})

			t.Run("Phase 4: restores changed field", func(t *testing.T) {
				current, err := tc.getResource(ctx, setup, tc.resourceName)
				if err != nil {
					t.Fatalf("Failed to get %s: %v", tc.resourceType, err)
				}

				tc.modifyResource(t, current)

				err = tc.updateResource(ctx, setup, current)
				if err != nil {
					t.Fatalf("Failed to update %s with modified field: %v", tc.resourceType, err)
				}

				_, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("manage function failed: %v", err)
				}

				if !tc.skipModified && !modified {
					t.Errorf("Expected modified=true when restoring changed field, got false")
				}

				restored, err := tc.getResource(ctx, setup, tc.resourceName)
				if err != nil {
					t.Fatalf("Failed to get %s after restoration: %v", tc.resourceType, err)
				}

				tc.verifyRestore(t, restored)

				if !tc.skipNamespaceVerify {
					verifyNamespace(t, restored)
				}
				verifyOwnerReference(t, restored)
			})
		})
	}
}

func TestSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(nil), []runtime.Object{newSchedulerConfigMap(nil)})

	setup.kubeInformers.Start(ctx.Done())
	setup.configInformers.Start(ctx.Done())
	setup.operatorConfigInformers.Start(ctx.Done())

	t.Run("Phase 1: no resources exist initially", func(t *testing.T) {
		_, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler", metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected ServiceAccount to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, kubeSchedulerClusterRoleBindingName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected KubeScheduler ClusterRoleBinding to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, volumeSchedulerClusterRoleBindingName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected VolumeScheduler ClusterRoleBinding to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Get(ctx, "metrics", metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected Service to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.RbacV1().Roles(operatorclient.OperatorNamespace).Get(ctx, "prometheus-k8s", metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected Role to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.RbacV1().RoleBindings(operatorclient.OperatorNamespace).Get(ctx, "prometheus-k8s", metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected RoleBinding to not exist (NotFound error), got: %v", err)
		}

		_, err = setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Errorf("Expected Deployment to not exist (NotFound error), got: %v", err)
		}
	})

	t.Run("Phase 2: sync creates all resources", func(t *testing.T) {
		err := setup.reconciler.sync(queueItem{kind: "secondaryscheduler", name: ""})
		if err != nil {
			t.Fatalf("sync failed: %v", err)
		}

		sa, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get ServiceAccount after sync: %v", err)
		} else {
			verifyNamespace(t, sa)
			verifyOwnerReference(t, sa)
		}

		kubeSchedulerCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, kubeSchedulerClusterRoleBindingName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get KubeScheduler ClusterRoleBinding after sync: %v", err)
		} else {
			verifyOwnerReference(t, kubeSchedulerCRB)
		}

		volumeSchedulerCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, volumeSchedulerClusterRoleBindingName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get VolumeScheduler ClusterRoleBinding after sync: %v", err)
		} else {
			verifyOwnerReference(t, volumeSchedulerCRB)
		}

		service, err := setup.kubeClient.CoreV1().Services(operatorclient.OperatorNamespace).Get(ctx, "metrics", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get Service after sync: %v", err)
		} else {
			verifyNamespace(t, service)
			verifyOwnerReference(t, service)
		}

		role, err := setup.kubeClient.RbacV1().Roles(operatorclient.OperatorNamespace).Get(ctx, "prometheus-k8s", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get Role after sync: %v", err)
		} else {
			verifyNamespace(t, role)
			verifyOwnerReference(t, role)
		}

		roleBinding, err := setup.kubeClient.RbacV1().RoleBindings(operatorclient.OperatorNamespace).Get(ctx, "prometheus-k8s", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get RoleBinding after sync: %v", err)
		} else {
			verifyNamespace(t, roleBinding)
			verifyOwnerReference(t, roleBinding)
		}

		deployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("Failed to get Deployment after sync: %v", err)
		} else {
			verifyName(t, deployment)
			verifyNamespace(t, deployment)
			verifyOwnerReference(t, deployment)

			// Verify deployment has annotations for each resource
			annotations := deployment.Spec.Template.Annotations
			if annotations == nil {
				t.Error("Expected Deployment to have spec.template.annotations, but it's nil")
			} else {
				// Verify all expected annotations are present
				// Note: With fake clients, resource versions may be empty strings for some resources
				// The important thing is that the annotation keys are present, indicating that
				// sync() is properly tracking all resources
				expectedAnnotations := []string{
					"secondaryschedulers.operator.openshift.io/cluster",
					"configmaps/test-config",
					"serviceaccounts/secondary-scheduler",
					"clusterrolebindings/secondary-scheduler-system-kube-scheduler",
					"clusterrolebindings/secondary-scheduler-system-volume-scheduler",
					"services/metrics",
					"roles/prometheus-k8s",
					"rolebindings/prometheus-k8s",
					"servicemonitors/secondary-scheduler",
				}

				for _, key := range expectedAnnotations {
					if _, exists := annotations[key]; !exists {
						t.Errorf("Expected annotation %q not found in deployment spec.template.annotations", key)
					}
				}

				// Verify the configmap annotation has a non-empty value
				if val := annotations["configmaps/test-config"]; val == "" {
					t.Error("Expected annotation 'configmaps/test-config' to have non-empty resource version")
				}
			}
		}

		updatedScheduler, err := setup.operatorClient.OperatorClient.SecondarySchedulers(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperatorConfigName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get SecondaryScheduler after sync: %v", err)
		}

		if len(updatedScheduler.Status.Generations) == 0 {
			t.Error("Expected status.Generations to be updated, but it's empty")
		} else {
			generation := updatedScheduler.Status.Generations[0]
			if generation.Group != "apps" || generation.Resource != "deployments" {
				t.Errorf("Expected generation for apps/deployments, got %s/%s", generation.Group, generation.Resource)
			}
			if generation.Name != operatorclient.OperandName {
				t.Errorf("Expected generation name %q, got %q", operatorclient.OperandName, generation.Name)
			}
			if generation.Namespace != operatorclient.OperatorNamespace {
				t.Errorf("Expected generation namespace %q, got %q", operatorclient.OperatorNamespace, generation.Namespace)
			}
			// Verify LastGeneration is set to the deployment's generation
			// This ensures sync() properly tracks the deployment generation in the status
			if generation.LastGeneration != deployment.ObjectMeta.Generation {
				t.Errorf("Expected LastGeneration to match deployment generation %d, got %d", deployment.ObjectMeta.Generation, generation.LastGeneration)
			}
		}
	})
}

func TestGetResourceVersion(t *testing.T) {
	testCases := []struct {
		name            string
		obj             metav1.Object
		expectedVersion string
	}{
		{
			name:            "nil object returns 0",
			obj:             nil,
			expectedVersion: "0",
		},
		{
			name: "object with empty ResourceVersion returns empty string",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-sa",
					ResourceVersion: "",
				},
			},
			expectedVersion: "",
		},
		{
			name: "object with valid ResourceVersion returns the version",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-sa",
					ResourceVersion: "12345",
				},
			},
			expectedVersion: "12345",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getResourceVersion(tc.obj)
			if result != tc.expectedVersion {
				t.Errorf("Expected %q, got %q", tc.expectedVersion, result)
			}
		})
	}
}

func TestSyncResourceVersionAnnotations(t *testing.T) {
	testCases := []struct {
		name            string
		resourceVersion string
	}{
		{
			name:            "ResourceVersionsNotSet",
			resourceVersion: "",
		},
		{
			name:            "ResourceVersionsSet",
			resourceVersion: "1001",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.TODO(), 30*time.Second)
			defer cancel()

			// Create ConfigMap with specified ResourceVersion
			cm := newSchedulerConfigMap(func(cm *corev1.ConfigMap) {
				cm.ResourceVersion = tc.resourceVersion
			})
			setup := setupTestReconciler(t, ctx, nil, newSecondaryScheduler(nil), []runtime.Object{cm})

			// Add reactors to set ResourceVersions when resources are created
			if tc.resourceVersion != "" {
				fakeClient := setup.kubeClient.(*kubefake.Clientset)
				for _, resource := range []string{"serviceaccounts", "clusterrolebindings", "services", "roles", "rolebindings"} {
					fakeClient.PrependReactor("create", resource, func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
						action.(kubetesting.CreateAction).GetObject().(metav1.Object).SetResourceVersion(tc.resourceVersion)
						return false, nil, nil
					})
				}
				fakeDynamicClient := setup.dynamicClient.(*dynamicfake.FakeDynamicClient)
				fakeDynamicClient.PrependReactor("create", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
					createAction := action.(kubetesting.CreateAction)
					obj := createAction.GetObject().(*unstructured.Unstructured)
					obj.SetResourceVersion(tc.resourceVersion)
					return false, nil, nil
				})
			}

			setup.kubeInformers.Start(ctx.Done())
			setup.configInformers.Start(ctx.Done())
			setup.operatorConfigInformers.Start(ctx.Done())

			// Wait for informers to sync
			if !cache.WaitForCacheSync(ctx.Done(), setup.kubeInformers.InformersFor(operatorclient.OperatorNamespace).Core().V1().ConfigMaps().Informer().HasSynced) {
				t.Fatal("failed to sync informer caches")
			}

			item := queueItem{
				kind: "secondaryscheduler",
			}

			if err := setup.reconciler.sync(item); err != nil {
				t.Fatalf("sync failed: %v", err)
			}

			// Get the deployment and verify resource version annotations
			deployment, err := setup.kubeClient.AppsV1().Deployments(operatorclient.OperatorNamespace).Get(ctx, operatorclient.OperandName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get deployment: %v", err)
			}

			annotations := deployment.Spec.Template.Annotations
			if annotations == nil {
				t.Fatal("Expected deployment to have spec.template.annotations")
			}

			// Verify all expected annotations have the expected ResourceVersion
			expectedAnnotations := map[string]string{
				"configmaps/" + schedulerConfigMapName:                         tc.resourceVersion,
				"serviceaccounts/secondary-scheduler":                          tc.resourceVersion,
				"clusterrolebindings/" + kubeSchedulerClusterRoleBindingName:   tc.resourceVersion,
				"clusterrolebindings/" + volumeSchedulerClusterRoleBindingName: tc.resourceVersion,
				"services/metrics":                    tc.resourceVersion,
				"roles/prometheus-k8s":                tc.resourceVersion,
				"rolebindings/prometheus-k8s":         tc.resourceVersion,
				"servicemonitors/secondary-scheduler": tc.resourceVersion,
			}

			for key, expectedValue := range expectedAnnotations {
				value, exists := annotations[key]
				if !exists {
					t.Errorf("Expected annotation %q to exist", key)
					continue
				}

				if value != expectedValue {
					t.Errorf("Expected annotation %q to be %q, got %q", key, expectedValue, value)
				}
			}
		})
	}
}

func verifyOwnerReference(t *testing.T, obj metav1.Object) {
	t.Helper()
	if diff := cmp.Diff(newOwnerReference(), obj.GetOwnerReferences()); diff != "" {
		t.Errorf("OwnerReferences mismatch (-want +got):\n%s", diff)
	}
}

func verifyNamespace(t *testing.T, obj metav1.Object) {
	t.Helper()
	if obj.GetNamespace() != operatorclient.OperatorNamespace {
		t.Errorf("Expected Namespace=%q, got %q", operatorclient.OperatorNamespace, obj.GetNamespace())
	}
}

func verifyName(t *testing.T, obj metav1.Object) {
	t.Helper()
	if obj.GetName() != operatorclient.OperandName {
		t.Errorf("Expected Name=%q, got %q", operatorclient.OperandName, obj.GetName())
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

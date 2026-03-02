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
	"k8s.io/apimachinery/pkg/runtime"
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

	return secondarySchedulerClient, fakeKubeClient, kubeInformersForNamespaces, configInformers, operatorConfigInformers, dynamicClient
}

// testSetup holds all the components needed for testing the reconciler
type testSetup struct {
	reconciler              *TargetConfigReconciler
	operatorClient          *operatorclient.SecondarySchedulerClient
	kubeClient              kubernetes.Interface
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

func TestManageServiceAccount(t *testing.T) {
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

	t.Run("Phase 1: no ServiceAccount exists initially", func(t *testing.T) {
		_, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler", metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			t.Fatalf("Expected ServiceAccount to not exist (NotFound error), got: %v", err)
		}
	})

	t.Run("Phase 2: manageServiceAccount creates ServiceAccount", func(t *testing.T) {
		sa, modified, err := setup.reconciler.manageServiceAccount(secondaryScheduler)
		if err != nil {
			t.Fatalf("manageServiceAccount failed: %v", err)
		}

		if !modified {
			t.Error("Expected modified=true when creating ServiceAccount, got false")
		}

		actualSA, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get ServiceAccount after creation: %v", err)
		}

		// Verify the returned ServiceAccount matches what we got from the client
		if sa.Name != actualSA.Name {
			t.Errorf("Returned ServiceAccount name %q doesn't match actual %q", sa.Name, actualSA.Name)
		}
		if sa.ResourceVersion != actualSA.ResourceVersion {
			t.Errorf("Returned ServiceAccount ResourceVersion %q doesn't match actual %q", sa.ResourceVersion, actualSA.ResourceVersion)
		}

		verifyNamespace(t, actualSA)
		verifyOwnerReference(t, actualSA)
	})

	t.Run("Phase 3: manageServiceAccount with no changes returns modified=false", func(t *testing.T) {
		_, modified, err := setup.reconciler.manageServiceAccount(secondaryScheduler)
		if err != nil {
			t.Fatalf("manageServiceAccount failed: %v", err)
		}

		if modified {
			t.Error("Expected modified=false when no changes needed, got true")
		}

		actualSA, err := setup.kubeClient.CoreV1().ServiceAccounts(operatorclient.OperatorNamespace).Get(ctx, "secondary-scheduler", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get ServiceAccount: %v", err)
		}
		verifyOwnerReference(t, actualSA)
	})
}

func TestManageClusterRoleBindings(t *testing.T) {
	testCases := []struct {
		name           string
		crbName        string
		manageFunc     func(*TargetConfigReconciler, *secondaryschedulersv1.SecondaryScheduler) (*rbacv1.ClusterRoleBinding, bool, error)
		manageFuncName string
	}{
		{
			name:    "KubeScheduler",
			crbName: kubeSchedulerClusterRoleBindingName,
			manageFunc: func(r *TargetConfigReconciler, ss *secondaryschedulersv1.SecondaryScheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
				return r.manageKubeSchedulerClusterRoleBinding(ss)
			},
			manageFuncName: "manageKubeSchedulerClusterRoleBinding",
		},
		{
			name:    "VolumeScheduler",
			crbName: volumeSchedulerClusterRoleBindingName,
			manageFunc: func(r *TargetConfigReconciler, ss *secondaryschedulersv1.SecondaryScheduler) (*rbacv1.ClusterRoleBinding, bool, error) {
				return r.manageVolumeSchedulerClusterRoleBinding(ss)
			},
			manageFuncName: "manageVolumeSchedulerClusterRoleBinding",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

			t.Run("Phase 1: no ClusterRoleBinding exists initially", func(t *testing.T) {
				_, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, tc.crbName, metav1.GetOptions{})
				if !apierrors.IsNotFound(err) {
					t.Fatalf("Expected ClusterRoleBinding to not exist (NotFound error), got: %v", err)
				}
			})

			t.Run("Phase 2: creates ClusterRoleBinding", func(t *testing.T) {
				crb, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("%s failed: %v", tc.manageFuncName, err)
				}

				if !modified {
					t.Error("Expected modified=true when creating ClusterRoleBinding, got false")
				}

				actualCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, tc.crbName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get ClusterRoleBinding after creation: %v", err)
				}

				// Verify the returned ClusterRoleBinding matches what we got from the client
				if crb.Name != actualCRB.Name {
					t.Errorf("Returned ClusterRoleBinding name %q doesn't match actual %q", crb.Name, actualCRB.Name)
				}
				if crb.ResourceVersion != actualCRB.ResourceVersion {
					t.Errorf("Returned ClusterRoleBinding ResourceVersion %q doesn't match actual %q", crb.ResourceVersion, actualCRB.ResourceVersion)
				}

				verifyOwnerReference(t, actualCRB)
			})

			t.Run("Phase 3: no changes returns modified=false", func(t *testing.T) {
				_, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("%s failed: %v", tc.manageFuncName, err)
				}

				if modified {
					t.Error("Expected modified=false when no changes needed, got true")
				}

				actualCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, tc.crbName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get ClusterRoleBinding: %v", err)
				}
				verifyOwnerReference(t, actualCRB)
			})

			t.Run("Phase 4: restores changed field", func(t *testing.T) {
				currentCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, tc.crbName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get ClusterRoleBinding: %v", err)
				}

				// Find the ServiceAccount subject to modify
				if len(currentCRB.Subjects) == 0 {
					t.Fatalf("Expected ClusterRoleBinding to have at least one subject, got 0")
				}

				saSubjectIdx := -1
				for i, subject := range currentCRB.Subjects {
					if subject.Kind == "ServiceAccount" && subject.Name == "secondary-scheduler" {
						saSubjectIdx = i
						break
					}
				}
				if saSubjectIdx == -1 {
					t.Fatalf("Expected to find ServiceAccount subject 'secondary-scheduler' in ClusterRoleBinding")
				}

				origNamespace := currentCRB.Subjects[saSubjectIdx].Namespace
				currentCRB.Subjects[saSubjectIdx].Namespace = "wrong-namespace"
				if currentCRB.Subjects[saSubjectIdx].Namespace == origNamespace {
					t.Fatalf("Subject namespace has not changed")
				}
				_, err = setup.kubeClient.RbacV1().ClusterRoleBindings().Update(ctx, currentCRB, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Failed to update ClusterRoleBinding with wrong namespace: %v", err)
				}

				_, modified, err := tc.manageFunc(setup.reconciler, secondaryScheduler)
				if err != nil {
					t.Fatalf("%s failed: %v", tc.manageFuncName, err)
				}

				if !modified {
					t.Error("Expected modified=true when restoring changed field, got false")
				}

				restoredCRB, err := setup.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, tc.crbName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get ClusterRoleBinding after restoration: %v", err)
				}

				// Find the ServiceAccount subject again in the restored CRB
				if len(restoredCRB.Subjects) == 0 {
					t.Fatalf("Expected restored ClusterRoleBinding to have at least one subject, got 0")
				}

				restoredSASubjectIdx := -1
				for i, subject := range restoredCRB.Subjects {
					if subject.Kind == "ServiceAccount" && subject.Name == "secondary-scheduler" {
						restoredSASubjectIdx = i
						break
					}
				}
				if restoredSASubjectIdx == -1 {
					t.Fatalf("Expected to find ServiceAccount subject 'secondary-scheduler' in restored ClusterRoleBinding")
				}

				if restoredCRB.Subjects[restoredSASubjectIdx].Namespace != origNamespace {
					t.Errorf("Expected namespace to be restored to %q, got %q", origNamespace, restoredCRB.Subjects[restoredSASubjectIdx].Namespace)
				}

				verifyOwnerReference(t, restoredCRB)
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
				for _, resource := range []string{"serviceaccounts", "clusterrolebindings"} {
					fakeClient.PrependReactor("create", resource, func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
						action.(kubetesting.CreateAction).GetObject().(metav1.Object).SetResourceVersion(tc.resourceVersion)
						return false, nil, nil
					})
				}
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
				name: "",
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

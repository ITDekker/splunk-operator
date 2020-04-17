package testenv

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/onsi/ginkgo"
	ginkgoconfig "github.com/onsi/ginkgo/config"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
)

const (
	defaultOperatorImage = "splunk/splunk-operator"
	defaultSplunkImage   = "splunk/splunk:latest"
	defaultSparkImage    = "splunk/spark"

	// PollInterval specifies the polling interval
	PollInterval = 1 * time.Second
	// DefaultTimeout is the max timeout before we failed.
	DefaultTimeout = 5 * time.Minute
)

var (
	metricsHost                  = "0.0.0.0"
	metricsPort            int32 = 8383
	specifiedOperatorImage       = defaultOperatorImage
	specifiedSplunkImage         = defaultSplunkImage
	specifiedSparkImage          = defaultSparkImage
	specifiedSkipTeardown        = false
)

type cleanupFunc func() error

// TestEnv represents a namespaced-isolated k8s cluster environment (aka virtual k8s cluster) to run tests against
type TestEnv struct {
	kubeAPIServer      string
	name               string
	namespace          string
	serviceAccountName string
	roleName           string
	roleBindingName    string
	operatorName       string
	operatorImage      string
	splunkImage        string
	sparkImage         string
	initialized        bool
	skipTeardown       bool
	kubeClient         client.Client
	Log                logr.Logger
	cleanupFuncs       []cleanupFunc
}

func init() {
	l := zap.LoggerTo(ginkgo.GinkgoWriter)
	l.WithName("testenv")
	logf.SetLogger(l)

	flag.StringVar(&specifiedOperatorImage, "operator-image", defaultOperatorImage, "operator image to use")
	flag.StringVar(&specifiedSplunkImage, "splunk-image", defaultSplunkImage, "splunk enterprise (splunkd) image to use")
	flag.StringVar(&specifiedSparkImage, "spark-image", defaultSparkImage, "spark image to use")
	flag.BoolVar(&specifiedSkipTeardown, "skip-teardown", false, "True to skip tearing down the test env after use")
}

// GetKubeClient returns the kube client to talk to kube-apiserver
func (testenv *TestEnv) GetKubeClient() client.Client {
	return testenv.kubeClient
}

// NewDefaultTestEnv creates a default test environment
func NewDefaultTestEnv(name string) (*TestEnv, error) {
	return NewTestEnv(name, specifiedOperatorImage, specifiedSplunkImage, specifiedSparkImage)
}

// NewTestEnv creates a new test environment to run tests againsts
func NewTestEnv(name, operatorImage, splunkImage, sparkImage string) (*TestEnv, error) {

	testenv := &TestEnv{
		name:               name,
		namespace:          "ns-" + name,
		serviceAccountName: "sa-" + name,
		roleName:           "role-" + name,
		roleBindingName:    "rolebinding-" + name,
		operatorName:       "op-" + name,
		operatorImage:      operatorImage,
		splunkImage:        splunkImage,
		sparkImage:         sparkImage,
		skipTeardown:       specifiedSkipTeardown,
	}

	testenv.Log = logf.Log.WithValues("testenv", testenv.name)

	// Scheme
	enterprisev1.SchemeBuilder.AddToScheme(scheme.Scheme)

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	testenv.kubeAPIServer = cfg.Host
	testenv.Log.Info("Using kube-apiserver\n", "kube-apiserver", cfg.Host)

	//
	metricsAddr := fmt.Sprintf("%s:%d", metricsHost, metricsPort+int32(ginkgoconfig.GinkgoConfig.ParallelNode))

	kubeManager, err := manager.New(cfg, manager.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: metricsAddr,
	})
	if err != nil {
		return nil, err
	}

	testenv.kubeClient = kubeManager.GetClient()
	if testenv.kubeClient == nil {
		return nil, fmt.Errorf("kubeClient is nil")
	}

	// We need to start the manager to setup the cache. Otherwise, we have to
	// use apireader instead of kubeclient when retrieving resources
	go func() {
		err := kubeManager.Start(signals.SetupSignalHandler())
		if err != nil {
			panic("Unable to start kube manager. Error: " + err.Error())
		}
	}()

	if err := testenv.setup(); err != nil {
		// teardown() should still be invoked
		return nil, err
	}

	return testenv, nil
}

//GetName returns the name of the testenv
func (testenv *TestEnv) GetName() string {
	return testenv.name
}

func (testenv *TestEnv) setup() error {
	testenv.Log.Info("testenv initializing.\n")

	var err error
	err = testenv.createNamespace()
	if err != nil {
		return err
	}

	err = testenv.createSA()
	if err != nil {
		return err
	}

	err = testenv.createRole()
	if err != nil {
		return err
	}

	err = testenv.createRoleBinding()
	if err != nil {
		return err
	}

	err = testenv.createOperator()
	if err != nil {
		return err
	}

	testenv.initialized = true
	testenv.Log.Info("testenv initialized.\n", "namespace", testenv.namespace, "operatorImage", testenv.operatorImage, "splunkImage", testenv.splunkImage, "sparkImage", testenv.sparkImage)
	return nil
}

// Teardown cleanup the resources use in this testenv
func (testenv *TestEnv) Teardown() error {

	if testenv.skipTeardown {
		testenv.Log.Info("testenv teardown is skipped!\n")
		return nil
	}

	testenv.initialized = false

	for fn, err := testenv.popCleanupFunc(); err == nil; fn, err = testenv.popCleanupFunc() {
		cleanupErr := fn()
		if cleanupErr != nil {
			testenv.Log.Error(cleanupErr, "CleanupFunc returns an error. Attempt to continue.\n")
		}
	}

	testenv.Log.Info("testenv deleted.\n")
	return nil
}

func (testenv *TestEnv) pushCleanupFunc(fn cleanupFunc) {
	testenv.cleanupFuncs = append(testenv.cleanupFuncs, fn)
}

func (testenv *TestEnv) popCleanupFunc() (cleanupFunc, error) {
	if len(testenv.cleanupFuncs) == 0 {
		return nil, fmt.Errorf("cleanupFuncs is empty")
	}

	fn := testenv.cleanupFuncs[len(testenv.cleanupFuncs)-1]
	testenv.cleanupFuncs = testenv.cleanupFuncs[:len(testenv.cleanupFuncs)-1]

	return fn, nil
}

func (testenv *TestEnv) createNamespace() error {

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testenv.namespace,
		},
	}

	err := testenv.GetKubeClient().Create(context.TODO(), namespace)
	if err != nil {
		return err
	}

	// Cleanup the namespace when we teardown this testenv
	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), namespace)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete namespace")
			return err
		}
		if err = wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
			key := client.ObjectKey{Name: testenv.namespace, Namespace: testenv.namespace}
			ns := &corev1.Namespace{}
			err := testenv.GetKubeClient().Get(context.TODO(), key, ns)
			if errors.IsNotFound(err) {
				return true, nil
			}
			if ns.Status.Phase == corev1.NamespaceTerminating {
				return false, nil
			}

			return true, nil
		}); err != nil {
			testenv.Log.Error(err, "Unable to delete namespace")
			return err
		}

		return nil
	})

	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testenv.namespace}
		ns := &corev1.Namespace{}
		err := testenv.GetKubeClient().Get(context.TODO(), key, ns)
		if err != nil {
			// Try again
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if ns.Status.Phase == corev1.NamespaceActive {
			return true, nil
		}

		return false, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to get namespace")
		return err
	}

	return nil
}

func (testenv *TestEnv) createSA() error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testenv.serviceAccountName,
			Namespace: testenv.namespace,
		},
	}

	err := testenv.GetKubeClient().Create(context.TODO(), sa)
	if err != nil {
		testenv.Log.Error(err, "Unable to create service account")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), sa)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete service account")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestEnv) createRole() error {
	role := createRole(testenv.roleName, testenv.namespace)

	err := testenv.GetKubeClient().Create(context.TODO(), role)
	if err != nil {
		testenv.Log.Error(err, "Unable to create role")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), role)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete role")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestEnv) createRoleBinding() error {
	binding := createRoleBinding(testenv.roleBindingName, testenv.serviceAccountName, testenv.namespace, testenv.roleName)

	err := testenv.GetKubeClient().Create(context.TODO(), binding)
	if err != nil {
		testenv.Log.Error(err, "Unable to create rolebinding")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), binding)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete rolebinding")
			return err
		}
		return nil
	})

	return nil
}

func (testenv *TestEnv) createOperator() error {
	op := createOperator(testenv.operatorName, testenv.namespace, testenv.serviceAccountName, testenv.operatorImage, testenv.splunkImage, testenv.sparkImage)
	err := testenv.GetKubeClient().Create(context.TODO(), op)
	if err != nil {
		testenv.Log.Error(err, "Unable to create operator")
		return err
	}

	testenv.pushCleanupFunc(func() error {
		err := testenv.GetKubeClient().Delete(context.TODO(), op)
		if err != nil {
			testenv.Log.Error(err, "Unable to delete operator")
			return err
		}
		return nil
	})

	if err := wait.PollImmediate(PollInterval, DefaultTimeout, func() (bool, error) {
		key := client.ObjectKey{Name: testenv.operatorName, Namespace: testenv.namespace}
		deployment := &appsv1.Deployment{}
		err := testenv.GetKubeClient().Get(context.TODO(), key, deployment)
		if err != nil {
			return false, err
		}

		if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
			return false, nil
		}

		if deployment.Status.ReadyReplicas < *op.Spec.Replicas {
			return false, nil
		}

		return true, nil
	}); err != nil {
		testenv.Log.Error(err, "Unable to delete operator")
		return err
	}
	return nil
}

// NewDeployment creates a new deployment
func (testenv *TestEnv) NewDeployment(name string) (*Deployment, error) {
	d := Deployment{
		name:    testenv.GetName() + "-" + name,
		testenv: testenv,
	}

	return &d, nil
}

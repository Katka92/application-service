package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"go/build"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/pact-foundation/pact-go/dsl"
	pactTypes "github.com/pact-foundation/pact-go/types"
	appstudiov1alpha1 "github.com/redhat-appstudio/application-service/api/v1alpha1"
	"github.com/redhat-appstudio/application-service/gitops/testutils"
	devfile "github.com/redhat-appstudio/application-service/pkg/devfile"
	github "github.com/redhat-appstudio/application-service/pkg/github"
	"github.com/redhat-appstudio/application-service/pkg/spi"
	"github.com/redhat-appstudio/application-service/pkg/util/ioutils"
	appstudioshared "github.com/redhat-appstudio/managed-gitops/appstudio-shared/apis/appstudio.redhat.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	g       *gomega.WithT
	cancel1 context.CancelFunc
)

func TestContracts(t *testing.T) {
	g = gomega.NewGomegaWithT(t)

	err := setupTestEnv(t)
	if err != nil {
		t.Errorf("Failed to setup TestEnv. \n%+v", err)
	}

	pact := &dsl.Pact{
		Provider: "HAS",
	}
	pact.LogLevel = "TRACE"

	// Certificate magic - for the mocked service to be able to communicate with kube-apiserver & for authorization
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(testEnv.Config.CAData)
	certs, err := tls.X509KeyPair(testEnv.Config.CertData, testEnv.Config.KeyData)
	if err != nil {
		panic(err)
	}
	tlsConfig := &tls.Config{
		RootCAs:      caCertPool,
		Certificates: []tls.Certificate{certs},
	}
	// End of certificate magic

	//var pactDir = "/home/katka/HAC/hac-dev/pact/pacts"
	_, err = pact.VerifyProvider(t, pactTypes.VerifyRequest{
		ProviderBaseURL: testEnv.Config.Host,
		//PactURLs:        []string{filepath.ToSlash(fmt.Sprintf("%s/contractscontroller-myprovider.json", pactDir))},
		BrokerURL:      "http://pact-broker-kkanova-pact.apps.rhosd-07.hacbs.ccitredhat.com/",
		BrokerUsername: "admin",
		BrokerPassword: "A7buXM163NNyUadV",
		StateHandlers: pactTypes.StateHandlers{
			"No app with the name myapp in the default namespace exists.": func() error {
				return nil
			},
		},
		CustomTLSConfig:            tlsConfig,
		Verbose:                    true,
		PublishVerificationResults: true,
		ProviderVersion:            "1.0.0",
	})
	if err != nil {
		t.Errorf("Error while verifying tests. \n %+v", err)
	}

	cancel1()
	err = testEnv.Stop()
	if err != nil {
		fmt.Println("Stopping failed")
		fmt.Printf("%+v", err)
		panic("Cleanup failed")
	}

}

func setupTestEnv(t *testing.T) error {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel1 = context.WithCancel(context.TODO())
	managedGitOpsDepVersion := "v0.0.0-20220623041404-010a781bb3fb"

	//By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "hack", "routecrd"),
			filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "redhat-appstudio", "managed-gitops", "appstudio-shared@"+managedGitOpsDepVersion, "manifests"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Errorf("Error setting test environment: %v", err)
	}

	err = appstudiov1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("Error adding appstudiov1alpha1 scheme: %v", err)
	}

	err = routev1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("Error adding routev1 scheme: %v", err)
	}

	err = appstudioshared.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("Error adding appstudioshared scheme: %v", err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Errorf("Error creating client: %v", err)
	}

	// Setup the equivalent of what OpenShift Pipelines
	// would do.
	svcAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pipeline",
			Namespace: "default",
		},
	}

	k8sClient.Create(context.Background(), &svcAccount)

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		t.Errorf("Error creating manager: %v", err)
	}

	err = (&ApplicationReconciler{
		Client:       k8sManager.GetClient(),
		Scheme:       k8sManager.GetScheme(),
		Log:          ctrl.Log.WithName("controllers").WithName("Application"),
		GitHubClient: github.GetMockedClient(),
		GitHubOrg:    github.AppStudioAppDataOrg,
	}).SetupWithManager(k8sManager)
	if err != nil {
		t.Errorf("Error creating Applicationreconciler: %v", err)
	}

	err = (&ComponentReconciler{
		Client:          k8sManager.GetClient(),
		Scheme:          k8sManager.GetScheme(),
		Log:             ctrl.Log.WithName("controllers").WithName("Component"),
		Executor:        testutils.NewMockExecutor(),
		AppFS:           ioutils.NewMemoryFilesystem(),
		ImageRepository: "docker.io/foo/customized",
		SPIClient:       spi.MockSPIClient{},
	}).SetupWithManager(k8sManager)
	if err != nil {
		t.Errorf("Error creating ComponentReconciler: %v", err)
	}

	err = (&ComponentDetectionQueryReconciler{
		Client:             k8sManager.GetClient(),
		Scheme:             k8sManager.GetScheme(),
		Log:                ctrl.Log.WithName("controllers").WithName("ComponentDetectionQuery"),
		SPIClient:          spi.MockSPIClient{},
		AlizerClient:       devfile.MockAlizerClient{},
		DevfileRegistryURL: devfile.DevfileStageRegistryEndpoint, // Use the staging devfile registry for tests
		AppFS:              ioutils.NewMemoryFilesystem(),
	}).SetupWithManager(k8sManager)
	if err != nil {
		t.Errorf("Error creating ComponentDetectionQueryReconciler: %v", err)
	}

	err = (&ApplicationSnapshotEnvironmentBindingReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("ApplicationSnapshotEnvironmentBinding"),
		Executor: testutils.NewMockExecutor(),
		AppFS:    ioutils.NewMemoryFilesystem(),
	}).SetupWithManager(k8sManager)
	if err != nil {
		t.Errorf("Error creating ApplicationShanpshotenvironmentBindingReconciler: %v", err)
	}

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		// Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
	return nil
}

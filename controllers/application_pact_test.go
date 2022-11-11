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
	"github.com/pact-foundation/pact-go/dsl"
	pactTypes "github.com/pact-foundation/pact-go/types"
	appstudiov1alpha1 "github.com/redhat-appstudio/application-api/api/v1alpha1"
	devfile "github.com/redhat-appstudio/application-service/pkg/devfile"
	github "github.com/redhat-appstudio/application-service/pkg/github"
	"github.com/redhat-appstudio/application-service/pkg/spi"
	"github.com/redhat-appstudio/application-service/pkg/util/ioutils"

	"github.com/redhat-developer/gitops-generator/pkg/testutils"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// g       *gomega.WithT
	// k8sClient client.Client
	cancel1 context.CancelFunc
	// myTestEnv *envtest.Environment
	// ctx     context.Context
)

func TestContracts(t *testing.T) {
	// g = gomega.NewGomegaWithT(t)

	err := setuptestEnv(t)
	if err != nil {
		t.Errorf("Failed to setup testEnv. \n%+v", err)
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

	var pactDir = "/home/rhopp/Downloads"
	_, err = pact.VerifyProvider(t, pactTypes.VerifyRequest{
		ProviderBaseURL: testEnv.Config.Host,
		PactURLs:        []string{filepath.ToSlash(fmt.Sprintf("%s/hacdev-has.json", pactDir))},
		// BrokerURL:      "http://pact-broker-kkanova-pact.apps.rhosd-07.hacbs.ccitredhat.com/",
		// BrokerUsername: "admin",
		// BrokerPassword: "A7buXM163NNyUadV",
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

func setuptestEnv(t *testing.T) error {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel1 = context.WithCancel(context.TODO())
	applicationAPIDepVersion := "v0.0.0-20221108172336-c9e003808d1f"

	//By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			// filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("..", "hack", "routecrd"),
			filepath.Join(build.Default.GOPATH, "pkg", "mod", "github.com", "redhat-appstudio", "application-api@"+applicationAPIDepVersion, "manifests"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Errorf("Error setting test environment: %v", err)
	}

	klog.Info("Adding appstudio v1alpha1 to scheme")
	err = appstudiov1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("Error adding appstudiov1alpha1 scheme: %v", err)
	}

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Errorf("Error creating client: %v", err)
	}

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
		LocalClient:     k8sManager.GetClient(),
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

	err = (&SnapshotEnvironmentBindingReconciler{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName("SnapshotEnvironmentBinding"),
		Executor: testutils.NewMockExecutor(),
		AppFS:    ioutils.NewMemoryFilesystem(),
	}).SetupWithManager(k8sManager)
	if err != nil {
		t.Errorf("Error creating SnapshotEnvironmentBindingReconciler: %v", err)
	}

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		// Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
	return nil
}

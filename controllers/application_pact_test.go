package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"go/build"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/pact-foundation/pact-go/dsl"
	pactTypes "github.com/pact-foundation/pact-go/types"
	appstudiov1alpha1 "github.com/redhat-appstudio/application-api/api/v1alpha1"
	devfile "github.com/redhat-appstudio/application-service/pkg/devfile"
	github "github.com/redhat-appstudio/application-service/pkg/github"
	"github.com/redhat-appstudio/application-service/pkg/spi"
	"github.com/redhat-appstudio/application-service/pkg/util/ioutils"

	"github.com/redhat-developer/gitops-generator/pkg/testutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	var pactDir = "/home/katka/hac/hac-dev/pact/pacts"
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
			"App myapp exists and has component nodejsss and java-springboot-sample": func() error {
				appName := "myapp"
				HASAppNamespace := "default"
				compName1 := "nodejsss"
				sampleRepoLink1 := "https://github.com/devfile-samples/devfile-sample-java-springboot-basic"

				hasApp := &appstudiov1alpha1.Application{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "appstudio.redhat.com/v1alpha1",
						Kind:       "Application",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      appName,
						Namespace: HASAppNamespace,
					},
					Spec: appstudiov1alpha1.ApplicationSpec{
						DisplayName: appName,
						Description: "Some description",
					},
				}
				hasComp1 := &appstudiov1alpha1.Component{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "appstudio.redhat.com/v1alpha1",
						Kind:       "Component",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      compName1,
						Namespace: HASAppNamespace,
					},
					Spec: appstudiov1alpha1.ComponentSpec{
						ComponentName: compName1,
						Application:   appName,
						Source: appstudiov1alpha1.ComponentSource{
							ComponentSourceUnion: appstudiov1alpha1.ComponentSourceUnion{
								GitSource: &appstudiov1alpha1.GitSource{
									URL: sampleRepoLink1,
								},
							},
						},
					},
				}

				k8sClient.Create(ctx, hasApp)
				hasAppLookupKey := types.NamespacedName{Name: appName, Namespace: HASAppNamespace}
				createdHasApp := &appstudiov1alpha1.Application{}
				fmt.Println("Checking app created")
				for i := 0; i < 12; i++ {
					fmt.Println("In for")
					k8sClient.Get(context.Background(), hasAppLookupKey, createdHasApp)
					if len(createdHasApp.Status.Conditions) > 0 {
						if createdHasApp.Status.Conditions[0].Type == "Created" {
							break
						}
					}
					time.Sleep(10000)
				}

				k8sClient.Create(ctx, hasComp1)
				hasCompLookupKey := types.NamespacedName{Name: compName1, Namespace: HASAppNamespace}
				createdHasComp := &appstudiov1alpha1.Component{}
				fmt.Println("Checking comp created")
				for i := 0; i < 12; i++ {
					fmt.Println("In for")
					k8sClient.Get(context.Background(), hasCompLookupKey, createdHasComp)
					if len(createdHasComp.Status.Conditions) > 1 {
						fmt.Println("Comp: ", createdHasComp)
						break
					}
					time.Sleep(10000)
				}
				fmt.Println("Comp: ", createdHasComp)

				fmt.Println("Checking app containing component")
				for i := 0; i < 12; i++ {
					fmt.Println("In for")
					k8sClient.Get(context.Background(), hasAppLookupKey, createdHasApp)
					if len(createdHasApp.Status.Conditions) > 0 && strings.Contains(createdHasApp.Status.Devfile, compName1) {
						fmt.Println("App: ", createdHasApp.Status.Devfile)
						break
					}
					time.Sleep(10000)
				}
				fmt.Println("App devfile: ")
				fmt.Println(createdHasApp.Status.Devfile)
				return nil
			},
		},
		// AfterEach: func() error {
		// 	//Remove all applications after each tests
		// 	k8sClient.DeleteAllOf(context.Background(), &appstudiov1alpha1.Application{}, client.InNamespace("default"))
		// 	return nil
		// },
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

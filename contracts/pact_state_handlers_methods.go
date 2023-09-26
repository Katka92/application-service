//
// Copyright 2023 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package contracts

import (
	"context"
	"reflect"
	"strings"
	"time"

	gomega "github.com/onsi/gomega"
	models "github.com/pact-foundation/pact-go/v2/models"
	appstudiov1alpha1 "github.com/redhat-appstudio/application-api/api/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

const timeout = 10 * time.Second
const interval = 250 * time.Millisecond

func createApp(setup bool, state models.ProviderState) (models.ProviderStateResponse, error) {
	params := parseApp(state.Parameters)
	hasApp := getApplicationSpec(params.appName, params.namespace)
	hasAppLookupKey := types.NamespacedName{Name: params.appName, Namespace: params.namespace}
	createdHasApp := &appstudiov1alpha1.Application{}

	if !setup {
		// teardown test phase, cleaning env
		cleanUpNamespace(params.namespace)
		return nil, nil
	}

	// check that app is not here
	gomega.Eventually(func() bool {
		k8sClient.Get(context.Background(), hasAppLookupKey, createdHasApp)
		println("I should be false from state: ", state.Name)
		return len(createdHasApp.Status.Conditions) > 0
	}, timeout, interval).Should(gomega.BeFalse())

	// create app
	gomega.Expect(k8sClient.Create(ctx, hasApp)).Should(gomega.Succeed())

	// check it is created
	gomega.Eventually(func() bool {
		k8sClient.Get(context.Background(), hasAppLookupKey, createdHasApp)
		return len(createdHasApp.Status.Conditions) > 0
	}, timeout, interval).Should(gomega.BeTrue())

	return nil, nil

}

func createComponents(setup bool, state models.ProviderState) (models.ProviderStateResponse, error) {
	components := parseComp(state.Parameters)

	if !setup {
		// teardown test phase, cleaning env
		cleanUpNamespace(components[0].app.namespace)
		return nil, nil
	}

	for _, comp := range components {
		ghComp := getGhComponentSpec(comp.name, comp.app.namespace, comp.app.appName, comp.repo)

		hasAppLookupKey := types.NamespacedName{Name: comp.app.appName, Namespace: comp.app.namespace}
		createdHasApp := &appstudiov1alpha1.Application{}

		//create gh component
		gomega.Expect(k8sClient.Create(ctx, ghComp)).Should(gomega.Succeed())
		hasCompLookupKey := types.NamespacedName{Name: comp.name, Namespace: comp.app.namespace}
		createdHasComp := &appstudiov1alpha1.Component{}
		gomega.Eventually(func() bool {
			k8sClient.Get(context.Background(), hasCompLookupKey, createdHasComp)
			return len(createdHasComp.Status.Conditions) > 1
		}, timeout, interval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			k8sClient.Get(context.Background(), hasAppLookupKey, createdHasApp)
			return len(createdHasApp.Status.Conditions) > 0 && strings.Contains(createdHasApp.Status.Devfile, comp.name)
		}, timeout, interval).Should(gomega.BeTrue())
	}
	return nil, nil

}

func appDoesntExist(setup bool, state models.ProviderState) (models.ProviderStateResponse, error) {
	app := parseApp(state.Parameters)
	if !setup {
		// teardown test phase, cleaning env
		cleanUpNamespace(app.namespace)
		return nil, nil
	}

	hasApp := getApplicationSpec(app.appName, app.namespace)

	k8sClient.Delete(context.Background(), hasApp)

	return nil, nil
}

func cleanUpNamespace(namespace string) {
	removeAllInstacesInNamespace(namespace, &appstudiov1alpha1.Component{})
	removeAllInstacesInNamespace(namespace, &appstudiov1alpha1.Application{})
	removeAllInstacesInNamespace(namespace, &appstudiov1alpha1.ComponentDetectionQuery{})
}

func removeAllInstacesInNamespace(namespace string, myInstance client.Object) {
	namespace = ""
	// remove resources in namespace
	k8sClient.DeleteAllOf(context.Background(), myInstance, client.InNamespace(namespace))

	// watch number of resources existing
	gomega.Eventually(func() bool {
		objectKind := strings.Split(reflect.TypeOf(myInstance).String(), ".")[1]
		remainingCount := getObjectCountInNamespace(objectKind, namespace)
		println("Instances remaining of type", objectKind, ": ", remainingCount)
		return remainingCount == 0
	}, timeout, interval).Should(gomega.BeTrue())
}

func getObjectCountInNamespace(objectKind string, namespace string) int {
	unstructuredObject := &unstructured.Unstructured{}

	unstructuredObject.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   appstudiov1alpha1.GroupVersion.Group,
		Version: appstudiov1alpha1.GroupVersion.Version,
		Kind:    objectKind,
	})

	k8sClient.List(context.Background(), unstructuredObject, &client.ListOptions{Namespace: namespace})
	listOfObjects, _ := unstructuredObject.ToList()

	return len(listOfObjects.Items)
}

func cleanUpNamespaces() {
	println("clean up namespaces")
	removeAllInstances(&appstudiov1alpha1.Component{})
	removeAllInstances(&appstudiov1alpha1.Application{})
	removeAllInstances(&appstudiov1alpha1.ComponentDetectionQuery{})
}

func removeAllInstances(myInstance client.Object) {
	objectKind := strings.Split(reflect.TypeOf(myInstance).String(), ".")[1]
	listOfNamespaces := getListOfNamespaces(objectKind)
	for namespace := range listOfNamespaces {
		println("namespace: ", namespace)
		k8sClient.DeleteAllOf(context.Background(), myInstance, client.InNamespace(namespace))

		gomega.Eventually(func() bool {
			objectKind := strings.Split(reflect.TypeOf(myInstance).String(), ".")[1]
			remainingCount := getObjectCountInNamespace(objectKind, namespace)
			println("Instances remaining of type", objectKind, ": ", remainingCount)
			return remainingCount == 0
		}, timeout, interval).Should(gomega.BeTrue())
	}

}

func getListOfNamespaces(objectKind string) map[string]struct{} {
	unstructuredObject := &unstructured.Unstructured{}

	unstructuredObject.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   appstudiov1alpha1.GroupVersion.Group,
		Version: appstudiov1alpha1.GroupVersion.Version,
		Kind:    objectKind,
	})

	k8sClient.List(context.Background(), unstructuredObject, &client.ListOptions{Namespace: ""})
	listOfObjects, _ := unstructuredObject.ToList()
	println("Found", len(listOfObjects.Items), "items of", objectKind, "objects.")
	namespaces := make(map[string]struct{})
	for _, item := range listOfObjects.Items {
		myobj := item.Object
		namespace := myobj["metadata"].(map[string]interface{})["namespace"].(string)
		namespaces[namespace] = struct{}{}
	}
	return namespaces
}

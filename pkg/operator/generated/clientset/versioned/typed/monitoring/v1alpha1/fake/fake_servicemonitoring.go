// Copyright 2021 The gpe-collector authors.
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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/google/gpe-collector/pkg/operator/apis/monitoring/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeServiceMonitorings implements ServiceMonitoringInterface
type FakeServiceMonitorings struct {
	Fake *FakeMonitoringV1alpha1
	ns   string
}

var servicemonitoringsResource = schema.GroupVersionResource{Group: "monitoring.googleapis.com", Version: "v1alpha1", Resource: "servicemonitorings"}

var servicemonitoringsKind = schema.GroupVersionKind{Group: "monitoring.googleapis.com", Version: "v1alpha1", Kind: "ServiceMonitoring"}

// Get takes name of the serviceMonitoring, and returns the corresponding serviceMonitoring object, and an error if there is any.
func (c *FakeServiceMonitorings) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ServiceMonitoring, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(servicemonitoringsResource, c.ns, name), &v1alpha1.ServiceMonitoring{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ServiceMonitoring), err
}

// List takes label and field selectors, and returns the list of ServiceMonitorings that match those selectors.
func (c *FakeServiceMonitorings) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ServiceMonitoringList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(servicemonitoringsResource, servicemonitoringsKind, c.ns, opts), &v1alpha1.ServiceMonitoringList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ServiceMonitoringList{ListMeta: obj.(*v1alpha1.ServiceMonitoringList).ListMeta}
	for _, item := range obj.(*v1alpha1.ServiceMonitoringList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested serviceMonitorings.
func (c *FakeServiceMonitorings) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(servicemonitoringsResource, c.ns, opts))

}

// Create takes the representation of a serviceMonitoring and creates it.  Returns the server's representation of the serviceMonitoring, and an error, if there is any.
func (c *FakeServiceMonitorings) Create(ctx context.Context, serviceMonitoring *v1alpha1.ServiceMonitoring, opts v1.CreateOptions) (result *v1alpha1.ServiceMonitoring, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(servicemonitoringsResource, c.ns, serviceMonitoring), &v1alpha1.ServiceMonitoring{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ServiceMonitoring), err
}

// Update takes the representation of a serviceMonitoring and updates it. Returns the server's representation of the serviceMonitoring, and an error, if there is any.
func (c *FakeServiceMonitorings) Update(ctx context.Context, serviceMonitoring *v1alpha1.ServiceMonitoring, opts v1.UpdateOptions) (result *v1alpha1.ServiceMonitoring, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(servicemonitoringsResource, c.ns, serviceMonitoring), &v1alpha1.ServiceMonitoring{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ServiceMonitoring), err
}

// Delete takes name of the serviceMonitoring and deletes it. Returns an error if one occurs.
func (c *FakeServiceMonitorings) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(servicemonitoringsResource, c.ns, name), &v1alpha1.ServiceMonitoring{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeServiceMonitorings) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(servicemonitoringsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ServiceMonitoringList{})
	return err
}

// Patch applies the patch and returns the patched serviceMonitoring.
func (c *FakeServiceMonitorings) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ServiceMonitoring, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(servicemonitoringsResource, c.ns, name, pt, data, subresources...), &v1alpha1.ServiceMonitoring{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ServiceMonitoring), err
}

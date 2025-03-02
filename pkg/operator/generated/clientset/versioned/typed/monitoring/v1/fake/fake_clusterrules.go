// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
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

	monitoringv1 "github.com/GoogleCloudPlatform/prometheus-engine/pkg/operator/apis/monitoring/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeClusterRules implements ClusterRulesInterface
type FakeClusterRules struct {
	Fake *FakeMonitoringV1
}

var clusterrulesResource = schema.GroupVersionResource{Group: "monitoring.googleapis.com", Version: "v1", Resource: "clusterrules"}

var clusterrulesKind = schema.GroupVersionKind{Group: "monitoring.googleapis.com", Version: "v1", Kind: "ClusterRules"}

// Get takes name of the clusterRules, and returns the corresponding clusterRules object, and an error if there is any.
func (c *FakeClusterRules) Get(ctx context.Context, name string, options v1.GetOptions) (result *monitoringv1.ClusterRules, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(clusterrulesResource, name), &monitoringv1.ClusterRules{})
	if obj == nil {
		return nil, err
	}
	return obj.(*monitoringv1.ClusterRules), err
}

// List takes label and field selectors, and returns the list of ClusterRules that match those selectors.
func (c *FakeClusterRules) List(ctx context.Context, opts v1.ListOptions) (result *monitoringv1.ClusterRulesList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(clusterrulesResource, clusterrulesKind, opts), &monitoringv1.ClusterRulesList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &monitoringv1.ClusterRulesList{ListMeta: obj.(*monitoringv1.ClusterRulesList).ListMeta}
	for _, item := range obj.(*monitoringv1.ClusterRulesList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested clusterRules.
func (c *FakeClusterRules) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(clusterrulesResource, opts))
}

// Create takes the representation of a clusterRules and creates it.  Returns the server's representation of the clusterRules, and an error, if there is any.
func (c *FakeClusterRules) Create(ctx context.Context, clusterRules *monitoringv1.ClusterRules, opts v1.CreateOptions) (result *monitoringv1.ClusterRules, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(clusterrulesResource, clusterRules), &monitoringv1.ClusterRules{})
	if obj == nil {
		return nil, err
	}
	return obj.(*monitoringv1.ClusterRules), err
}

// Update takes the representation of a clusterRules and updates it. Returns the server's representation of the clusterRules, and an error, if there is any.
func (c *FakeClusterRules) Update(ctx context.Context, clusterRules *monitoringv1.ClusterRules, opts v1.UpdateOptions) (result *monitoringv1.ClusterRules, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(clusterrulesResource, clusterRules), &monitoringv1.ClusterRules{})
	if obj == nil {
		return nil, err
	}
	return obj.(*monitoringv1.ClusterRules), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeClusterRules) UpdateStatus(ctx context.Context, clusterRules *monitoringv1.ClusterRules, opts v1.UpdateOptions) (*monitoringv1.ClusterRules, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(clusterrulesResource, "status", clusterRules), &monitoringv1.ClusterRules{})
	if obj == nil {
		return nil, err
	}
	return obj.(*monitoringv1.ClusterRules), err
}

// Delete takes name of the clusterRules and deletes it. Returns an error if one occurs.
func (c *FakeClusterRules) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(clusterrulesResource, name, opts), &monitoringv1.ClusterRules{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeClusterRules) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(clusterrulesResource, listOpts)

	_, err := c.Fake.Invokes(action, &monitoringv1.ClusterRulesList{})
	return err
}

// Patch applies the patch and returns the patched clusterRules.
func (c *FakeClusterRules) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *monitoringv1.ClusterRules, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(clusterrulesResource, name, pt, data, subresources...), &monitoringv1.ClusterRules{})
	if obj == nil {
		return nil, err
	}
	return obj.(*monitoringv1.ClusterRules), err
}

// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package botanist

import (
	"context"
	"fmt"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeleteManagedResources deletes all managed resources labeled with `origin=gardener` from the Shoot namespace in the Seed.
func (b *Botanist) DeleteManagedResources(ctx context.Context) error {
	return b.SeedClientSet.Client().DeleteAllOf(
		ctx,
		&resourcesv1alpha1.ManagedResource{},
		client.InNamespace(b.Shoot.SeedNamespace),
		client.MatchingLabels{managedresources.LabelKeyOrigin: managedresources.LabelValueGardener},
	)
}

// WaitUntilManagedResourcesDeleted waits until all managed resources labeled with `origin=gardener` are gone or the context is cancelled.
func (b *Botanist) WaitUntilManagedResourcesDeleted(ctx context.Context) error {
	return b.waitUntilManagedResourceAreDeleted(ctx, client.InNamespace(b.Shoot.SeedNamespace), client.MatchingLabels{managedresources.LabelKeyOrigin: managedresources.LabelValueGardener})
}

func (b *Botanist) waitUntilManagedResourceAreDeleted(ctx context.Context, listOpt ...client.ListOption) error {
	return managedresources.WaitUntilListDeleted(ctx, b.SeedClientSet.Client(), &resourcesv1alpha1.ManagedResourceList{}, listOpt...)
}

// KeepObjectsForManagedResources sets ManagedResource.Spec.KeepObjects to true.
func (b *Botanist) KeepObjectsForManagedResources(ctx context.Context) error {
	managedResources := &resourcesv1alpha1.ManagedResourceList{}
	if err := b.SeedClientSet.Client().List(ctx, managedResources, client.InNamespace(b.Shoot.SeedNamespace), client.MatchingLabels{managedresources.LabelKeyOrigin: managedresources.LabelValueGardener}); err != nil {
		return fmt.Errorf("failed to list all managed resource, %w", err)
	}

	for _, resource := range managedResources.Items {
		if err := managedresources.SetKeepObjects(ctx, b.SeedClientSet.Client(), resource.Namespace, resource.Name, true); err != nil {
			return err
		}
	}

	return nil
}

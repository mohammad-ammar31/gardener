// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package shootsecret

import (
	"context"
	"encoding/json"
	"fmt"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1alpha1helper "github.com/gardener/gardener/pkg/apis/core/v1alpha1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/extensions"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const finalizerName = "gardenlet.gardener.cloud/secret-controller"

type reconciler struct {
	gardenClient client.Client
	seedClient   client.Client
}

// NewReconciler returns a new reconciler for secrets related to shoots.
func NewReconciler(gardenClient, seedClient client.Client) reconcile.Reconciler {
	return &reconciler{
		gardenClient: gardenClient,
		seedClient:   seedClient,
	}
}

func (r *reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{}
	if err := r.seedClient.Get(ctx, request.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving object from store: %w", err)
	}

	namespace := &corev1.Namespace{}
	if err := r.seedClient.Get(ctx, kutil.Key(secret.Namespace), namespace); err != nil {
		return reconcile.Result{}, err
	}
	if namespace.Labels[v1beta1constants.GardenRole] != v1beta1constants.GardenRoleShoot {
		return reconcile.Result{}, nil
	}

	shootState, shoot, err := extensions.GetShootStateForCluster(ctx, r.gardenClient, r.seedClient, secret.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if controllerutil.ContainsFinalizer(secret, finalizerName) {
				log.Info("Removing finalizer")
				if err := controllerutils.RemoveFinalizers(ctx, r.seedClient, secret, finalizerName); err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
				}
			}
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if secret.DeletionTimestamp != nil {
		return r.delete(ctx, log, secret, shootState, shoot)
	}
	return r.reconcile(ctx, log, secret, shootState)
}

func (r *reconciler) reconcile(
	ctx context.Context,
	log logr.Logger,
	secret *corev1.Secret,
	shootState *gardencorev1alpha1.ShootState,
) (
	reconcile.Result,
	error,
) {
	log.Info("Reconciling secret information in ShootState and ensuring its finalizer")

	if !controllerutil.ContainsFinalizer(secret, finalizerName) {
		log.Info("Adding finalizer")
		if err := controllerutils.AddFinalizers(ctx, r.seedClient, secret, finalizerName); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
	}

	dataJSON, err := json.Marshal(secret.Data)
	if err != nil {
		return reconcile.Result{}, err
	}

	patch := client.StrategicMergeFrom(shootState.DeepCopy())

	dataList := gardencorev1alpha1helper.GardenerResourceDataList(shootState.Spec.Gardener)
	dataList.Upsert(&gardencorev1alpha1.GardenerResourceData{
		Name:   secret.Name,
		Labels: secret.Labels,
		Type:   "secret",
		Data:   runtime.RawExtension{Raw: dataJSON},
	})
	shootState.Spec.Gardener = dataList

	return reconcile.Result{}, r.gardenClient.Patch(ctx, shootState, patch)
}

func (r *reconciler) delete(
	ctx context.Context,
	log logr.Logger,
	secret *corev1.Secret,
	shootState *gardencorev1alpha1.ShootState,
	shoot *gardencorev1beta1.Shoot,
) (
	reconcile.Result,
	error,
) {
	if lastOp := shoot.Status.LastOperation; lastOp != nil && lastOp.Type == gardencorev1beta1.LastOperationTypeMigrate {
		log.Info("Keeping Secret in ShootState since Shoot is in migration but releasing the finalizer")
	} else {
		log.Info("Removing Secret from ShootState and releasing its finalizer")

		patch := client.StrategicMergeFrom(shootState.DeepCopy())

		dataList := gardencorev1alpha1helper.GardenerResourceDataList(shootState.Spec.Gardener)
		dataList.Delete(secret.Name)
		shootState.Spec.Gardener = dataList

		if err := r.gardenClient.Patch(ctx, shootState, patch); err != nil {
			return reconcile.Result{}, err
		}
	}

	if controllerutil.ContainsFinalizer(secret, finalizerName) {
		log.Info("Removing finalizer")
		if err := controllerutils.RemoveFinalizers(ctx, r.seedClient, secret, finalizerName); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
		}
	}

	return reconcile.Result{}, nil
}

/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	leaderelectionv1 "github.com/600lyy/multi-cluster-leader-election/api/v1"
	"github.com/600lyy/multi-cluster-leader-election/pkg/jitter"
	"github.com/600lyy/multi-cluster-leader-election/pkg/leaderelection"
	"github.com/600lyy/multi-cluster-leader-election/pkg/resourcelock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	leaseOperatorAnnotation = "leaderelection.600lyy.io/lease-operator"
	finalizerName           = "leaderelection.600lyy.io/finalizer"
)

var (
	ignoredNamespaces = []string{
		"cert-manager",
		"cnrm-system",
		"gke-managed-cim",
		"gmp-public",
		"gmp-system",
		"kube-node-lease",
		"kube-public",
		"multi-cluster-leader-election-system",
		"ssa-demo",
		"kube-system",
	}
)

// LeaseReconciler reconciles a Lease object
// LeaseReconciler watches namespace objects
type LeaseReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	LeaderElector *leaderelection.LeaderElector
}

//+kubebuilder:rbac:groups=leaderelection.600lyy.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=leaderelection.600lyy.io,resources=leases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=leaderelection.600lyy.io,resources=leases/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Lease object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling multi-cluster lease")

	namespaceObj := &corev1.Namespace{}
	err := r.Get(ctx, req.NamespacedName, namespaceObj)
	if err != nil {
		log.Error(err, "namespace not found", "namespace", req.NamespacedName.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if isIgnoredNamespace(namespaceObj.Name) {
		log.Info("ingore system managed namespaces", "namespace", namespaceObj.Name)
		return ctrl.Result{}, nil
	}
	// When user attempts to delete a resource, the API server handling the delete
	// request notices the values in the "finalizer" field and add a metadata.deletionTimestamp
	// field with time user started the deleteion. This field indicates the deletion of the object
	// has been requested, but the deletion will not be complete until all the finailzers are removed.
	// For more details, check Finalizer and Deletion:
	// - https://kubernetes.io/blog/2021/05/14/using-finalizers-to-control-deletion/
	if namespaceObj.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add a finalizer if not present to delete its lease object
		if !controllerutil.ContainsFinalizer(namespaceObj, finalizerName) {
			log.Info("adding finalizer to the namespace", "namespace", namespaceObj.Name)
			namespaceObj.ObjectMeta.Finalizers = append(namespaceObj.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(ctx, namespaceObj); err != nil {
				log.Error(err, "unable to update namespace", "namespace", namespaceObj.Name)
				return ctrl.Result{}, err
			}
		}
		// Attempt to fetch lease, creat a new one if not present
		lease := &leaderelectionv1.Lease{}
		err := r.Get(ctx, client.ObjectKey{Name: namespaceObj.Name}, lease)
		if err != nil {
			if apierrors.IsNotFound(err) {
				now := metav1.NewTime(r.LeaderElector.Clock.Now())
				ler := &resourcelock.LeaderElectionRecord{
					HolderIdentity:       "Unknown",
					LeaseDurationSeconds: int(r.LeaderElector.Config.LeaseDuration / time.Second),
					AcquireTime:          now,
					RenewTime:            now,
				}
				lease = &leaderelectionv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespaceObj.Name,
						Name:      "lease-gsc",
						Annotations: map[string]string{
							"managed-by": leaseOperatorAnnotation,
						},
					},
					Spec: resourcelock.LeaderElectionRecordToLeaseSpec(ler),
				}
				// Create a lease object for the namespace, but hold off to create the lease
				// in the google storage bucket until next reconcile
				if err = r.Create(ctx, lease); err != nil {
					log.Error(err, "failed to create the lease object for namespace", "ns", namespaceObj.Name)
					return ctrl.Result{}, err
				}
			} else {
				log.Error(err, "unable to fetch the lease object for namespace", "ns", namespaceObj.Name)
				return ctrl.Result{}, err
			}
		} else {
			// hanlde update or reconcile
			cctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if err = r.LeaderElector.TryAcquireOrRenew(cctx); err != nil {
				log.Error(err, "unbale to acquire or renew the lease file in storage bucket")
				return ctrl.Result{}, err
			}
			// lease.Spec.HolderIdentity = r.LeaderElector.GetLeader()
			// update lease status
			lease.Status = resourcelock.LeaderElectionRecordToLeaseStatus(&r.LeaderElector.ObservedRecord)
			if err = r.Status().Update(ctx, lease); err != nil {
				log.Error(err, "unable to update lease status")
				return ctrl.Result{}, err
			}
		}
		if reconcileReenqueuePeriod, err := jitter.GenerateJitterReenqueuePeriod(lease); err != nil {
			return ctrl.Result{}, err
		} else {
			log.Info("successully finished reconcile", "lease", lease.Name, "time to next reconcile", reconcileReenqueuePeriod)
			return ctrl.Result{RequeueAfter: reconcileReenqueuePeriod}, nil
		}
	} else {
		// hanlde delete
		if controllerutil.ContainsFinalizer(namespaceObj, finalizerName) {
			log.Info("finalizer found, deleting the lease object for namespace")
			lease := &leaderelectionv1.Lease{}
			if err := r.Get(ctx, client.ObjectKey{Name: namespaceObj.Name}, lease); err != nil {
				log.Error(err, "unable to fetch the lease object for namespace", "ns", namespaceObj.Name)
				return ctrl.Result{}, err
			}

			if err := r.Delete(ctx, lease); err != nil {
				log.Error(err, "unable to delete the lease for namespace", "namespace", namespaceObj.Name)
				return ctrl.Result{}, err
			}
			log.Info("Lease for the namespace deleted", "namespace", namespaceObj.Name)

			// Remove the finalizer and udpate the namespace object
			controllerutil.RemoveFinalizer(namespaceObj, finalizerName)
			if err := r.Update(ctx, namespaceObj); err != nil {
				log.Error(err, "Unable to remove finalizer and update namespace", "namespace", namespaceObj.Name)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Complete(r)
}

func isIgnoredNamespace(namespace string) bool {
	for _, ns := range ignoredNamespaces {
		if namespace == ns {
			return true
		}
	}
	return false
}

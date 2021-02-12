package string

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/mittwald/kubernetes-secret-generator/pkg/apis/types/v1alpha1"
	"github.com/mittwald/kubernetes-secret-generator/pkg/controller/crd"
	"github.com/mittwald/kubernetes-secret-generator/pkg/controller/secret"
)

var log = logf.Log.WithName("controller_string_secret")

// Add creates a new Secret Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileString{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

type MyCRStatus struct {
	// +kubebuilder:validation:Enum=Success,Failure
	Status     string      `json:"status,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

type ReconcileString struct {
	// This Client, initialized using mgr.Client() above, is a split Client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("string-secret-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	// Watch for changes to primary resource Secret
	err = c.Watch(&source.Kind{Type: &v1alpha1.String{}}, &handler.EnqueueRequestForObject{}, ignoreDeletionPredicate())
	if err != nil {
		return err
	}

	return nil
}

func ignoreDeletionPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore updates to CR status in which case metadata.Generation does not change
			return e.MetaOld.GetGeneration() != e.MetaNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
}

// Reconcile reads that state of the cluster for a Secret object and makes changes based on the state read
// and what is in the Secret.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileString) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	fmt.Println("reconciling")
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling String")
	ctx := context.TODO()
	// Fetch the Secret instance
	instance := &v1alpha1.String{}
	err := r.client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	fieldNames := instance.Spec.FieldNames
	length := instance.Spec.Length
	encoding := instance.Spec.Encoding
	data := instance.Spec.Data
	secretType := instance.Spec.Type
	recreate := instance.Spec.ForceRecreate

	values := make(map[string][]byte)

	for key := range data {
		values[key] = []byte(data[key])
	}

	secretLength, isByteLength, err := crd.ParseByteLength(secret.SecretLength(), length)
	if err != nil {
		//TODO errorstuff
	}

	existing := &v1.Secret{}
	err = r.client.Get(ctx, request.NamespacedName, existing)
	// secret not found, create new one
	if errors.IsNotFound(err) {
		for _, field := range fieldNames {
			randomString, randErr := secret.GenerateRandomString(secretLength, encoding, isByteLength)
			if randErr != nil {
				reqLogger.Error(err, "could not generate new random string")
				return reconcile.Result{RequeueAfter: time.Second * 30}, err
			}
			values[field] = randomString
		}
		desiredSecret := crd.NewSecret(instance, values, secretType)

		err = r.client.Create(ctx, desiredSecret)
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// TODO do error stuff
			} else {
				return reconcile.Result{Requeue: true}, err
			}
		}

		stringRef, err := reference.GetReference(r.scheme, desiredSecret)
		if err != nil {
			return reconcile.Result{}, err
		}
		status := instance.GetStatus()
		status.SetState(v1alpha1.ReconcilerStateCompleted)
		status.SetSecret(stringRef)

		if err := r.client.Status().Update(ctx, instance); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	} else {
		// other error occurred
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	// no errors, so secret exists

	for _, field := range fieldNames {
		if string(existing.Data[field]) == "" || recreate {
			randomString, randErr := secret.GenerateRandomString(secretLength, encoding, isByteLength)
			if randErr != nil {
				reqLogger.Error(err, "could not generate new random string")
				return reconcile.Result{RequeueAfter: time.Second * 30}, err
			}
			values[field] = randomString
		}
	}

	// check if secret was created by this cr
	existingOwnerRefs := existing.OwnerReferences
	ownedByCR := false
	for _, ref := range existingOwnerRefs {
		if ref.Kind != "String" {
			continue
		} else {
			ownedByCR = true
			break
		}

	}
	if !ownedByCR {
		// secret is not owned by cr, do nothing
		reqLogger.Info("secret not generated by this cr, skipping")
		return reconcile.Result{}, nil
	}
	targetSec := existing.DeepCopy()

	// Add new keys to data
	for key := range values {
		targetSec.Data[key] = values[key]
	}

	err = r.client.Update(ctx, targetSec)
	if err != nil {
		return reconcile.Result{Requeue: true}, err
	}

	var stringRef *v1.ObjectReference
	stringRef, err = reference.GetReference(r.scheme, targetSec)
	if err != nil {
		return reconcile.Result{}, err
	}

	// set status information TODO do something useful with this
	status := instance.GetStatus()
	status.SetState(v1alpha1.ReconcilerStateCompleted)
	status.SetSecret(stringRef)

	if err := r.client.Status().Update(ctx, instance); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil

}

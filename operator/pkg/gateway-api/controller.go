// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package gateway_api

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/logging/logfields"
)

const (
	// controllerName is the gateway controller name used in cilium.
	controllerName = "io.cilium/gateway-controller"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(gatewayv1beta1.AddToScheme(scheme))
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ciliumv2.AddToScheme(scheme))
}

type internalModel struct {
	// TODO(tam): I am not sure if we need to cache anything for performance gain,
	// the client is reading from cache already.
}

type Controller struct {
	mgr ctrl.Manager

	model *internalModel
}

// NewController returns a new gateway controller, which is implemented
// using the controller-runtime library.
func NewController(secretsNamespace string) (*Controller, error) {
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	m := new(internalModel)

	gwcReconciler := &gatewayClassReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Model:          m,
		controllerName: controllerName,
	}
	if err = gwcReconciler.SetupWithManager(mgr); err != nil {
		return nil, err
	}

	gwReconciler := &gatewayReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		SecretsNamespace: secretsNamespace,
		Model:            m,
		controllerName:   controllerName,
	}
	if err = gwReconciler.SetupWithManager(mgr); err != nil {
		return nil, err
	}

	hrReconciler := &httpRouteReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Model:  m,
	}
	if err = hrReconciler.SetupWithManager(mgr); err != nil {
		return nil, err
	}

	return &Controller{
		mgr:   mgr,
		model: m,
	}, nil
}

func (m *Controller) Run() {
	if err := m.mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.WithField(logfields.Controller, "gateway-api").WithError(err).Error("Unable to start controller")
	}
}

func hasMatchingController(ctx context.Context, c client.Client, controllerName string) func(object client.Object) bool {
	return func(obj client.Object) bool {
		scopedLog := log.WithFields(logrus.Fields{
			logfields.Controller: "gateway",
			logfields.Resource:   obj.GetName(),
		})
		gw, ok := obj.(*gatewayv1beta1.Gateway)
		if !ok {
			return false
		}

		gwc := &gatewayv1beta1.GatewayClass{}
		key := types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}
		if err := c.Get(ctx, key, gwc); err != nil {
			scopedLog.WithError(err).Error("Unable to get GatewayClass")
			return false
		}

		return string(gwc.Spec.ControllerName) == controllerName
	}
}

func getGatewaysForSecret(ctx context.Context, c client.Client, obj client.Object) []types.NamespacedName {
	scopedLog := log.WithFields(logrus.Fields{
		logfields.Controller: gateway,
		logfields.Resource:   obj.GetName(),
	})

	gwList := &gatewayv1beta1.GatewayList{}
	if err := c.List(ctx, gwList); err != nil {
		scopedLog.WithError(err).Warn("Unable to list Gateways")
		return nil
	}

	var gateways []types.NamespacedName
	for _, gw := range gwList.Items {
		for _, l := range gw.Spec.Listeners {
			if l.TLS == nil {
				continue
			}

			for _, cert := range l.TLS.CertificateRefs {
				if !IsSecret(cert) {
					continue
				}
				ns := namespaceDerefOr(cert.Namespace, gw.GetNamespace())
				if string(cert.Name) == obj.GetName() &&
					ns == obj.GetNamespace() {
					gateways = append(gateways, client.ObjectKey{
						Namespace: ns,
						Name:      gw.GetName(),
					})
				}
			}
		}
	}
	return gateways
}

func onlyStatusChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectOld.(type) {
			case *gatewayv1beta1.GatewayClass:
				o, ok := e.ObjectOld.(*gatewayv1beta1.GatewayClass)
				if !ok {
					return false
				}
				n, ok := e.ObjectNew.(*gatewayv1beta1.GatewayClass)
				if !ok {
					return false
				}
				return !cmp.Equal(o.Status, n.Status)
			case *gatewayv1beta1.Gateway:
				o, ok := e.ObjectOld.(*gatewayv1beta1.Gateway)
				if !ok {
					return false
				}
				n, ok := e.ObjectNew.(*gatewayv1beta1.Gateway)
				if !ok {
					return false
				}
				return !cmp.Equal(o.Status, n.Status)
			case *gatewayv1beta1.HTTPRoute:
				o, ok := e.ObjectOld.(*gatewayv1beta1.HTTPRoute)
				if !ok {
					return false
				}
				n, ok := e.ObjectNew.(*gatewayv1beta1.HTTPRoute)
				if !ok {
					return false
				}
				return !cmp.Equal(o.Status, n.Status)
			default:
				return false
			}
		},
	}
}

func success() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func fail(e error) (ctrl.Result, error) {
	return ctrl.Result{}, e
}

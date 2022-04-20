package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	"github.com/maistra/istio-operator/pkg/apis/external"
	jaegerv1 "github.com/maistra/istio-operator/pkg/apis/external/jaeger/v1"
	kialiv1alpha1 "github.com/maistra/istio-operator/pkg/apis/external/kiali/v1alpha1"
	maistrav1 "github.com/maistra/istio-operator/pkg/apis/maistra/v1"
	maistrav2 "github.com/maistra/istio-operator/pkg/apis/maistra/v2"
	"github.com/maistra/istio-operator/pkg/controller/common/cni"
	. "github.com/maistra/istio-operator/pkg/controller/common/test"
	"github.com/maistra/istio-operator/pkg/controller/versions"
	routev1 "github.com/openshift/api/route/v1"
)

var featureEnabled = maistrav2.Enablement{
	Enabled: ptrTrue,
}
var featureDisabled = maistrav2.Enablement{
	Enabled: ptrFalse,
}

func TestAddonsInstall(t *testing.T) {
	const (
		operatorNamespace  = "istio-operator"
		smcpName           = "test"
		cniDaemonSetName   = "istio-node"
		domain             = "test.com"
		kialiName          = "kiali"
		kialiExistingName  = "kiali-existing"
		jaegerName         = "jaeger"
		jaegerExistingName = "jaeger-existing"
	)

	if testing.Verbose() {
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr), zap.Level(zapcore.Level(-5))))
	}

	jaegerRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jaegerExistingName,
			Namespace: controlPlaneNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance":  jaegerExistingName,
				"app.kubernetes.io/component": "query-route",
			},
		},
		Spec: routev1.RouteSpec{
			Host: fmt.Sprintf("%s.%s", jaegerExistingName, domain),
		},
	}
	jaegerExisting := &jaegerv1.Jaeger{
		Base: external.Base{
			ObjectMeta: metav1.ObjectMeta{Name: jaegerExistingName, Namespace: controlPlaneNamespace},
		},
	}

	testCases := []IntegrationTestCase{
		{
			name: "kiali.install.jaeger.install",
			smcp: NewSMCPForKialiJaegerTests(smcpName, kialiName, "", versions.V2_0.String()),
			create: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
		{
			name: "kiali.install.jaeger.existing",
			smcp: NewSMCPForKialiJaegerTests(smcpName, "", jaegerExistingName, versions.V2_0.String()),
			resources: []runtime.Object{
				jaegerExisting,
				jaegerRoute,
			},
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).Passes(ExpectedKialiCreate(jaegerExistingName, domain)),
				),
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
		},
		{
			name: "kiali.existing.jaeger.install",
			smcp: NewSMCPForKialiJaegerTests(smcpName, kialiName, "", versions.V2_0.String()),
			resources: []runtime.Object{
				&kialiv1alpha1.Kiali{Base: external.Base{
					ObjectMeta: metav1.ObjectMeta{Name: kialiName, Namespace: controlPlaneNamespace},
				}},
			},
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("patch").On("kialis").Named(kialiName).In(controlPlaneNamespace).Passes(ExpectedKialiPatch(jaegerName, domain)),
				),
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
		{
			name: "kiali.existing.jaeger.existing",
			smcp: NewSMCPForKialiJaegerTests(smcpName, kialiExistingName, jaegerExistingName, versions.V2_0.String()),
			resources: []runtime.Object{
				&kialiv1alpha1.Kiali{Base: external.Base{
					ObjectMeta: metav1.ObjectMeta{Name: kialiExistingName, Namespace: controlPlaneNamespace},
				}},
				jaegerExisting,
				jaegerRoute,
			},
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("patch").On("kialis").Named(kialiExistingName).In(controlPlaneNamespace).Passes(ExpectedKialiPatch(jaegerExistingName, domain)),
				),
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("kialis").Named(kialiExistingName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("kialis").Named(kialiExistingName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
		},
	}

	RunSimpleInstallTest(t, testCases)
}

func TestExternalJaegerV1_1(t *testing.T) {
	const (
		operatorNamespace  = "istio-operator"
		smcpName           = "test"
		cniDaemonSetName   = "istio-node"
		domain             = "test.com"
		kialiName          = "kiali"
		kialiExistingName  = "kiali-existing"
		jaegerName         = "jaeger"
		jaegerExistingName = "jaeger-existing"
	)

	if testing.Verbose() {
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr), zap.Level(zapcore.Level(-5))))
	}

	jaegerRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jaegerExistingName,
			Namespace: controlPlaneNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/instance":  jaegerExistingName,
				"app.kubernetes.io/component": "query-route",
			},
		},
		Spec: routev1.RouteSpec{
			Host: fmt.Sprintf("%s.%s", jaegerExistingName, domain),
		},
	}
	jaegerExisting := &jaegerv1.Jaeger{
		Base: external.Base{
			ObjectMeta: metav1.ObjectMeta{Name: jaegerExistingName, Namespace: controlPlaneNamespace},
		},
	}

	testCases := []IntegrationTestCase{
		{
			name: "jaeger.v2",
			smcp: NewSMCPForKialiJaegerTests(smcpName, "", jaegerExistingName, versions.V1_1.String()),
			resources: []runtime.Object{
				jaegerExisting,
				jaegerRoute,
			},
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).Passes(ExpectedKialiCreate(jaegerExistingName, domain)),
				),
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
		},
		{
			name: "jaeger.v1",
			smcp: &maistrav1.ServiceMeshControlPlane{
				ObjectMeta: metav1.ObjectMeta{Name: smcpName, Namespace: controlPlaneNamespace},
				Spec: maistrav1.ControlPlaneSpec{
					Version:  versions.V1_1.String(),
					Template: "maistra",
					Istio: maistrav1.NewHelmValues(map[string]interface{}{
						"global": map[string]interface{}{
							"proxy": map[string]interface{}{
								"tracer": "zipkin",
							},
							"tracer": map[string]interface{}{
								"zipkin": map[string]interface{}{
									"address": jaegerExistingName + "-collector.cp-namespace.svc.cluster.local:9411",
								},
							},
						},
						"tracing": map[string]interface{}{
							"enabled": false,
						},
						"kiali": map[string]interface{}{
							"jaegerInClusterURL": jaegerExistingName + "-query.cp-namespace.svc.cluster.local",
						},
					}),
				},
			},
			resources: []runtime.Object{
				jaegerExisting,
				jaegerRoute,
			},
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).Passes(ExpectedKialiCreate(jaegerExistingName, domain)),
				),
				Assertions: ActionAssertions{
					Assert("create").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("create").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("create").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("kialis").Named(kialiName).In(controlPlaneNamespace).IsSeen(),
					Assert("delete").On("jaegers").Named(jaegerName).In(controlPlaneNamespace).IsNotSeen(),
					Assert("delete").On("jaegers").Named(jaegerExistingName).In(controlPlaneNamespace).IsNotSeen(),
				},
			},
		},
	}
	RunSimpleInstallTest(t, testCases)
}

func NewSMCPForKialiJaegerTests(smcpName, kialiName, jaegerName, version string) *maistrav2.ServiceMeshControlPlane {
	enable := true
	return NewV2SMCPResource(smcpName, controlPlaneNamespace, &maistrav2.ControlPlaneSpec{
		Version: version,
		Tracing: &maistrav2.TracingConfig{
			Type: maistrav2.TracerTypeJaeger,
		},
		Addons: &maistrav2.AddonsConfig{
			Kiali: &maistrav2.KialiAddonConfig{
				Enablement: maistrav2.Enablement{
					Enabled: &enable,
				},
				Name: kialiName,
			},
			Jaeger: &maistrav2.JaegerAddonConfig{
				Name: jaegerName,
			},
		},
	})
}

func ExpectedKialiCreate(jaegerName, domain string) VerifierTestFunc {
	return func(action clienttesting.Action) error {
		createAction := action.(clienttesting.CreateAction)
		obj := createAction.GetObject()
		kiali := obj.(*unstructured.Unstructured)
		if err := VerifyKialiUpdate(jaegerName, domain, maistrav1.NewHelmValues(kiali.Object)); err != nil {
			fmt.Println(fmt.Sprintf("kiali:\n%v", kiali))
			return err
		}
		return nil
	}
}

func ExpectedKialiPatch(jaegerName, domain string) VerifierTestFunc {
	return func(action clienttesting.Action) error {
		patchAction := action.(clienttesting.PatchAction)
		if patchAction.GetPatchType() != types.MergePatchType {
			return fmt.Errorf("unexpected patch type: %s", patchAction.GetPatchType())
		}
		patch := map[string]interface{}{}
		if err := json.Unmarshal(patchAction.GetPatch(), &patch); err != nil {
			return err
		}
		patchValues := maistrav1.NewHelmValues(patch)
		if err := VerifyKialiUpdate(jaegerName, domain, patchValues); err != nil {
			fmt.Println(fmt.Sprintf("patch:\n%s", string(patchAction.GetPatch())))
			return err
		}
		return nil
	}
}

func VerifyKialiUpdate(jaegerName, domain string, values *maistrav1.HelmValues) error {
	var allErrors []error
	expectedGrafanaURL := "https://grafana." + domain
	if url, _, _ := values.GetString("spec.external_services.grafana.url"); url != expectedGrafanaURL {
		allErrors = append(allErrors, fmt.Errorf("unexpected grafana URL, expected %s, got %s", expectedGrafanaURL, url))
	}
	if enabled, _, _ := values.GetBool("spec.external_services.grafana.enabled"); !enabled {
		allErrors = append(allErrors, fmt.Errorf("expected grafana to be enabled"))
	}
	if _, ok, _ := values.GetString("spec.external_services.grafana.auth.password"); !ok {
		allErrors = append(allErrors, fmt.Errorf("expected grafana password to be set"))
	}
	expectedJaegerURL := fmt.Sprintf("http://%s.%s", jaegerName, domain)
	if url, _, _ := values.GetString("spec.external_services.tracing.url"); url != expectedJaegerURL {
		allErrors = append(allErrors, fmt.Errorf("unexpected jaeger URL, expected %s, got %s", expectedJaegerURL, url))
	}
	if enabled, _, _ := values.GetBool("spec.external_services.tracing.enabled"); !enabled {
		allErrors = append(allErrors, fmt.Errorf("expected jaeger to be enabled"))
	}
	if _, ok, _ := values.GetString("spec.external_services.tracing.auth.password"); !ok {
		allErrors = append(allErrors, fmt.Errorf("expected jaeger password to be set"))
	}
	if _, ok, _ := values.GetString("spec.external_services.prometheus.auth.password"); !ok {
		allErrors = append(allErrors, fmt.Errorf("expected prometheus password to be set"))
	}
	if len(allErrors) > 0 {
		return errors.NewAggregate(allErrors)
	}
	return nil
}

func TestPatchAddonsResult(t *testing.T) {
	requeueWithTimeout := reconcile.Result{RequeueAfter: backoffInterval}

	testCases := []struct {
		name                         string
		kialiEnabled                 bool
		grafanaEnabled               bool
		jaegerEnabled                bool
		objects                      []runtime.Object
		expectedReconciliationResult reconcile.Result
		expecterError                error
	}{
		{
			name:           "should requeue reconciliation with timeout when jaeger and grafana are enabled, but their routes do not exist",
			kialiEnabled:   true,
			grafanaEnabled: true,
			jaegerEnabled:  true,
			objects: []runtime.Object{
				newKiali(),
				newHtpasswd(),
			},
			expectedReconciliationResult: requeueWithTimeout,
			expecterError:                nil,
		},
		{
			name:           "should requeue reconciliation with timeout when jaeger and grafana are enabled, but jaeger route does not exist",
			kialiEnabled:   true,
			grafanaEnabled: true,
			jaegerEnabled:  true,
			objects: []runtime.Object{
				newKiali(),
				newHtpasswd(),
				newGrafanaRoute("grafana.istio-system.svc.cluster.local"),
			},
			expectedReconciliationResult: requeueWithTimeout,
			expecterError:                nil,
		},
		{
			name:           "should requeue reconciliation with timeout when jaeger and grafana are enabled, but grafana route does not exist",
			kialiEnabled:   true,
			grafanaEnabled: true,
			jaegerEnabled:  true,
			objects: []runtime.Object{
				newKiali(),
				newHtpasswd(),
				newJaegerRoute("jaeger-query.istio-system.svc.cluster.local"),
			},
			expectedReconciliationResult: requeueWithTimeout,
			expecterError:                nil,
		},
		{
			name:           "reconciliation should succeed when jaeger and grafana are enabled and their routes exist",
			kialiEnabled:   true,
			grafanaEnabled: true,
			jaegerEnabled:  true,
			objects: []runtime.Object{
				newKiali(),
				newHtpasswd(),
				newGrafanaRoute("grafana.istio-system.svc.cluster.local"),
				newJaegerRoute("jaeger-query.istio-system.svc.cluster.local"),
			},
			expectedReconciliationResult: reconcile.Result{},
			expecterError:                nil,
		},
	}

	for _, tc := range testCases {
		smcpSpec := newSmcpSpec(tc.kialiEnabled, tc.grafanaEnabled, tc.jaegerEnabled)
		smcp := New21SMCPResource("basic", "istio-system", smcpSpec)
		smcp.Status = maistrav2.ControlPlaneStatus{AppliedSpec: *smcpSpec}

		s := scheme.Scheme
		configureKialiAPI(s)
		configureRouteAPI(s)

		c := fake.NewFakeClientWithScheme(s, tc.objects...)
		r := newReconciler(c, s, &record.FakeRecorder{}, "istio-operator", cni.Config{Enabled: true})
		r.instanceReconcilerFactory = NewControlPlaneInstanceReconciler

		_, smcpReconciler := r.getOrCreateReconciler(smcp)
		res, err := smcpReconciler.PatchAddons(context.TODO(), &smcp.Spec)

		if res != tc.expectedReconciliationResult {
			t.Fatalf("expected to get %s, but got: %s", toString(tc.expectedReconciliationResult), toString(res))
		}
		if err != tc.expecterError {
			t.Fatalf("expected to get [%v], but got: [%v]", tc.expecterError, err)
		}
	}
}

func TestPatchAddonsReconciliationWithExponentialBackoff(t *testing.T) {
	smcpSpec := newSmcpSpec(true, true, true)
	smcp := New21SMCPResource("basic", "istio-system", smcpSpec)
	smcp.Status = maistrav2.ControlPlaneStatus{AppliedSpec: *smcpSpec}

	s := scheme.Scheme
	configureKialiAPI(s)
	configureRouteAPI(s)

	// there is no Jaeger route, so reconciliation should be repeated
	expectedObjects := []runtime.Object{
		newKiali(),
		newHtpasswd(),
		newGrafanaRoute("grafana.istio-system.svc.cluster.local"),
	}

	var c client.Client
	var tracker *EnhancedTracker
	c, tracker = CreateClientWithScheme(s, expectedObjects...)

	r := newReconciler(c, s, &record.FakeRecorder{}, "istio-operator", cni.Config{Enabled: true})
	r.instanceReconcilerFactory = NewControlPlaneInstanceReconciler
	_, smcpReconciler := r.getOrCreateReconciler(smcp)

	maxBackoffReached := false
	maxBackoff := reconcile.Result{RequeueAfter: backoffMaxDuration}
	i := 0
	// timeout should be increased every time the PatchAddons function is called until the max timeout is reached
	for !maxBackoffReached {
		res, err := smcpReconciler.PatchAddons(context.TODO(), &smcp.Spec)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == maxBackoff {
			maxBackoffReached = true
			break
		}

		backoffMultiplier := math.Pow(2, float64(i))
		expectedResult := reconcile.Result{
			RequeueAfter: time.Duration(backoffMultiplier) * backoffInterval,
		}
		if res != expectedResult {
			t.Fatalf("expected to get result [%s], but got [%s]; iteration: %d", toString(expectedResult), toString(res), i)
		}
		i++
	}

	// timeout should not be increased after reaching the maximum duration
	res, err := smcpReconciler.PatchAddons(context.TODO(), &smcp.Spec)
	expectedResult := reconcile.Result{RequeueAfter: backoffMaxDuration}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != expectedResult {
		t.Fatalf("expected to get result [%s], but got [%s]", toString(expectedResult), toString(res))
	}

	// simulate that Jaeger route becomes available
	jaegerRoute := newJaegerRoute("jaeger-query.istio-system.svc.cluster.local")
	if err := tracker.Add(jaegerRoute); err != nil {
		t.Fatalf("Failed to add Jaeger route to the runtime object tracker")
	}

	// timeout should not be increased after reaching the maximum duration
	res, err = smcpReconciler.PatchAddons(context.TODO(), &smcp.Spec)
	expectedResult = reconcile.Result{}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != expectedResult {
		t.Fatalf("expected to get result [%s], but got [%s]", toString(expectedResult), toString(res))
	}

	// simulate that Jaeger route becomes unavailable
	if err := tracker.Delete(routeGVR, jaegerRoute.Namespace, jaegerRoute.Name); err != nil {
		t.Fatalf("Failed to delete Jaeger route from the runtime object tracker: %v", err)
	}

	// timeout should not be reset
	res, err = smcpReconciler.PatchAddons(context.TODO(), &smcp.Spec)
	expectedResult = reconcile.Result{RequeueAfter: backoffInterval}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != expectedResult {
		t.Fatalf("expected to get result [%s], but got [%s]", toString(expectedResult), toString(res))
	}
}

func newSmcpSpec(kialiEnabled, grafanaEnabled, jaegerEnabled bool) *maistrav2.ControlPlaneSpec {
	spec := &maistrav2.ControlPlaneSpec{
		Addons: &maistrav2.AddonsConfig{},
	}

	if kialiEnabled {
		spec.Addons.Kiali = &maistrav2.KialiAddonConfig{
			Enablement: featureEnabled,
		}
	}
	if grafanaEnabled {
		spec.Addons.Grafana = &maistrav2.GrafanaAddonConfig{
			Enablement: featureEnabled,
		}
	}
	if jaegerEnabled {
		spec.Tracing = &maistrav2.TracingConfig{
			Type: maistrav2.TracerTypeJaeger,
		}
	}

	return spec
}

func newHtpasswd() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "htpasswd",
			Namespace: "istio-system",
		},
		Data: map[string][]byte{
			"rawPasswd": []byte("123"),
		},
	}
}

func newKiali() *kialiv1alpha1.Kiali {
	return &kialiv1alpha1.Kiali{
		Base: external.Base{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kiali",
				Namespace: "istio-system",
			},
		},
	}
}

func newGrafanaRoute(hostname string) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grafana",
			Namespace: "istio-system",
		},
		Spec: routev1.RouteSpec{
			Host: hostname,
		},
	}
}

func newJaegerRoute(hostname string) *routev1.Route {
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jaeger",
			Namespace: "istio-system",
			Labels: map[string]string{
				"app.kubernetes.io/instance":  "jaeger",
				"app.kubernetes.io/component": "query-route",
			},
		},
		Spec: routev1.RouteSpec{
			Host: hostname,
		},
	}
}

func configureKialiAPI(s *runtime.Scheme) {
	kialiGroupVersion := schema.GroupVersion{
		Group:   "kiali.io",
		Version: "v1alpha1",
	}
	s.AddKnownTypes(kialiGroupVersion, &kialiv1alpha1.Kiali{})
}

func configureRouteAPI(s *runtime.Scheme) {
	routeGroupVersion := schema.GroupVersion{
		Group:   "route.openshift.io",
		Version: "v1",
	}
	s.AddKnownTypes(routeGroupVersion, &routev1.Route{}, &routev1.RouteList{})
}

func toString(r reconcile.Result) string {
	if !r.Requeue && r.RequeueAfter == 0 {
		return "reconcile.Result{}"
	}
	return fmt.Sprintf("reconcile.Result{Requeue: %t, RequeuAfter: %d}", r.Requeue, r.RequeueAfter)
}

var routeGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}

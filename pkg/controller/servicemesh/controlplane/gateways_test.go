package controlplane

import (
	"fmt"
	"testing"

	v1 "github.com/maistra/istio-operator/pkg/apis/maistra/v1"
	v2 "github.com/maistra/istio-operator/pkg/apis/maistra/v2"
	"github.com/maistra/istio-operator/pkg/controller/common"
	. "github.com/maistra/istio-operator/pkg/controller/common/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestAdditionalIngressGatewayInstall(t *testing.T) {
	enabled := true
	additionalGatewayName := "additional-gateway"
	appNamespace := "app-namespace"
	testCases := []IntegrationTestCase{
		{
			name: "no-namespace",
			smcp: New20SMCPResource(controlPlaneName, controlPlaneNamespace, &v2.ControlPlaneSpec{
				Gateways: &v2.GatewaysConfig{
					ClusterIngress: &v2.ClusterIngressGatewayConfig{
						IngressGatewayConfig: v2.IngressGatewayConfig{
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
							},
						},
					},
					IngressGateways: map[string]*v2.IngressGatewayConfig{
						additionalGatewayName: {
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
							},
						},
					},
				},
			}),
			create: IntegrationTestValidation{
				Verifier: VerifyActions(
					Verify("create").On("deployments").Named("istio-ingressgateway").In(controlPlaneNamespace).Passes(ExpectedLabelGatewayCreate("maistra.io/gateway", "istio-ingressgateway."+controlPlaneNamespace)),
					Verify("create").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(ExpectedLabelGatewayCreate("maistra.io/gateway", additionalGatewayName+"."+controlPlaneNamespace)),
				),
				Assertions: ActionAssertions{},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
		{
			name: "cp-namespace",
			smcp: New20SMCPResource(controlPlaneName, controlPlaneNamespace, &v2.ControlPlaneSpec{
				Gateways: &v2.GatewaysConfig{
					IngressGateways: map[string]*v2.IngressGatewayConfig{
						additionalGatewayName: {
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
								Namespace: controlPlaneNamespace,
							},
						},
					},
				},
			}),
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("create").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(ExpectedLabelGatewayCreate("maistra.io/gateway", additionalGatewayName+"."+controlPlaneNamespace)),
				),
				Assertions: ActionAssertions{},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
		{
			name: "app-namespace",
			resources: []runtime.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appNamespace}},
				&v1.ServiceMeshMemberRoll{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: controlPlaneNamespace},
					Status: v1.ServiceMeshMemberRollStatus{
						ConfiguredMembers: []string{
							appNamespace,
						},
					},
				},
			},
			smcp: New20SMCPResource(controlPlaneName, controlPlaneNamespace, &v2.ControlPlaneSpec{
				Gateways: &v2.GatewaysConfig{
					IngressGateways: map[string]*v2.IngressGatewayConfig{
						additionalGatewayName: {
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
								Namespace: appNamespace,
							},
						},
					},
				},
			}),
			create: IntegrationTestValidation{
				Verifier: ActionVerifier(
					Verify("create").On("deployments").Named(additionalGatewayName).In(appNamespace).Passes(ExpectedExternalGatewayCreate),
				),
				Assertions: ActionAssertions{},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					// TODO: MAISTRA-1333 gateways in other namepsaces do not get deleted properly
					//Assert("delete").On("deployments").Named(additionalGatewayName).In(appNamespace).IsSeen(),
				},
			},
		},
		{
			name: "labels",
			smcp: New20SMCPResource(controlPlaneName, controlPlaneNamespace, &v2.ControlPlaneSpec{
				Gateways: &v2.GatewaysConfig{
					IngressGateways: map[string]*v2.IngressGatewayConfig{
						additionalGatewayName: {
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
								Service: v2.GatewayServiceConfig{
									Metadata: &v2.MetadataConfig{
										Labels: map[string]string{
											"test": "test",
										},
									},
								},
								Namespace: controlPlaneNamespace,
							},
						},
					},
				},
			}),
			create: IntegrationTestValidation{
				Verifier: VerifyActions(
					Verify("create").On("networkpolicies").Named("istio-ingressgateway").In(controlPlaneNamespace).Passes(
						ExpectedLabelMatchedByNetworkPolicy("istio", "ingressgateway"),
					),
					Verify("create").On("networkpolicies").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(
						ExpectedLabelMatchedByNetworkPolicy("test", "test"),
					),
					Verify("create").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(
						ExpectedLabelGatewayCreate("test", "test"),
					),
				),
				Assertions: ActionAssertions{},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
		{
			name: "labels-2.1",
			smcp: New21SMCPResource(controlPlaneName, controlPlaneNamespace, &v2.ControlPlaneSpec{
				Gateways: &v2.GatewaysConfig{
					IngressGateways: map[string]*v2.IngressGatewayConfig{
						additionalGatewayName: {
							GatewayConfig: v2.GatewayConfig{
								Enablement: v2.Enablement{
									Enabled: &enabled,
								},
								Service: v2.GatewayServiceConfig{
									Metadata: &v2.MetadataConfig{
										Labels: map[string]string{
											"test": "test",
										},
									},
								},
								Namespace: controlPlaneNamespace,
							},
						},
					},
				},
			}),
			create: IntegrationTestValidation{
				Verifier: VerifyActions(
					Verify("create").On("networkpolicies").Named("istio-ingressgateway").In(controlPlaneNamespace).Passes(
						ExpectedLabelMatchedByNetworkPolicy("istio", "ingressgateway"),
					),
					Verify("create").On("networkpolicies").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(
						ExpectedLabelMatchedByNetworkPolicy("test", "test"),
					),
					Verify("create").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).Passes(
						ExpectedLabelGatewayCreate("test", "test"),
						ExpectedLabelGatewayCreate("maistra.io/gateway", additionalGatewayName+"."+controlPlaneNamespace),
						ExpectedLabelGatewayCreate("app", additionalGatewayName),
					),
				),
				Assertions: ActionAssertions{},
			},
			delete: IntegrationTestValidation{
				Assertions: ActionAssertions{
					Assert("delete").On("deployments").Named(additionalGatewayName).In(controlPlaneNamespace).IsSeen(),
				},
			},
		},
	}
	RunSimpleInstallTest(t, testCases)
}

func ExpectedLabelGatewayCreate(labelName string, expectedValue string) func(action clienttesting.Action) error {
	return func(action clienttesting.Action) error {
		createAction := action.(clienttesting.CreateAction)
		obj := createAction.GetObject()
		gateway := obj.(*unstructured.Unstructured)
		if val, ok := common.GetLabel(gateway, labelName); ok {
			if val != expectedValue {
				return fmt.Errorf("expected %s label to be %s, got %s", labelName, expectedValue, val)
			}
		} else {
			return fmt.Errorf("gateway should have %s label defined", labelName)
		}
		return nil
	}
}

func ExpectedExternalGatewayCreate(action clienttesting.Action) error {
	createAction := action.(clienttesting.CreateAction)
	obj := createAction.GetObject()
	gateway := obj.(*unstructured.Unstructured)
	if len(gateway.GetOwnerReferences()) > 0 {
		return fmt.Errorf("external gateway should not have an owner reference")
	}
	return nil
}

func ExpectedLabelMatchedByNetworkPolicy(labelName string, expectedValue string) func(action clienttesting.Action) error {
	return func(action clienttesting.Action) error {
		createAction := action.(clienttesting.CreateAction)
		obj := createAction.GetObject()
		networkPolicy := obj.(*unstructured.Unstructured)
		if val, found, err := unstructured.NestedString(networkPolicy.Object, "spec", "podSelector", "matchLabels", labelName); err == nil {
			if !found || val != expectedValue {
				return fmt.Errorf("expected %s label to be matched against value %s, but didn't", labelName, expectedValue)
			}
		} else if err != nil {
			return err
		}

		return nil
	}
}

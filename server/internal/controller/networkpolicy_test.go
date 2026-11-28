package controller

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

const testGatewayNamespace = "kubetty-gateway-dev"

func ingressPortNumbers(rule networkingv1.NetworkPolicyIngressRule) []int32 {
	var ports []int32
	for _, p := range rule.Ports {
		ports = append(ports, p.Port.IntVal)
	}
	return ports
}

func TestBuildNetworkPolicy_AdmitsOnlyTheGatewayNamespace(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gui       bool
		wantPorts []int32
	}{
		{name: "terminal only", gui: false, wantPorts: []int32{8080}},
		{name: "with VNC", gui: true, wantPorts: []int32{8080, 5901}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testProject("np")
			p.GUIEnabled = tc.gui
			cfg := testConfig()

			np := BuildNetworkPolicy(p, cfg, testGatewayNamespace)

			if !reflect.DeepEqual(np.Spec.PodSelector.MatchLabels, projectSelectorLabels(p, cfg)) {
				t.Errorf("podSelector = %v, want the project's selector labels", np.Spec.PodSelector.MatchLabels)
			}
			if !reflect.DeepEqual(np.Spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}) {
				t.Errorf("policyTypes = %v, want [Ingress]", np.Spec.PolicyTypes)
			}
			if len(np.Spec.Ingress) != 1 {
				t.Fatalf("ingress has %d rules, want exactly 1 (gateway only): %+v", len(np.Spec.Ingress), np.Spec.Ingress)
			}
			rule := np.Spec.Ingress[0]
			wantPeer := networkingv1.NetworkPolicyPeer{NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/metadata.name": testGatewayNamespace},
			}}
			if !reflect.DeepEqual(rule.From, []networkingv1.NetworkPolicyPeer{wantPeer}) {
				t.Errorf("from = %+v, want only the gateway namespace", rule.From)
			}
			if got := ingressPortNumbers(rule); !reflect.DeepEqual(got, tc.wantPorts) {
				t.Errorf("ports = %v, want %v", got, tc.wantPorts)
			}
		})
	}
}

func legacySameNamespaceRule() networkingv1.NetworkPolicyIngressRule {
	tcp := corev1.ProtocolTCP
	return networkingv1.NetworkPolicyIngressRule{
		From:  []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{}}},
		Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 8080}}},
	}
}

func TestUpdateNetworkPolicyIfNeeded_RemovesTheSameNamespaceRule(t *testing.T) {
	p := testProject("np-upgrade")
	cfg := testConfig()
	cfg.GatewayNamespace = testGatewayNamespace

	existing := BuildNetworkPolicy(p, cfg, testGatewayNamespace)
	existing.Spec.Ingress = append(existing.Spec.Ingress, legacySameNamespaceRule())
	client := fake.NewSimpleClientset(existing)
	ctrl := NewWithClient(Config{ResourceConfig: cfg}, newMockStore(), client)

	if err := ctrl.updateNetworkPolicyIfNeeded(context.Background(), p, cfg); err != nil {
		t.Fatalf("updateNetworkPolicyIfNeeded: %v", err)
	}

	got, err := client.NetworkingV1().NetworkPolicies(cfg.Namespace).Get(context.Background(), existing.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get NetworkPolicy: %v", err)
	}
	if want := BuildNetworkPolicy(p, cfg, testGatewayNamespace).Spec; !reflect.DeepEqual(got.Spec, want) {
		t.Errorf("NetworkPolicy spec after update = %+v, want %+v", got.Spec, want)
	}
}

func TestUpdateNetworkPolicyIfNeeded_LeavesAMatchingPolicyAlone(t *testing.T) {
	p := testProject("np-current")
	cfg := testConfig()
	cfg.GatewayNamespace = testGatewayNamespace

	client := fake.NewSimpleClientset(BuildNetworkPolicy(p, cfg, testGatewayNamespace))
	ctrl := NewWithClient(Config{ResourceConfig: cfg}, newMockStore(), client)

	if err := ctrl.updateNetworkPolicyIfNeeded(context.Background(), p, cfg); err != nil {
		t.Fatalf("updateNetworkPolicyIfNeeded: %v", err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() != "get" {
			t.Errorf("unexpected %s on %s for an up-to-date policy", a.GetVerb(), a.GetResource().Resource)
		}
	}
}

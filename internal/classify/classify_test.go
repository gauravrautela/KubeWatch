package classify

import (
	"reflect"
	"testing"
)

func TestActorType(t *testing.T) {
	cases := []struct {
		user string
		want string
	}{
		{"gaurav.rautela", ActorHuman},
		{"alice@example.com", ActorHuman},
		{"system:serviceaccount:ci:deployer", ActorServiceAccount},
		{"system:kube-controller-manager", ActorSystem},
		{"system:node:node-1", ActorSystem},
		{"", ActorUnknown},
	}
	for _, c := range cases {
		if got := ActorType(c.user); got != c.want {
			t.Errorf("ActorType(%q) = %q, want %q", c.user, got, c.want)
		}
	}
}

func TestClassifyKindAndPaths(t *testing.T) {
	cases := []struct {
		name        string
		kind        string
		subResource string
		operation   string
		diffJSON    string
		want        []string
	}{
		{"create role", "Role", "", "CREATE", `[{"path":"rules","op":"add","new":[]}]`, []string{ClassRBAC}},
		{"delete deployment", "Deployment", "", "DELETE", `[]`, []string{ClassOther}},
		{"create configmap", "ConfigMap", "", "CREATE", `[{"path":"data","op":"add"}]`, []string{ClassConfigData}},
		{"create networkpolicy", "NetworkPolicy", "", "CREATE", `[]`, []string{ClassNetwork}},
		{"status only", "Deployment", "", "UPDATE",
			`[{"path":"status.replicas","op":"replace","old":2,"new":3},{"path":"metadata.resourceVersion","op":"replace","old":"1","new":"2"}]`,
			[]string{ClassStatusOnly}},
		{"managed fields only", "Deployment", "", "UPDATE",
			`[{"path":"metadata.managedFields","op":"replace"}]`,
			[]string{ClassStatusOnly}},
		{"labels only", "Deployment", "", "UPDATE",
			`[{"path":"metadata.labels.team","op":"add","new":"payments"}]`,
			[]string{ClassMetadataOnly}},
		{"empty diff", "Deployment", "", "UPDATE", `[]`, []string{ClassStatusOnly}},
		{"unparseable diff", "Deployment", "", "UPDATE", `not-json`, []string{ClassOther}},
		{"configmap data", "ConfigMap", "", "UPDATE",
			`[{"path":"data.timeout","op":"replace","old":"5s","new":"1s"}]`,
			[]string{ClassConfigData}},
		{"service spec", "Service", "", "UPDATE",
			`[{"path":"spec.ports","op":"replace"}]`,
			[]string{ClassNetwork}},
		{"replicas", "Deployment", "", "UPDATE",
			`[{"path":"spec.replicas","op":"replace","old":2,"new":5}]`,
			[]string{ClassScale}},
		{"scale subresource", "Deployment", "scale", "UPDATE", `[]`, []string{ClassScale}},
		{"rolebinding subjects", "RoleBinding", "", "UPDATE",
			`[{"path":"subjects","op":"replace"}]`,
			[]string{ClassRBAC}},
		{"ingress status only stays noise", "Ingress", "", "UPDATE",
			`[{"path":"status.loadBalancer","op":"replace"}]`,
			[]string{ClassStatusOnly}},
		{"unmatched spec path", "Deployment", "", "UPDATE",
			`[{"path":"spec.strategy.type","op":"replace","old":"Recreate","new":"RollingUpdate"}]`,
			[]string{ClassOther}},
		{"noise plus signal keeps signal only", "Deployment", "", "UPDATE",
			`[{"path":"status.replicas","op":"replace"},{"path":"spec.replicas","op":"replace","old":2,"new":5}]`,
			[]string{ClassScale}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.kind, c.subResource, c.operation, c.diffJSON)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Classify() = %v, want %v", got, c.want)
			}
		})
	}
}

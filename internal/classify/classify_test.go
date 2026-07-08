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

func TestClassifyContainerChanges(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		diffJSON string
		want     []string
	}{
		{"image bump", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"repo/app:v1"}],"new":[{"name":"app","image":"repo/app:v2"}]}]`,
			[]string{ClassImage}},
		{"env change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","env":[{"name":"A","value":"1"}]}],"new":[{"name":"app","image":"i","env":[{"name":"A","value":"2"}]}]}]`,
			[]string{ClassEnv}},
		{"envFrom change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"}],"new":[{"name":"app","image":"i","envFrom":[{"configMapRef":{"name":"cm"}}]}]}]`,
			[]string{ClassEnv}},
		{"resources change", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","resources":{"limits":{"cpu":"1"}}}],"new":[{"name":"app","image":"i","resources":{"limits":{"cpu":"2"}}}]}]`,
			[]string{ClassResources}},
		{"image and env together", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"v1","env":[{"name":"A","value":"1"}]}],"new":[{"name":"app","image":"v2","env":[{"name":"A","value":"2"}]}]}]`,
			[]string{ClassEnv, ClassImage}},
		{"container added", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"}],"new":[{"name":"app","image":"i"},{"name":"sidecar","image":"s"}]}]`,
			[]string{ClassImage}},
		{"container removed", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i"},{"name":"sidecar","image":"s"}],"new":[{"name":"app","image":"i"}]}]`,
			[]string{ClassImage}},
		{"command change unattributed", "Deployment",
			`[{"path":"spec.template.spec.containers","op":"replace","old":[{"name":"app","image":"i","command":["a"]}],"new":[{"name":"app","image":"i","command":["b"]}]}]`,
			[]string{ClassOther}},
		{"cronjob container path", "CronJob",
			`[{"path":"spec.jobTemplate.spec.template.spec.containers","op":"replace","old":[{"name":"job","image":"v1"}],"new":[{"name":"job","image":"v2"}]}]`,
			[]string{ClassImage}},
		{"bare pod container path", "Pod",
			`[{"path":"spec.containers","op":"replace","old":[{"name":"app","image":"v1"}],"new":[{"name":"app","image":"v2"}]}]`,
			[]string{ClassImage}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.kind, "", "UPDATE", c.diffJSON)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Classify() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestClassifyNoiseKinds(t *testing.T) {
	cases := []struct {
		kind      string
		operation string
		diffJSON  string
	}{
		{"Lease", "UPDATE", `[{"path":"spec.renewTime","op":"replace","old":"a","new":"b"}]`},
		{"Lease", "DELETE", `[]`},
		{"Event", "CREATE", `[]`},
		{"Endpoints", "UPDATE", `[{"path":"subsets","op":"replace"}]`},
		{"EndpointSlice", "UPDATE", `[{"path":"endpoints","op":"replace"}]`},
		{"CiliumEndpointSlice", "UPDATE", `[]`},
		{"TokenReview", "CREATE", `[]`},
		{"SubjectAccessReview", "CREATE", `[]`},
		{"LocalSubjectAccessReview", "CREATE", `[]`},
		{"SelfSubjectAccessReview", "CREATE", `[]`},
		{"SelfSubjectRulesReview", "CREATE", `[]`},
	}
	for _, c := range cases {
		got := Classify(c.kind, "", c.operation, c.diffJSON)
		if want := []string{ClassNoiseKind}; !reflect.DeepEqual(got, want) {
			t.Errorf("Classify(%s %s) = %v, want %v", c.operation, c.kind, got, want)
		}
	}
}

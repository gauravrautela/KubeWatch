package classify

import "testing"

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

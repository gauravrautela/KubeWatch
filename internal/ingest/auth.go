// Package ingest is the hub-side API that receives, buffers, and stores events.
package ingest

// Authenticator maps a bearer token to the cluster identity it represents.
type Authenticator interface {
	ClusterFor(token string) (cluster string, ok bool)
}

// StaticAuth is a fixed token->cluster map loaded from configuration.
type StaticAuth map[string]string

// ClusterFor implements Authenticator.
func (a StaticAuth) ClusterFor(token string) (string, bool) {
	c, ok := a[token]
	return c, ok
}

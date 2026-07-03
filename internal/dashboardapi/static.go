package dashboardapi

import (
	"net/http"
	"os"
	"path/filepath"
)

// spaFileServer serves static files from dir, falling back to index.html for
// paths that don't map to a real file (SPA client-side routing).
func spaFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		p := filepath.Join(dir, clean)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

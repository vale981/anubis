// Package xess vendors a copy of Xess and makes it available at /.xess/xess.css
//
// This is intended to be used as a vendored package in other projects.
package xess

import (
	"embed"
	"net/http"
	"path/filepath"

	"github.com/vale981/anubis"
	"github.com/vale981/anubis/internal"
)

//go:generate go tool github.com/a-h/templ/cmd/templ generate

var (
	//go:embed *.css static
	Static embed.FS

	URL = "/.within.website/x/xess/xess.css"
)

func init() {
	Mount(http.DefaultServeMux)

	//goland:noinspection GoBoolExpressions
	if anubis.Version != "devel" {
		URL = filepath.Join(filepath.Dir(URL), "xess.min.css")
	}

	URL = URL + "?cachebuster=" + anubis.Version
}

func Mount(mux *http.ServeMux) {
	mux.Handle("/.within.website/x/xess/", internal.UnchangingCache(http.StripPrefix("/.within.website/x/xess/", http.FileServerFS(Static))))
}

// This file will read in the srcs, and output a file per input source
// none of the files that we have import other things, so we don't need bundling
package main

import (
	"flag"
	"os"
	"path"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {

	// outDir will be something like the below. It is populated
	// by Bazels RULEDIR
	// bazel-out/darwin_arm64-fastbuild/bin/web
	outDir := flag.String("test", "", "directory to emit files to")
	flag.Parse()

	// build JS
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{"web/src/entry.js"},
		Outfile: path.Join(*outDir,
			"htdocs", "assets", "js", "bundle_new.js"),
		Bundle:            true,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Write:             true,
		LogLevel:          api.LogLevelInfo,
	})

	if len(result.Errors) > 0 {
		os.Exit(1)
	}

	// minify CSS
	result = api.Build(api.BuildOptions{
		EntryPoints:       []string{"web/htdocs/assets/css/codesearch.css"},
		Bundle:            true,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Outfile: path.Join(*outDir,
			"htdocs", "assets", "css", "codesearch.min.css"),
		LogLevel: api.LogLevelInfo,
		Write:    true,
	})

	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}

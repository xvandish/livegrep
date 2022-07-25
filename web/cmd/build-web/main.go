// This file will read in the srcs, and output a file per input source
// none of the files that we have import other things, so we don't need bundling
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	// "github.com/tdewolff/minify/v2/json"
	// "github.com/tdewolff/minify/v2/svg"
	// "github.com/tdewolff/minify/v2/xml"
)

func FilePathWalkDir(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// file - web/src/codesearch/codesearch.js
// file - web/src/codesearch/codesearch_ui.js
// file - web/src/entry.js
// file - web/src/fileview/fileview.js

func main() {

	// outDir will be something like the below. It is populated
	// by Bazels RULEDIR
	// bazel-out/darwin_arm64-fastbuild/bin/web
	outDir := flag.String("test", "", "directory to emit files to")
	flag.Parse()

	fmt.Println("outDir:", *outDir)
	fmt.Println("outDir len:", len(*outDir))

	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)

	// dir, err := os.Getwd()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Printf("dir: %s\n", dir)

	// files, err := FilePathWalkDir(*outDir)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// for _, file := range files {
	// 	fmt.Printf("file - %s\n", file)
	// }

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

	// for _, file := range result.OutputFiles {
	// 	// fmt.Print("%+v", file)
	// 	os.Stdout.Write(file.Contents)
	// }

}

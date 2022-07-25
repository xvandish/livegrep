// This file will read in the srcs, and output a file per input source
// none of the files that we have import other things, so we don't need bundling
package main

import (
	"os"
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
	// fmt.Printf("this is being called!\n")

	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("text/html", html.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)

	// dir, err := os.Getwd()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Printf("dir: %s\n", dir)

	// files, err := FilePathWalkDir("./")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// for _, file := range files {
	// 	fmt.Printf("file - %s\n", file)
	// }

	result := api.Build(api.BuildOptions{
		EntryPoints:       []string{"web/src/entry.js"},
		Outfile:           "bundle_new.js",
		Bundle:            true,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Write:             false,
		LogLevel:          api.LogLevelInfo,
	})

	if len(result.Errors) > 0 {
		os.Exit(1)
	}

	for _, file := range result.OutputFiles {
		// fmt.Print("%+v", file)
		os.Stdout.Write(file.Contents)
	}

}

package main

import (
	"encoding/json"
	_ "expvar"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"strings"

	"github.com/livegrep/livegrep/server"
	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/middleware"
)

var (
	serveAddr   = flag.String("listen", "127.0.0.1:8910", "The address to listen on")
	backendAddr = flag.String("connect", "localhost:9999", "The address to connect to")
	docRoot     = flag.String("docroot", "", "The livegrep document root (web/ directory). If not provided, this defaults to web/ inside the bazel-created runfiles directory adjacent to the livegrep binary.")
	indexConfig = flag.String("index-config", "", "Codesearch index config file; provide to enable repo browsing")
	reload      = flag.Bool("reload", false, "Reload template files on every request")
	_           = flag.Bool("logtostderr", false, "[DEPRECATED] compatibility with glog")
	minifyHTML  = flag.Bool("minify-html", false, "Minify rendered HTML templates on the fly")
)

func runfilesPath(sourcePath string) (string, error) {
	programPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return path.Join(programPath+".runfiles", "com_github_livegrep_livegrep", sourcePath), nil
}

func main() {
	flag.Parse()

	if *docRoot == "" {
		var err error
		*docRoot, err = runfilesPath("web")
		if err != nil {
			log.Fatalf(err.Error())
		}
	}

	cfg := &config.Config{
		DocRoot: *docRoot,
		Listen:  *serveAddr,
		Reload:  *reload,
		Backends: []config.Backend{
			{Id: "", Addr: *backendAddr},
		},
		GoogleIAPConfig: config.GoogleIAPConfig{
			ProjectNumber:    os.Getenv("GOOGLE_IAP_PROJECT_NUMBER"),
			BackendServiceID: os.Getenv("GOOGLE_IAP_BACKEND_SERVICE_ID"),
			ProjectID:        os.Getenv("GOOGLE_IAP_PROJECT_ID"),
		},
		StatsD: config.StatsD{
			Address:    os.Getenv("STATSD_ADDRESS"),
			Prefix:     os.Getenv("STATSD_PREFIX"),
			Tags:       strings.Split(os.Getenv("STATSD_TAGS"), ","),
			TagsFormat: os.Getenv("STATSD_TAGS_FORMAT"),
		},
	}

	if *indexConfig != "" {
		data, err := ioutil.ReadFile(*indexConfig)
		if err != nil {
			log.Fatalf(err.Error())
		}

		if err = json.Unmarshal(data, &cfg.IndexConfig); err != nil {
			log.Fatalf("reading %s: %s", flag.Arg(0), err.Error())
		}
	}

	if len(flag.Args()) != 0 {
		data, err := ioutil.ReadFile(flag.Arg(0))
		if err != nil {
			log.Fatalf(err.Error())
		}

		if err = json.Unmarshal(data, &cfg); err != nil {
			log.Fatalf("reading %s: %s", flag.Arg(0), err.Error())
		}
	}

	cfg.MinifyHTML = *minifyHTML || cfg.MinifyHTML

	handler, err := server.New(cfg)
	if err != nil {
		panic(err.Error())
	}

	if cfg.ReverseProxy {
		handler = middleware.UnwrapProxyHeaders(handler)
	}

	if middleware.ShouldEnableGoogleIAP(cfg.GoogleIAPConfig) {
		handler = middleware.WrapWithIAP(handler, cfg.GoogleIAPConfig)
		log.Printf("Enabled GoogleIAPAuthMiddleware")
	}

	http.DefaultServeMux.Handle("/", handler)

	log.Printf("Listening on %s.", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, nil))
}

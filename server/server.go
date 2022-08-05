package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"

	"golang.org/x/net/context"

	"github.com/bmizerany/pat"
	"gopkg.in/alexcesaro/statsd.v2"

	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/log"
	"github.com/livegrep/livegrep/server/reqid"
	"github.com/livegrep/livegrep/server/templates"
	pb "github.com/livegrep/livegrep/src/proto/go_proto"
)

var serveUrlParseError = fmt.Errorf("failed to parse repo and path from URL")

type page struct {
	Title         string
	ScriptName    string
	ScriptData    interface{}
	IncludeHeader bool
	Data          interface{}
	Config        *config.Config
	AssetHashes   map[string]string
	Nonce         template.HTMLAttr // either `` or ` nonce="..."`
}

type server struct {
	config      *config.Config
	bk          map[string]*Backend
	bkOrder     []string
	inner       http.Handler
	Templates   map[string]*template.Template
	OpenSearch  *texttemplate.Template
	AssetHashes map[string]string
	Layout      *template.Template

	statsd *statsd.Client

	sync.Mutex
	repos              map[string]*pb.Tree
	serveFilePathRegex *regexp.Regexp
}

func (s *server) loadTemplates() {
	s.Templates = make(map[string]*template.Template)
	err := templates.LoadTemplates(s.config.DocRoot, s.Templates)
	if err != nil {
		panic(fmt.Sprintf("loading templates: %v", err))
	}

	p := s.config.DocRoot + "/templates/opensearch.xml"
	s.OpenSearch = texttemplate.Must(texttemplate.ParseFiles(p))

	s.AssetHashes = make(map[string]string)
	err = templates.LoadAssetHashes(
		path.Join(s.config.DocRoot, "hashes.txt"),
		s.AssetHashes)
	if err != nil {
		panic(fmt.Sprintf("loading templates: %v", err))
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.inner.ServeHTTP(w, r)
}

func (s *server) ServeRoot(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/search", 303)
}

func (s *server) ServeSearch(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	urls := make(map[string]map[string]string, len(s.bk))
	backends := make([]*Backend, 0, len(s.bk))
	sampleRepo := ""
	// Used to display a connection status. Default selected is first backend.
	firstBkShortStatus := ""
	firstBkStatus := ""
	// for the purposes of the frontend, we only care about precense/whether a repo is in this. We
	// don't actually use its value
	internalRepos := make(map[string]int, 100)
	for idx, bkId := range s.bkOrder {
		bk := s.bk[bkId]
		backends = append(backends, bk)
		bk.I.Lock()
		m := make(map[string]string, len(bk.I.Trees))
		urls[bk.Id] = m
		for _, r := range bk.I.Trees {
			if sampleRepo == "" {
				sampleRepo = r.Name
			}
			m[r.Name] = r.Metadata.UrlPattern
			// TODO: only do this if some metadata indicated that we're ok with it
			internalRepos[r.Name] = 1
		}
		if idx == 0 {
			firstBkShortStatus, firstBkStatus = bk.getTextStatus()
		}
		bk.I.Unlock()
	}

	s.renderPage(ctx, w, r, "index.html", &page{
		Title:         "code search",
		ScriptName:    "codesearch",
		IncludeHeader: true,
		Data: struct {
			Backends                []*Backend
			SampleRepo              string
			FirstBackendShortStatus string
			FirstBackendStatus      string
		}{
			Backends:                backends,
			SampleRepo:              sampleRepo,
			FirstBackendShortStatus: firstBkShortStatus,
			FirstBackendStatus:      firstBkStatus,
		},
	})
}

func (s *server) ServeFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	repoName, path, err := getRepoPathFromURL(s.serveFilePathRegex, r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	commit := r.URL.Query().Get("commit")
	if commit == "" {
		commit = "HEAD"
	}

	if len(s.repos) == 0 {
		http.Error(w, "File browsing not enabled", 404)
		return
	}

	repo, ok := s.repos[repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	data, err := buildFileData(path, repo, commit)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading file: %s", err), 500)
		return
	}

	script_data := &struct {
		RepoInfo *pb.Tree `json:"repo_info"`
		FilePath string   `json:"file_path"`
		Commit   string   `json:"commit"`
	}{repo, path, commit}

	s.renderPage(ctx, w, r, "fileview.html", &page{
		Title:         data.PathSegments[len(data.PathSegments)-1].Name,
		ScriptName:    "fileview",
		ScriptData:    script_data,
		IncludeHeader: false,
		Data:          data,
	})
}

func (s *server) ServeAbout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	s.renderPage(ctx, w, r, "about.html", &page{
		Title:         "about",
		IncludeHeader: true,
	})
}

func (s *server) ServeHelp(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Help is now shown in the main search page when no search has been entered.
	http.Redirect(w, r, "/search", 303)
}

// GKE load balancers refuse to serve a page if the backing service livenessProbe fails
// So, if using ServeHealthcheck there's a chance the deployment will serve a 404
// even if the frontend/server is healthy, but the codesearch instance isn't healthy
// Use the following function if you just need a 200 when the server is running
func (s *server) ServeHealthZ(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *server) ServeHealthcheck(w http.ResponseWriter, r *http.Request) {
	// All backends must have (at some point) reported an index age for us to
	// report as healthy.
	// TODO: report as unhealthy if a backend goes down after we've spoken to
	// it.
	for _, bk := range s.bk {
		if bk.I.IndexTime.IsZero() {
			http.Error(w, fmt.Sprintf("unhealthy backend '%s' '%s'\n", bk.Id, bk.Addr), 500)
			return
		}
	}
	io.WriteString(w, "ok\n")
}

type stats struct {
	IndexAge int64 `json:"index_age"`
}

func (s *server) ServeStats(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// For index age, report the age of the stalest backend's index.
	now := time.Now()
	maxBkAge := time.Duration(-1) * time.Second
	for _, bk := range s.bk {
		if bk.I.IndexTime.IsZero() {
			// backend didn't report index time
			continue
		}
		bkAge := now.Sub(bk.I.IndexTime)
		if bkAge > maxBkAge {
			maxBkAge = bkAge
		}
	}
	replyJSON(ctx, w, 200, &stats{
		IndexAge: int64(maxBkAge / time.Second),
	})
}

func (s *server) ServeBackendStatus(w http.ResponseWriter, r *http.Request) {
	backendName := r.URL.Query().Get(":backend")
	var bk *Backend
	if backendName != "" {
		bk = s.bk[backendName]
		if bk == nil {
			writeError(nil, w, 400, "bad_backend",
				fmt.Sprintf("Unknown backend: %s", backendName))
			return
		}
	} else {
		for _, bk = range s.bk {
			break
		}
	}

	statusCode, age := bk.getStatus()
	io.WriteString(w, fmt.Sprintf("%d,%s", statusCode, age))
}

func (s *server) requestProtocol(r *http.Request) string {
	if s.config.ReverseProxy {
		if proto := r.Header.Get("X-Real-Proto"); len(proto) > 0 {
			return proto
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (s *server) ServeOpensearch(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	data := &struct {
		BackendName, BaseURL string
	}{
		BaseURL: s.requestProtocol(r) + "://" + r.Host + "/",
	}

	for _, bk := range s.bk {
		if bk.I.Name != "" {
			data.BackendName = bk.I.Name
			break
		}
	}

	templateName := "opensearch.xml"
	w.Header().Set("Content-Type", "application/xml")
	err := s.OpenSearch.ExecuteTemplate(w, templateName, data)
	if err != nil {
		log.Printf(ctx, "Error rendering %s: %s", templateName, err)
		return
	}
}

func (s *server) renderPage(ctx context.Context, w io.Writer, r *http.Request, templateName string, pageData *page) {
	if s.statsd != nil {
		normTemplName := "pages." + strings.Replace(templateName, ".html", "", 1)
		defer s.statsd.NewTiming().Send(normTemplName + ".response_time")
		s.statsd.Increment(normTemplName + ".hits")
	}

	t, ok := s.Templates[templateName]
	if !ok {
		log.Printf(ctx, "Error: no template named %v", templateName)
		return
	}

	pageData.Config = s.config
	pageData.AssetHashes = s.AssetHashes

	nonce := "" // custom nonce computation can go here

	if nonce != "" {
		pageData.Nonce = template.HTMLAttr(fmt.Sprintf(` nonce="%s"`, nonce))
	}

	err := t.ExecuteTemplate(w, templateName, pageData)
	if err != nil {
		log.Printf(ctx, "Error rendering %v: %s", templateName, err)
		return
	}
}

type reloadHandler struct {
	srv   *server
	inner http.Handler
}

func (h *reloadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.srv.loadTemplates()
	h.inner.ServeHTTP(w, r)
}

type handler func(c context.Context, w http.ResponseWriter, r *http.Request)

const RequestTimeout = 30 * time.Second

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	ctx = reqid.NewContext(ctx, reqid.New())
	log.Printf(ctx, "http request: remote=%q method=%q url=%q",
		r.RemoteAddr, r.Method, r.URL)
	h(ctx, w, r)
}

func (s *server) Handler(f func(c context.Context, w http.ResponseWriter, r *http.Request)) http.Handler {
	return handler(f)
}

// Takes a search query, performs the search, then renders the results into HTML and serves it
func (s *server) ServeRenderedSearchResults(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	data, statusCode, errorMsg, errorMsgLong := s.ServerSideAPISearchV2(ctx, w, r)

	if (statusCode) > 200 {
		w.WriteHeader(statusCode)
		w.Write([]byte(errorMsgLong))
		log.Printf(ctx, "error status=%d code=%s message=%s", statusCode, errorMsg, errorMsgLong)
		return
	}

	s.renderPage(ctx, w, r, "searchresults_partial.html", &page{
		IncludeHeader: false,
		Data:          data,
	})
}

func New(cfg *config.Config) (http.Handler, error) {
	srv := &server{
		config: cfg,
		bk:     make(map[string]*Backend),
		repos:  make(map[string]*pb.Tree),
	}
	srv.loadTemplates()

	if cfg.StatsD.Address != "" {
		ctx := context.Background()
		log.Printf(ctx, "Initializing StatsD client")
		args := []statsd.Option{statsd.Address(cfg.StatsD.Address)}

		if cfg.StatsD.Prefix != "" {
			log.Printf(ctx, "appending prefix: %s", cfg.StatsD.Prefix)
			args = append(args, statsd.Prefix(cfg.StatsD.Prefix))
		}

		if cfg.StatsD.Tags[0] != "" {
			var tagsFormat statsd.TagFormat
			givenFmt := cfg.StatsD.TagsFormat
			if givenFmt == "datadog" {
				tagsFormat = statsd.Datadog
			} else if givenFmt == "influxdb" {
				tagsFormat = statsd.InfluxDB
			} else {
				panic(fmt.Sprint("Invalid TagsFormat: %s. Only 'datadog' and 'influxdb' allowed", givenFmt))
			}

			log.Printf(ctx, "appending tags: %v\n", cfg.StatsD.Tags)
			log.Printf(ctx, "appending tagsFormat: %v\n", tagsFormat)
			args = append(args, statsd.Tags(cfg.StatsD.Tags...), statsd.TagsFormat(tagsFormat))
		}

		statsdClient, err := statsd.New(args...)

		if err != nil {
			panic(fmt.Sprintf("could not initialize StatsD client: %v", err))
		}
		srv.statsd = statsdClient
		log.Printf(ctx, "Finished initializing StatsD client")
	}

	for _, bk := range srv.config.Backends {
		be, e := NewBackend(bk, srv)
		if e != nil {
			return nil, e
		}
		be.Start()
		srv.bk[be.Id] = be
		srv.bkOrder = append(srv.bkOrder, be.Id)
	}

	// var repoNames []string
	// for _, r := range srv.config.IndexConfig.Repositories {
	// 	srv.repos[r.Name] = r
	// 	repoNames = append(repoNames, r.Name)
	// }

	// serveFilePathRegex, err := buildRepoRegex(repoNames)
	// if err != nil {
	// 	return nil, err
	// }
	// srv.serveFilePathRegex = serveFilePathRegex

	m := pat.New()
	m.Add("GET", "/healthz", http.HandlerFunc(srv.ServeHealthZ))
	m.Add("GET", "/debug/healthcheck", http.HandlerFunc(srv.ServeHealthcheck))
	m.Add("GET", "/debug/stats", srv.Handler(srv.ServeStats))
	m.Add("GET", "/search/:backend", srv.Handler(srv.ServeSearch))
	m.Add("GET", "/search/", srv.Handler(srv.ServeSearch))
	m.Add("GET", "/view/", srv.Handler(srv.ServeFile))
	m.Add("GET", "/about", srv.Handler(srv.ServeAbout))
	m.Add("GET", "/help", srv.Handler(srv.ServeHelp))
	m.Add("GET", "/opensearch.xml", srv.Handler(srv.ServeOpensearch))
	m.Add("GET", "/", srv.Handler(srv.ServeRoot))

	m.Add("GET", "/api/v1/search/:backend", srv.Handler(srv.ServeAPISearch))
	m.Add("GET", "/api/v1/search/", srv.Handler(srv.ServeAPISearch))
	m.Add("GET", "/api/v1/bkstatus/:backend", http.HandlerFunc(srv.ServeBackendStatus))
	m.Add("GET", "/api/v1/bkstatus/", http.HandlerFunc(srv.ServeBackendStatus))

	m.Add("GET", "/api/v2/getRenderedSearchResults/:backend", srv.Handler(srv.ServeRenderedSearchResults))
	m.Add("GET", "/api/v2/getRenderedSearchResults/", srv.Handler(srv.ServeRenderedSearchResults))

	var h http.Handler = m

	if cfg.Reload {
		h = &reloadHandler{srv, h}
	}

	mux := http.NewServeMux()
	mux.Handle("/assets/", http.FileServer(http.Dir(path.Join(cfg.DocRoot, "htdocs"))))
	mux.Handle("/", h)

	srv.inner = mux

	return srv, nil
}

func (s *server) resetInteralRepos(repos map[string]*pb.Tree) {
	ctx := context.Background()
	s.Lock()
	s.repos = repos
	s.Unlock()
	log.Printf(ctx, "Backend change detected. Internal repos reloaded")
}

// when a backend reloads, this is called so that the new trees
// bkN allows for can be viewed through the internal viewer.
// This, of course, assumes that the repos are meant to be viewed
// through the internal view. In our case, this is true.
// Also, if we ever want multiple backends, we'll need to concat
// all backends available repoNames, not just pass in a single
// backends.
func (s *server) rebuildRepoRegex(repoNames []string) {
	ctx := context.Background()
	newFilePathRegex, err := buildRepoRegex(repoNames)
	if err != nil {
		log.Printf(ctx, "err trying to rebuild repo regex")
		return
	}

	s.Lock()
	s.serveFilePathRegex = newFilePathRegex
	s.Unlock()
	log.Printf(ctx, "Backend change detected. serveFilePathRegex rebuilt")
}

func buildRepoRegex(repoNames []string) (*regexp.Regexp, error) {
	// Sort in descending order of length so most specific match is selected by regex engine
	sort.Slice(repoNames, func(i, j int) bool {
		return len(repoNames[i]) >= len(repoNames[j])
	})

	// Build regex of form "(repo1|repo2)/(path)"
	var buf bytes.Buffer
	for i, repoName := range repoNames {
		buf.WriteString(regexp.QuoteMeta(repoName))
		if i < len(repoNames)-1 {
			buf.WriteString("|")
		}
	}
	repoRegexAlt := buf.String()
	repoFileRegex, err := regexp.Compile(fmt.Sprintf("(%s)/(.*)", repoRegexAlt))
	if err != nil {
		return nil, fmt.Errorf("failed to create regular expression for URL parsing")
	}

	return repoFileRegex, nil
}

func getRepoPathFromURL(repoRegex *regexp.Regexp, url string) (repo string, path string, err error) {
	matches := repoRegex.FindStringSubmatch(pat.Tail("/view/", url))
	if len(matches) == 0 {
		return "", "", serveUrlParseError
	}

	return matches[1], matches[2], nil
}

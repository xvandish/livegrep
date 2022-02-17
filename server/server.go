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
	texttemplate "text/template"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/api/idtoken"

	"github.com/bmizerany/pat"
	"github.com/go-redis/redis/v8"
	libhoney "github.com/honeycombio/libhoney-go"

	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/log"
	"github.com/livegrep/livegrep/server/reqid"
	"github.com/livegrep/livegrep/server/templates"
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
	repos       map[string]config.RepoConfig
	inner       http.Handler
	Templates   map[string]*template.Template
	OpenSearch  *texttemplate.Template
	AssetHashes map[string]string
	Layout      *template.Template

	honey *libhoney.Builder
	redis *redis.Client

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

// a ws api will
func (s *server) ServeInitSearchInfo(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	urls := make(map[string]map[string]string, len(s.bk))
	backends := make([]*Backend, 0, len(s.bk))
	sampleRepo := ""
	for _, bkId := range s.bkOrder {
		bk := s.bk[bkId]
		backends = append(backends, bk)
		bk.I.Lock()
		m := make(map[string]string, len(bk.I.Trees))
		urls[bk.Id] = m
		for _, r := range bk.I.Trees {
			if sampleRepo == "" {
				sampleRepo = r.Name
			}
			m[r.Name] = r.Url
		}
		bk.I.Unlock()
	}

	script_data := &struct {
		RepoUrls           map[string]map[string]string `json:"repo_urls"`
		InternalViewRepos  map[string]config.RepoConfig `json:"internal_view_repos"`
		DefaultSearchRepos []string                     `json:"default_search_repos"`
		LinkConfigs        []config.LinkConfig          `json:"link_configs"`
		Backends           []*Backend                   `json:"backends"`
		SampleRepo         string                       `json:"sample_repo"`
	}{urls, s.repos, s.config.DefaultSearchRepos, s.config.LinkConfigs, backends, sampleRepo}

	replyJSON(ctx, w, 200, &script_data, "", s)
}

func (s *server) ServeSearch(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	urls := make(map[string]map[string]string, len(s.bk))
	backends := make([]*Backend, 0, len(s.bk))
	sampleRepo := ""
	for _, bkId := range s.bkOrder {
		bk := s.bk[bkId]
		backends = append(backends, bk)
		bk.I.Lock()
		m := make(map[string]string, len(bk.I.Trees))
		urls[bk.Id] = m
		for _, r := range bk.I.Trees {
			if sampleRepo == "" {
				sampleRepo = r.Name
			}
			m[r.Name] = r.Url
		}
		bk.I.Unlock()
	}

	script_data := &struct {
		RepoUrls           map[string]map[string]string `json:"repo_urls"`
		InternalViewRepos  map[string]config.RepoConfig `json:"internal_view_repos"`
		DefaultSearchRepos []string                     `json:"default_search_repos"`
		LinkConfigs        []config.LinkConfig          `json:"link_configs"`
	}{urls, s.repos, s.config.DefaultSearchRepos, s.config.LinkConfigs}

	s.renderPage(ctx, w, r, "index.html", &page{
		Title:         "code search",
		ScriptName:    "codesearch",
		ScriptData:    script_data,
		IncludeHeader: true,
		Data: struct {
			Backends   []*Backend
			SampleRepo string
		}{
			Backends:   backends,
			SampleRepo: sampleRepo,
		},
	})
}

func (s *server) ServeFileInfo(ctx context.Context, w http.ResponseWriter, r *http.Request) {

	if len(s.repos) == 0 {
		http.Error(w, "File browsing not enabled", 404)
		return
	}

	params := r.URL.Query()
	log.Printf(ctx, "params: %v", params)

	repoName := params.Get("repo")
	path := params.Get("path")

	// if the path is empty, we're rendering a tree view
	if repoName == "" {
		http.Error(w, "repo search param must not be empty", 500)
		return
	}

	commit := params.Get("commit")
	if commit == "" {
		commit = "HEAD"
	}

	repo, ok := s.repos[repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	data, err := buildFileData(path, repo, commit)
	if err != nil {
		http.Error(w, "Error reading file", 500)
		return
	}

	script_data := &struct {
		RepoInfo config.RepoConfig `json:"repo_info"`
		FilePath string            `json:"file_path"`
		Commit   string            `json:"commit"`
		Data     interface{}       `json:"data"`
	}{repo, path, commit, data}

	replyJSON(ctx, w, 200, &script_data, "", s)
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
		http.Error(w, "Error reading file", 500)
		return
	}

	script_data := &struct {
		RepoInfo config.RepoConfig `json:"repo_info"`
		FilePath string            `json:"file_path"`
		Commit   string            `json:"commit"`
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

// GKE load balancers refuse to serve a page if the backing service livenessProbe fails
// So, if using ServeHealthcheck there's a chance the deployment will serve a 404
// event if the frontend/server is healthy, but the codesearch instance isn't healthy
// So use the following function if you just need a 200 when the server is running
func (s *server) ServeHealthZ(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func iapAuth(next http.Handler, cfg config.GoogleIAPConfig) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		// GKE and GCE health checks don't use JWT headers, so skip validation
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		defer timeTrack(ctx, time.Now(), "iapAuth wrapper + children (if any)")
		iapJWT := r.Header.Get("x-goog-iap-jwt-assertion")

		_, err := idtoken.Validate(ctx, iapJWT, cfg.Aud)

		if err != nil {
			log.Printf(ctx, "UNAUTHORIZED http request: remote=%q method=%q url=%q err:%v",
				r.RemoteAddr, r.Method, r.URL, err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
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
	}, "", s)
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
	defer timeTrack(ctx, time.Now(), "ServeHTTP")
	log.Printf(ctx, "http request: remote=%q method=%q url=%q",
		r.RemoteAddr, r.Method, r.URL)
	h(ctx, w, r)
}

func (s *server) Handler(f func(c context.Context, w http.ResponseWriter, r *http.Request)) http.Handler {
	return handler(f)
}

func shouldEnableGoogleIAP(cfg config.GoogleIAPConfig) bool {
	ctx := context.Background()
	if cfg.ProjectNumber == "" {
		return false
	}

	if cfg.BackendServiceID == "" && cfg.ProjectID == "" {
		log.Printf(ctx, "GoogleIAPConfig: ProjectNumber provided but no BackendServiceID or ProjectID found. Not enabling.")
		return false
	}

	if cfg.BackendServiceID != "" && cfg.ProjectID != "" {
		log.Printf(ctx, "GoogleIAPConfig: BackendServiceID and ProjectID are mutually exclusive. Not enabling.")
		return false
	}

	return true
}

func initializeGoogleIAPAud(cfg config.GoogleIAPConfig) {
	if cfg.BackendServiceID != "" { // GKE or GCE
		cfg.Aud = fmt.Sprintf("/projects/%s/global/backendServices/%s", cfg.ProjectNumber, cfg.BackendServiceID)
	} else { // GAE
		cfg.Aud = fmt.Sprintf("/projects/%s/apps/%s", cfg.ProjectNumber, cfg.ProjectID)
	}
}

func New(cfg *config.Config) (http.Handler, error) {
	srv := &server{
		config: cfg,
		bk:     make(map[string]*Backend),
		repos:  make(map[string]config.RepoConfig),
	}
	srv.loadTemplates()
	ctx := context.Background()

	if cfg.Honeycomb.WriteKey != "" {
		log.Printf(ctx,
			"Enabling honeycomb dataset=%s", cfg.Honeycomb.Dataset)
		srv.honey = libhoney.NewBuilder()
		srv.honey.WriteKey = cfg.Honeycomb.WriteKey
		srv.honey.Dataset = cfg.Honeycomb.Dataset
	}

	if cfg.RedisCacheConfig.Addr != "" {
		addr := cfg.RedisCacheConfig.Addr
		log.Printf(ctx, "Connecting to Redis cache at addr: %s", addr)

		if cfg.RedisCacheConfig.KeyTTL != "" {
			p, err := time.ParseDuration(cfg.RedisCacheConfig.KeyTTL)
			if err != nil {
				log.Printf(ctx, "error parsing redis ttl: %v\n", err)
				cfg.RedisCacheConfig.KeyTTLD = 0
			} else {
				cfg.RedisCacheConfig.KeyTTLD = p
			}
		}

		srv.redis = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: cfg.RedisCacheConfig.Password,
		})
		pong, err := srv.redis.Ping(ctx).Result()
		log.Printf(ctx, "%s, %v", pong, err)
	}

	if shouldEnableGoogleIAP(cfg.GoogleIAPConfig) {
		initializeGoogleIAPAud(cfg.GoogleIAPConfig)
	}

	for _, bk := range srv.config.Backends {
		be, e := NewBackend(bk.Id, bk.Addr)
		if e != nil {
			return nil, e
		}
		be.Start()
		srv.bk[be.Id] = be
		srv.bkOrder = append(srv.bkOrder, be.Id)
	}

	var repoNames []string
	for _, r := range srv.config.IndexConfig.Repositories {
		srv.repos[r.Name] = r
		repoNames = append(repoNames, r.Name)
	}

	serveFilePathRegex, err := buildRepoRegex(repoNames)
	if err != nil {
		return nil, err
	}
	srv.serveFilePathRegex = serveFilePathRegex

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

	m.Add("GET", "/api/v2/search/getInitInfo", srv.Handler(srv.ServeInitSearchInfo))
	m.Add("GET", "/api/v2/getFileInfo", srv.Handler(srv.ServeFileInfo))
	m.Add("GET", "/api/v1/search/:backend", srv.Handler(srv.ServeAPISearch))
	m.Add("GET", "/api/v1/search/", srv.Handler(srv.ServeAPISearch))

	var h http.Handler = m

	if cfg.Reload {
		h = &reloadHandler{srv, h}
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(path.Join(cfg.DocRoot, "htdocs")))

	// Only wrap with middleware if auth is going to be used, avoids unecessary checks on each request
	if shouldEnableGoogleIAP(cfg.GoogleIAPConfig) {
		log.Printf(context.Background(), "Enabling IAPAuth Middleware")
		mux.Handle("/assets/", iapAuth(fileServer, cfg.GoogleIAPConfig))
		mux.Handle("/", iapAuth(h, cfg.GoogleIAPConfig))
	} else {
		mux.Handle("/assets/", fileServer)
		mux.Handle("/", h)
	}

	srv.inner = mux

	return srv, nil
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

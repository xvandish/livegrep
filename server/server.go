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
	texttemplate "text/template"
	"time"

	"golang.org/x/net/context"

	"github.com/bmizerany/pat"
	"gopkg.in/alexcesaro/statsd.v2"

	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/log"
	"github.com/livegrep/livegrep/server/reqid"
	"github.com/livegrep/livegrep/server/templates"
)

var serveUrlParseError = fmt.Errorf("failed to parse repo and path from URL")
var newYorkTime *time.Location

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
	newRepos    map[string]map[string]config.RepoConfig
	inner       http.Handler
	Templates   map[string]*template.Template
	OpenSearch  *texttemplate.Template
	AssetHashes map[string]string
	Layout      *template.Template

	statsd *statsd.Client

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
	backends := make([]*Backend, 0, len(s.bk))
	sampleRepo := ""
	// Used to display a connection status. Default selected is first backend.
	for _, bkId := range s.bkOrder {
		bk := s.bk[bkId]
		backends = append(backends, bk)
		bk.I.Lock()
		for _, r := range bk.I.Trees {
			if sampleRepo == "" {
				sampleRepo = r.Name
				break
			}
		}
		bk.I.Unlock()
	}

	s.renderPage(ctx, w, r, "index.html", &page{
		Title:         "code search",
		ScriptName:    "codesearch",
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

func (s *server) ServeGitShow(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// repoName, filePath, err := getRepoPathFromURL(s.serveFilePathRegex, r.URL.Path, "/git-show/")
	parent := r.URL.Query().Get(":parent")
	repo := r.URL.Query().Get(":repo")
	commit := r.URL.Query().Get(":commitHash")
	repoName := parent + "/" + repo
	// rev := r.URL.Query().Get(":rev")

	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repoConfig, ok := s.repos[repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	// given /git-show/repo/some/path/here/commit/{commitHash}
	// TODO: We should think about restructuring the URL patterns so that
	// our modifiers come after.
	// E.g
	// /repo/some/path/commit/x
	// /repo/some/path/log

	if commit == "" {
		http.Error(w, "commit is empty", 500)
		return
	}

	data, err := gitShowCommit(repoConfig, commit)

	if err != nil {
		http.Error(w, fmt.Sprintf("error doing git-show: %v\n", err), 500)
		return
	}

	s.renderPage(ctx, w, r, "gitshowcommit.html", &page{
		Title:         "Git Show",
		IncludeHeader: false,
		Data:          data,
	})
}

func (s *server) ServeSimpleGitLog(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")
	path := pat.Tail("/delve/:parent/:repo/commits/:rev/", r.URL.Path)
	firstParent := r.URL.Query().Get("firstParent")

	if firstParent == "" {
		firstParent = "HEAD"
	}

	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repo, ok := s.repos[parent+"/"+repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	data, err := buildSimpleGitLogData(path, firstParent, repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error building log data: %v\n", err), 500)
		return
	}
	data.CommitLinkPrefix = "/delve/" + parent + "/" + repoName + "/"

	if !data.MaybeLastPage {
		w.Header().Set("X-next-parent", data.NextParent)
		w.Header().Set("X-maybe-last", fmt.Sprintf("%v", data.MaybeLastPage))
	}

	// we render a partial page rather than the whole thing
	// eventually we'll probably want this as a seperate route
	if r.URL.Query().Get("partial") == "true" {
		templateName := "simplegitlogpaginated.html"
		t, ok := s.Templates[templateName]
		if !ok {
			log.Printf(ctx, "Error: no template named %v", templateName)
			return
		}

		err := t.ExecuteTemplate(w, templateName, struct {
			Data interface{}
		}{
			Data: data,
		})

		if err != nil {
			log.Printf(ctx, "Error rendering %v: %s", templateName, err)
			return
		}
		return
	}

	s.renderPage(ctx, w, r, "simplegitlog.html", &page{
		Title:         "simplegitlog",
		ScriptName:    "gitlog",
		IncludeHeader: false,
		Data:          data,
	})
}

func (s *server) ServeGitBlob(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// start := time.Now()
	parent := r.URL.Query().Get(":parent")
	repo := r.URL.Query().Get(":repo")
	rev := r.URL.Query().Get(":rev")
	// m.Add("GET", "/:parent/:repo/blob/:rev/", srv.Handler(srv.ServeGitBlob))
	fmt.Printf("r.URL.Path=%s\n", r.URL.Path)

	// TODO(xvandish): this is temporary. Soon we can split out blob and directory code all the way
	// down, which will lend more utility then right now. Right now the differentiation between blob/tree
	// is superficial only. The buildFileData func works for both blobs and trees, meaning that this
	// "ServeGitBlob" function works for both entitites
	path := ""
	if strings.HasPrefix(r.URL.Path, "/delve/"+parent+"/"+repo+"/tree/") {
		path = pat.Tail("/delve/:parent/:repo/tree/:rev/", r.URL.Path)
	} else {
		path = pat.Tail("/delve/:parent/:repo/blob/:rev/", r.URL.Path)
	}

	parentMap, ok := s.newRepos[parent]

	if !ok {
		io.WriteString(w, fmt.Sprintf("parent: %s not found\n", parent))
		return
	}

	repoConfig, ok := parentMap[repo]

	if !ok {
		io.WriteString(w, fmt.Sprintf("repo: %s not found\n", repo))
		return
	}

	data, err := buildFileData(path, repoConfig, rev)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading file - %s", err), 500)
		return
	}
	// We build this carefully increase /blob/ is in the path to the file that
	// we're actually viewing
	data.LogLink = fmt.Sprintf("/delve/%s/%s/commits/%s/%s", parent, repo, rev, path)
	// if we were going to permalink, make it what we want
	// TODO(xvandish): do this in fileview.go
	if data.Permalink != "" {
		data.Permalink = fmt.Sprintf("/delve/%s/%s/blob/%s/%s", parent, repo, data.CommitHash, path)
	} else if data.Headlink != "" {
		data.Headlink = fmt.Sprintf("/delve/%s/%s/blob/%s/%s", parent, repo, "HEAD", path)
	}

	script_data := &struct {
		RepoInfo   config.RepoConfig `json:"repo_info"`
		FilePath   string            `json:"file_path"`
		Commit     string            `json:"commit"`
		CommitHash string            `json:"commit_hash"`
	}{repoConfig, path, rev, data.CommitHash}

	s.renderPage(ctx, w, r, "fileview.html", &page{
		Title:         data.PathSegments[len(data.PathSegments)-1].Name,
		ScriptName:    "fileview",
		ScriptData:    script_data,
		IncludeHeader: false,
		Data:          data,
	})

}

func (s *server) ServeFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	repoName, path, err := getRepoPathFromURL(s.serveFilePathRegex, r.URL.Path, "/view/")
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

	isBackup := 0
	if !bk.Up.IsUp {
		if bk.BackupBackend != nil && bk.BackupBackend.Up.IsUp {
			bk = bk.BackupBackend
			isBackup = 1
		}
	}

	statusCode, age := bk.getStatus()
	io.WriteString(w, fmt.Sprintf("%d,%s,%d", statusCode, age, isBackup))
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
		config:   cfg,
		bk:       make(map[string]*Backend),
		repos:    make(map[string]config.RepoConfig),
		newRepos: make(map[string]map[string]config.RepoConfig),
	}
	srv.loadTemplates()
	ctx := context.Background()

	log.Printf(ctx, "loading New_York/America time")
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Printf(ctx, "error loading America/New_York time: %v\n. Falling back to local/system", err)
		newYork = time.Local
	}
	newYorkTime = newYork

	if cfg.StatsD.Address != "" {
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
		be, e := NewBackend(bk)
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
	buildReposAndParents(srv, repoNames)
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
	m.Add("GET", "/delve/:parent/:repo/tree/:rev/", srv.Handler(srv.ServeGitBlob))
	m.Add("GET", "/delve/:parent/:repo/blob/:rev/", srv.Handler(srv.ServeGitBlob))
	m.Add("GET", "/delve/:parent/:repo/commit/:commitHash/", srv.Handler(srv.ServeGitShow))
	m.Add("GET", "/delve/:parent/:repo/commits/:rev/", srv.Handler(srv.ServeSimpleGitLog))
	m.Add("GET", "/delve/", srv.Handler(srv.ServeFile))
	m.Add("GET", "/simple-git-log/", srv.Handler(srv.ServeSimpleGitLog))
	m.Add("GET", "/git-show/", srv.Handler(srv.ServeGitShow))
	m.Add("GET", "/about", srv.Handler(srv.ServeAbout))
	m.Add("GET", "/help", srv.Handler(srv.ServeHelp))
	m.Add("GET", "/opensearch.xml", srv.Handler(srv.ServeOpensearch))
	m.Add("GET", "/", srv.Handler(srv.ServeRoot))

	// no matter what, the structure of urls for repos is
	// /project|user|org/repo
	//
	// For directories
	// /org/repo/tree/{commitHash}|{branchName}/path...
	//
	// For blobs
	// /org/repo/blob/{commitHash}|{branchName}/path...
	//
	// GitLab uses a seperator /-/tree/main between the reponame and the tree/blob

	// we can loop through repos to get the allowed prefixes, and the allowed suffixes
	// we can make it a map
	// {
	//    "parent": ["repo1", "repo2", "repo3" ]
	//  }

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

func buildReposAndParents(srv *server, repoNames []string) {
	parents := make(map[string]map[string]config.RepoConfig)
	for _, parentAndRepo := range repoNames {
		firstSlash := strings.Index(parentAndRepo, "/")
		parent := parentAndRepo[:firstSlash]
		if len(parents[parent]) == 0 {
			parents[parent] = make(map[string]config.RepoConfig)
		}
		onlyRepoName := parentAndRepo[firstSlash+1:]

		// fmt.Printf("srv.repos[%s]: %+v\n", parentAndRepo, srv.repos[parentAndRepo])
		parents[parent][onlyRepoName] = srv.repos[parentAndRepo]
	}

	srv.newRepos = parents
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

func getRepoPathFromURL(repoRegex *regexp.Regexp, url, pathPrefix string) (repo string, path string, err error) {
	matches := repoRegex.FindStringSubmatch(pat.Tail(pathPrefix, url))
	if len(matches) == 0 {
		return "", "", serveUrlParseError
	}

	return matches[1], matches[2], nil
}

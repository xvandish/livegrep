package server

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"golang.org/x/net/context"

	"github.com/bmizerany/pat"
	"gopkg.in/alexcesaro/statsd.v2"

	"github.com/livegrep/livegrep/server/config"
	"github.com/livegrep/livegrep/server/fileviewer"
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
	BodyId        string
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

	data, err := fileviewer.GitShowCommit(repoConfig, commit)

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

func (s *server) ServeGitBlameJson(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")
	rev := r.URL.Query().Get(":rev")
	path := pat.Tail("/api/v2/json/git-blame/:parent/:repo/:rev/", r.URL.Path)

	fullRepoPath := parent + "/" + repoName

	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repoConfig, ok := s.repos[fullRepoPath]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	blameData, err := fileviewer.GitBlameBlob(path, repoConfig, rev)

	if err != nil {
		w.WriteHeader(500)
		return
		// w.Write((err.Error()))
	}

	replyJSON(ctx, w, 200, blameData)
}

func (s *server) ServeSimpleGitLogJson(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")
	rev := r.URL.Query().Get(":rev")
	path := pat.Tail("/api/v2/json/git-log/:parent/:repo/:rev/", r.URL.Path)

	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repo, ok := s.repos[parent+"/"+repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	data, err := fileviewer.BuildSimpleGitLogData(path, rev, repo)

	if err != nil {
		http.Error(w, err.Error(), 500)
	}
	// fmt.Printf("git_log_data=%+v\n", data)
	data.CommitLinkPrefix = "/delve/" + parent + "/" + repoName

	replyJSON(ctx, w, 200, data)
	// now we need to marshal data to json
}

// returns whether the filebrowser is enabled for the particular repo
// mentioned, and if so the repoConfig
// repo is a string like `xvandish/livegrep`
func (s *server) filebrowseEnabled(repo string) (*config.RepoConfig, error) {
	if len(s.repos) == 0 {
		return nil, errors.New("File browsing and git commands not enabled")
	}

	repoConfig, ok := s.repos[repo]

	if !ok {
		return nil, errors.New("repo: %s not found. Maybe reload the server to read the latest config.\n")
	}

	return &repoConfig, nil
}

// what should the url be? I'd like to do something lik
// api/v2/json/git-log/:parent/:repo/?rev=x&path=x&after=x&blah
func (s *server) ServeGitLogJson(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	queryVals := r.URL.Query()

	parent := queryVals.Get(":parent")
	repo := queryVals.Get(":repo")

	repoConfig, err := s.filebrowseEnabled(parent + "/" + repo)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// see fileviewer.CommitOptions documentation for details
	// on each option
	revspec := queryVals.Get("revspec")
	path := queryVals.Get("path")

	// check the numerical values
	first := queryVals.Get("first")

	var firstVal uint64
	if queryVals.Has("first") {
		firstVal, err = strconv.ParseUint(first, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not parse first: %s\n", err.Error()), 500)
		}
	}

	var afterCursorVal uint64
	if queryVals.Has("afterCursor") {
		afterCursorVal, err = strconv.ParseUint(queryVals.Get("afterCursor"), 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not parse afterCursor: %s\n", err.Error()), 500)
		}

	}

	opts := fileviewer.CommitOptions{
		Range: revspec,
		Path:  path,
		N:     uint(firstVal),
		SkipN: uint(afterCursorVal),
	}

	commitLog, err := fileviewer.BuildGitLog(opts, *repoConfig)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	replyJSON(ctx, w, 200, commitLog)
}

func (s *server) ServeGitLsTreeJson(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")
	rev := r.URL.Query().Get(":rev")
	path := pat.Tail("/api/v2/json/git-ls-tree/:parent/:repo/:rev/", r.URL.Path)

	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repo, ok := s.repos[parent+"/"+repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	data, err := fileviewer.BuildDirectoryTree(path, repo, rev)
	if err != nil {
		writeError(ctx, w, 500, "", err.Error())
		return
	}

	replyJSON(ctx, w, 200, data)
}

func (s *server) ServeGitLsTreeRendered(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")
	rev := r.URL.Query().Get(":rev")
	path := pat.Tail("/api/v2/getRenderedFileTree/:parent/:repo/:rev/", r.URL.Path)

	fmt.Printf("parent:%s repoName:%s rev:%s path:%s\n", parent, repoName, rev, path)
	if len(s.repos) == 0 {
		http.Error(w, "File browsing and git commands not enabled", 404)
		return
	}

	repo, ok := s.repos[parent+"/"+repoName]
	if !ok {
		http.Error(w, "No such repo", 404)
		return
	}

	fmt.Printf("repo.Path=%s repo.Name=%s\n", repo.Path, repo.Name)
	data, err := fileviewer.BuildDirectoryTree(path, repo, rev)

	if err != nil {
		writeError(ctx, w, 500, "", err.Error())
		return
	}

	rendered := templates.RenderDirectoryTree(data, -15, repo.Name, rev, path)
	w.Write([]byte(rendered))
}

func (s *server) ServeSimpleGitLog(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repoName := r.URL.Query().Get(":repo")

	path := pat.Tail("/delve/:parent/:repo/commits/:rev/", r.URL.Path)
	firstParent := r.URL.Query().Get("firstParent")

	if firstParent == "" {
		firstParent = r.URL.Query().Get(":rev")
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

	data, err := fileviewer.BuildSimpleGitLogData(path, firstParent, repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error building log data: %v\n", err), 500)
		return
	}
	data.CommitLinkPrefix = "/delve/" + parent + "/" + repoName

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

func (s *server) ServeGitBlobRaw(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repo := r.URL.Query().Get(":repo")

	repoRevAndPath := pat.Tail("/raw-blob/:parent/:repo/+/", r.URL.Path)
	log.Printf(ctx, "repoRevAndPath: %s\n", repoRevAndPath)
	sp := strings.Split(repoRevAndPath, ":")

	log.Printf(ctx, "sp=%v\n", sp)
	var rev, path string
	if len(sp) == 2 {
		rev = sp[0]
		path = sp[1]
	} else {
		// we're in a broken case.
		log.Printf(ctx, "ERROR: repoRevAndPath: %s -- split len != 2\n", repoRevAndPath)
		if len(sp) == 1 && sp[0] != "" {
			log.Printf(ctx, "sp[1\n")
			rev = sp[0]
		} else {
			rev = "HEAD"
		}
		path = ""
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

	data, err := fileviewer.BuildFileData(path, repoConfig, rev)
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

	// log.Printf(ctx, "going to print raw thing")
	// log.Printf(ctx, "data is %+v", data)
	s.renderPage(ctx, w, r, "raw_blob_or_tree.html", &page{
		Title:         data.PathSegments[len(data.PathSegments)-1].Name,
		IncludeHeader: false,
		Data:          data,
	})
}

func logAndServeError(ctx context.Context, w http.ResponseWriter, errMsg string, errCode int) {
	log.Printf(ctx, errMsg)
	http.Error(w, errMsg, errCode)
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
		errMsg := fmt.Sprintf("delve-error: parent: %s not found\n", parent)
		logAndServeError(ctx, w, errMsg, 500)
		return
	}

	repoConfig, ok := parentMap[repo]

	if !ok {
		errMsg := fmt.Sprintf("delve-error: repo: %s not found\n", repo)
		logAndServeError(ctx, w, errMsg, 500)
		return
	}

	data, err := fileviewer.BuildFileData(path, repoConfig, rev)
	if err != nil {
		errMsg := fmt.Sprintf("delve-error: Error reading file path=%s, rev=%s - %s", path, rev, err)
		logAndServeError(ctx, w, errMsg, 500)
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
		errMsg := fmt.Sprintf("Error: No such repo: %s", repoName)
		logAndServeError(ctx, w, errMsg, 500)
		return
	}

	data, err := fileviewer.BuildFileData(path, repo, commit)
	if err != nil {
		errMsg := fmt.Sprintf("Error: reading file - %s", err)
		logAndServeError(ctx, w, errMsg, 500)
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

func (s *server) ServeAboutFileviewer(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	s.renderPage(ctx, w, r, "fileviewer_about.html", &page{
		Title:         "fileviewer about",
		IncludeHeader: true,
	})
}

func (s *server) ServeDiff(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repo := r.URL.Query().Get(":repo")
	revA := r.URL.Query().Get(":revA")
	revB := r.URL.Query().Get(":revB")

	path := pat.Tail("/diff/:parent/:repo/:revA/:revB/", r.URL.Path)

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

	diff, err := fileviewer.GetDiffBetweenTwoCommits(path, repoConfig, revA, revB, false)

	// most likely, a request for
	if diff == nil {
		if revA == revB {
			io.WriteString(w, "Commits for comparison are identical.")
		} else {
			w.WriteHeader(500)
		}
		return
	}

	rows := diff.GetDiffRowsSplit()
	if err != nil {
		log.Printf(ctx, "splitdiff err=%v\n", err)
		io.WriteString(w, err.Error())
		return
	}

	s.renderPage(ctx, w, r, "splitdiff.html", &page{
		Title:         "Diff",
		IncludeHeader: false,
		Data: struct {
			DiffRows []fileviewer.IDiffRow
			FileName string
		}{
			DiffRows: rows,
			FileName: filepath.Base(path),
		},
	})

	// io.WriteString(w, fmt.Sprintf("<html><body><div style=\"display:flex; gap:10px\">%s%s</div></body></html>", left, right))
}

// the fileviewer requests the repos it can use from the server, rather than the
// cs backend because the fileviewer is still not set up to update its list of repos
// when the index changes, so if we try to open a repo that the cs backend thinks is
// valid, but the server does not we'll error out.
// So instead, we stick with the safe method of asking the server whats up
// TODO: Finish that PR up that dynamically updates the webserver with the cs repos

// TODO: allow this page to be rendered when no branch is passed. In that case, we should
// figure out HEAD, then pass that to buildFileData
// TODO: When dfc is set, allow log to return whether there are "future" entries, so that users
// can jump back the most recent
// TODO: handle empty repos better
// TODO: we really need to know the HEAD rev!
func (s *server) ServeExperimental(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get(":parent")
	repo := r.URL.Query().Get(":repo")

	repoRevAndPath := pat.Tail("/experimental/:parent/:repo/+/", r.URL.Path)
	log.Printf(ctx, "repoRevAndPath: %s\n", repoRevAndPath)
	sp := strings.Split(repoRevAndPath, ":")

	log.Printf(ctx, "sp=%v\n", sp)
	var repoRev, path string
	if len(sp) == 2 {
		repoRev = sp[0]
		path = sp[1]
	} else {
		// we're in a broken case.
		log.Printf(ctx, "ERROR: repoRevAndPath: %s -- split len != 2\n", repoRevAndPath)
		if len(sp) == 1 && sp[0] != "" {
			log.Printf(ctx, "sp[1\n")
			repoRev = sp[0]
		} else {
			repoRev = "HEAD"
		}
		path = ""
	}

	q := r.URL.Query()
	dataFileCommit := q.Get("dfc")

	log.Printf(ctx, "repoRev=%s path=%s\n", repoRev, path)

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

	// TODO: cache this, or precompute it
	// important to know what branch/tag/commit
	headRev, err := fileviewer.GitRevParseAbbrev("HEAD", repoConfig.Path)
	if err != nil {
		io.WriteString(w, fmt.Sprintf("failed to fetch HEAD ref\n", repo))
		return
	}

	// if the repoRev == "HEAD", resolve it
	if repoRev == "HEAD" {
		repoRev = headRev
	}

	var commitToLoadFileAt string
	if dataFileCommit != "" {
		// if dfc is declared, view that file at that commit, not
		// the repo commit
		commitToLoadFileAt = dataFileCommit
	} else {
		// otherwise, get the last commit to modify the file. We use this to be more specific
		// about what we're seeing, which is useful for the frontend
		lastRev, err := fileviewer.GitGetLastRevToTouchPath(path, repoConfig.Path, repoRev)
		if err != nil {
			// still attempt to load the file at the repo commit, which will probably fail
			commitToLoadFileAt = repoRev
		} else {
			commitToLoadFileAt = lastRev
		}
	}

	data, err := fileviewer.BuildFileData(path, repoConfig, commitToLoadFileAt)
	// fileContent not filled in, but filepath exists
	if err != nil {
		// if this errors out, most likely the file does not exist,
		// TODO: dicide if this is clean enough, or whether buildFileData
		// should always return a "default" fileviewercontext
		filename := filepath.Base(path)
		data = &fileviewer.FileViewerContext{
			Repo:       repoConfig,
			RepoRev:    repoRev,
			Commit:     commitToLoadFileAt,
			CommitHash: commitToLoadFileAt,
			FileContent: &fileviewer.SourceFileContent{
				FilePath: path,
				FileName: filename,
				Invalid:  true,
			},
			FilePath: path,
			FileName: filename,
		}
	}

	// if readmeContent is available, we put it into

	// these options do not depend on the file existing.
	// they do however depend on the `repoRev` being valid.
	// that will be something to tackle in the future <- TODO(xvandish)
	// TODO: use goroutines to do these in parallel
	tree, err := fileviewer.BuildDirectoryTree(path, repoConfig, repoRev)
	if err != nil {
		log.Printf(ctx, "Error building directory tree: %s\n", err.Error())
	}
	branches, err := fileviewer.ListAllBranches(repoConfig)
	if err != nil {
		log.Printf(ctx, "Error getting branches: %s\n", err.Error())
	}
	tags, err := fileviewer.ListAllTags(repoConfig)
	if err != nil {
		log.Printf(ctx, "Error getting tags: %s\n", err.Error())
	}

	data.DirectoryTree = tree
	data.Branches = branches
	data.Tags = tags
	data.RepoRev = repoRev
	data.RepoConfig = repoConfig
	data.HeadRev = headRev

	script_data := &struct {
		RepoConfig config.RepoConfig
		RepoName   string
		Commit     string
		CommitHash string
		RepoRev    string
		HeadRev    string
		FilePath   string
		FileName   string
		Branches   []fileviewer.GitBranch // TODO: fix this
	}{repoConfig, repoConfig.Name, data.Commit, data.CommitHash, data.RepoRev, data.HeadRev, data.FilePath, data.FileName, data.Branches}

	s.renderPage(ctx, w, r, "experimental.html", &page{
		Title:         "experimental",
		IncludeHeader: false,
		ScriptName:    "experimental",
		ScriptData:    script_data,
		Data:          data,
		BodyId:        "fileviewer-body",
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

	status := bk.getStatus()
	replyJSON(context.Background(), w, 200, status)
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
	log.Printf(ctx, "found template %s", templateName)

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

	log.Printf(ctx, "success %s", templateName)
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
	m.Add("GET", "/diff/:parent/:repo/:revA/:revB/", srv.Handler(srv.ServeDiff))

	// the following handlers render HTML that JS code fetches and inlines into the page
	// so the pages don't have any headers or extra things
	// m.Add("GET", "/raw/:parent/:repo/tree/:rev/", srv.Handler(srv.ServeGitBlobRaw))
	m.Add("GET", "/raw-blob/:parent/:repo/+/", srv.Handler(srv.ServeGitBlobRaw))
	m.Add("GET", "/experimental/:parent/:repo/+/", srv.Handler(srv.ServeExperimental))
	m.Add("GET", "/simple-git-log/", srv.Handler(srv.ServeSimpleGitLog))
	m.Add("GET", "/git-show/", srv.Handler(srv.ServeGitShow))
	m.Add("GET", "/about", srv.Handler(srv.ServeAbout))
	m.Add("GET", "/about-fileviewer", srv.Handler(srv.ServeAboutFileviewer))
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
	m.Add("GET", "/api/v2/getRenderedFileTree/:parent/:repo/:rev/", srv.Handler(srv.ServeGitLsTreeRendered))
	// m.Add("GET", "/delve/:parent/:repo/commits/:rev/", srv.Handler(srv.ServeSimpleGitLog))
	// m.Add("GET", "/api/v2/json/git-log/:parent/:repo/:rev/", srv.Handler(srv.ServeSimpleGitLogJson))
	m.Add("GET", "/api/v2/json/git-log/:parent/:repo/", srv.Handler(srv.ServeGitLogJson))
	m.Add("GET", "/api/v2/json/git-blame/:parent/:repo/:rev/", srv.Handler(srv.ServeGitBlameJson))
	m.Add("GET", "/api/v2/json/git-ls-tree/:parent/:repo/:rev/", srv.Handler(srv.ServeGitLsTreeJson))
	// m.Add("POST", "/api/v2/json/fileviewer-repos", srv.Handler(srv.ServeFileviewerRepos))

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

// working thoughts ---
// context: client -> webserver (Go) -> codesearch (c++)
//  fileviewer is implemented entirely in webserver/Go
//  search is implemented entirely in codesearch, webserver is just the wrapper that renders results
//
// when we want to highlight the matches of a query within a file, we'd like
// to not redo the search. However, a search may have timed out, e.g all matches
// within a file may not have completed. We could of course cache or not based on that basis (timed_out or not).
//
// The brute force method, when a query is present in a fileviewer request
// with the `?q={}` param, is for the webserver to do the search by using the golang regexp package to do a search
// The good news is that golang/regepx uses the same RE2 syntax. The bad news is that results may be slightly different than the codesearch backend ones, as the fold_case and is_regex params have the ability to tune the search query.
//
// The most consistent way would be to simply repeat the query to codesearch again, but filter for the exact path, and with an extremely high number of max_matches, in case the search is for something simple like `t`. Additionally, we should design an api that only returns match bounds for matches, so that we don't bloat the payload with information we're not going to use. That does not need to happen at first.

// if we just rely on codesearch, we should also make it so the local Find box also reaches out to codesearch, rather than using a local js regex find, to avoid confusing/conflicting results between the two regex implementations.

// a difficulty in actually highlighting matches for a query in a file is syntax highlighting messing everything up.
// a document is rendered as a table of lineNums and lines
// each line is split into `n` spans, not into a simple <pre>lineText</pre>
// when we have a range of text we want to highlight, that text may be split into `n` spans!! That means that we cannot just easily add/remove <mark></mark> tags around the text content at specific positions.
// The good news is the spans do not modify the text. So, possibly, we could have a presentation layer under the fileviewer, and when a range is highlighted we can use a css blend filter to have the highlight color blend with the syntax highlighted content.

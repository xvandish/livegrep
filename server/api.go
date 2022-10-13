package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"golang.org/x/net/context"

	"github.com/livegrep/livegrep/server/api"
	"github.com/livegrep/livegrep/server/log"
	"github.com/livegrep/livegrep/server/reqid"

	pb "github.com/livegrep/livegrep/src/proto/go_proto"
)

func replyJSON(ctx context.Context, w http.ResponseWriter, status int, obj interface{}) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(obj); err != nil {
		log.Printf(ctx, "writing http response, data=%s err=%q",
			asJSON{obj},
			err.Error())
	}
}

func writeError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	log.Printf(ctx, "error status=%d code=%s message=%q",
		status, code, message)
	replyJSON(ctx, w, status, &api.ReplyError{Err: api.InnerError{Code: code, Message: message}})
}

func writeQueryError(ctx context.Context, w http.ResponseWriter, err error) {
	if code := grpc.Code(err); code == codes.InvalidArgument {
		writeError(ctx, w, 400, "query", grpc.ErrorDesc(err))
	} else {
		writeError(ctx, w, 500, "internal_error",
			fmt.Sprintf("Talking to backend: %s", err.Error()))
	}
}

func getQueryError(err error) (errCode int, errorMsg string, errorMsgLong string) {
	if code := grpc.Code(err); code == codes.InvalidArgument {
		return 400, "query", grpc.ErrorDesc(err)
	} else {
		return 500, "internal_error",
			fmt.Sprintf("Talking to backend: %s", err.Error())
	}
}

func extractQuery(ctx context.Context, r *http.Request) (pb.Query, bool, error) {
	params := r.URL.Query()
	var query pb.Query
	var err error

	regex := true
	if re, ok := params["regex"]; ok && re[0] == "false" {
		regex = false
	}

	if q, ok := params["q"]; ok {
		query, err = ParseQuery(q[0], regex)
		log.Printf(ctx, "parsing query q=%q out=%s", q[0], asJSON{query})
	}

	// Support old-style query arguments
	if line, ok := params["line"]; ok {
		query.Line = line[0]
		if !regex {
			query.Line = regexp.QuoteMeta(query.Line)
		}
	}
	if file, ok := params["file"]; ok {
		query.File = file[0]
		if !regex {
			query.File = regexp.QuoteMeta(query.File)
		}
	}
	if repo, ok := params["repo"]; ok {
		query.Repo = repo[0]
		if !regex {
			query.Repo = regexp.QuoteMeta(query.Repo)
		}
	}

	if fc, ok := params["fold_case"]; ok {
		if fc[0] == "false" {
			query.FoldCase = false
		} else if fc[0] == "true" {
			query.FoldCase = true
		} else {
			query.FoldCase = strings.IndexAny(query.Line, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == -1
		}
	}

	return query, regex, err
}

var (
	ErrTimedOut = errors.New("timed out talking to backend")
)

func stringSlice(ss []string) []string {
	if ss != nil {
		return ss
	}
	return []string{}
}

func reverse(strings []string) []string {
	newSstrings := make([]string, 0, len(strings))
	for i := len(strings) - 1; i >= 0; i-- {
		newSstrings = append(newSstrings, strings[i])
	}
	return newSstrings
}

func convertBounds(bounds []*pb.Bounds) [][2]int {
	convertedBounds := make([][2]int, 0, len(bounds))
	for _, bound := range bounds {
		convertedBounds = append(convertedBounds, [2]int{int(bound.Left), int(bound.Right)})
	}

	return convertedBounds
}

func (s *server) doSearch(ctx context.Context, backend *Backend, q *pb.Query) (*api.ReplySearch, error) {
	var search *pb.CodeSearchResult
	var err error

	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if id, ok := reqid.FromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "Request-Id", string(id))
	}

	search, err = backend.Codesearch.Search(
		ctx, q,
		grpc.FailFast(false),
	)
	if err != nil {
		log.Printf(ctx, "error talking to backend err=%s", err)
		return nil, err
	}

	reply := &api.ReplySearch{
		Results:     make([]*api.Result, 0),
		FileResults: make([]*api.FileResult, 0),
		TreeResults: make([]*api.TreeResult, 0),
		SearchType:  "normal",
	}

	if q.FilenameOnly {
		reply.SearchType = "filename_only"
	} else if q.TreenameOnly {
		reply.SearchType = "treename_only"
	}

	for _, r := range search.Results {
		reply.Results = append(reply.Results, &api.Result{
			Tree:          r.Tree,
			Version:       r.Version,
			Path:          r.Path,
			LineNumber:    int(r.LineNumber),
			ContextBefore: stringSlice(r.ContextBefore),
			ContextAfter:  stringSlice(r.ContextAfter),
			Bounds:        convertBounds(r.Bounds),
			Line:          r.Line,
		})
	}

	// sort based on the repo name first, then file path
	sort.Slice(reply.Results, func(i, j int) bool {
		if reply.Results[i].Tree != reply.Results[j].Tree {
			return reply.Results[i].Tree < reply.Results[j].Tree
		}

		return reply.Results[i].Path < reply.Results[j].Path
	})

	for _, r := range search.FileResults {
		reply.FileResults = append(reply.FileResults, &api.FileResult{
			Tree:    r.Tree,
			Version: r.Version,
			Path:    r.Path,
			Bounds:  [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
		})
	}

	for _, r := range search.TreeResults {
		reply.TreeResults = append(reply.TreeResults, &api.TreeResult{
			Name:    r.Name,
			Version: r.Version,
			Bounds:  [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
			// Only GitHub links are enabled atm.
			Metadata: &api.Metadata{
				Labels:      r.Metadata.Labels,
				ExternalUrl: r.Metadata.Github + "/tree/" + r.Version,
			},
		})
	}

	reply.Info = &api.Stats{
		RE2Time:     search.Stats.Re2Time,
		GitTime:     search.Stats.GitTime,
		SortTime:    search.Stats.SortTime,
		IndexTime:   search.Stats.IndexTime,
		AnalyzeTime: search.Stats.AnalyzeTime,
		TotalTime:   int64(time.Since(start) / time.Millisecond),
		ExitReason:  search.Stats.ExitReason.String(),
		NumMatches:  int(search.Stats.NumMatches),
	}
	return reply, nil
}

func (s *server) doSearchV2(ctx context.Context, backend *Backend, q *pb.Query) (*api.ReplySearchV2, error) {
	var search *pb.CodeSearchResult
	var err error

	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if id, ok := reqid.FromContext(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "Request-Id", string(id))
	}

	// initial attempts at checking whether the backend is available before querying it
	// did not work very well. So instead we just send the request, if it fails, go to
	// the backup
	backendIdxUsed := false
	search, err = backend.Codesearch.Search(
		ctx, q,
		grpc.FailFast(true),
	)

	if err != nil && grpc.Code(err) == 14 && backend.BackupBackend != nil {
		backendIdxUsed = true
		log.Printf(ctx, "SEARCH ERROR: Primary backend unavailable. state=%s. Trying backup=%s",
			backend.GrpcClient.GetState(), backend.BackupBackend.Id)
		search, err = backend.BackupBackend.Codesearch.Search(
			ctx, q,
			grpc.FailFast(true),
		)
	}

	if err != nil {
		log.Printf(ctx, "error talking to backend(s) err=%s", err)
		return nil, err
	}

	reply := &api.ReplySearchV2{
		Results:        make([]*api.ResultV2, 0),
		FileResults:    make([]*api.FileResult, 0),
		TreeResults:    make([]*api.TreeResult, 0),
		SearchType:     "normal",
		IndexAge:       time.Since(time.Unix(search.IndexTime, 0)).Round(time.Minute).String(),
		LastIndexed:    time.Unix(search.IndexTime, 0).In(newYorkTime).Format("2006-01-02 3:04PM ET"),
		BackupIdxUsed:  backendIdxUsed,
		NextMaxMatches: int(q.MaxMatches) * 3,
		CurrMaxMatches: int(q.MaxMatches),
	}

	if q.FilenameOnly {
		reply.SearchType = "filename_only"
	} else if q.TreenameOnly {
		reply.SearchType = "treename_only"
	}

	// https://source.static.kevinlin.info/webgrep/file/src/server/logic/search.js#l130
	// the following logic is mostly the same as the function linked above, and should be attributed to Kevin Lin
	dedupedResults := make(map[string]*api.ResultV2)
	for _, r := range search.Results {
		key := fmt.Sprintf("%s-%s", r.Tree, r.Path)
		lineNumber := int(r.LineNumber)

		existingResult, present := dedupedResults[key]
		if !present {
			existingResult = &api.ResultV2{
				Tree:         r.Tree,
				Version:      r.Version,
				Path:         r.Path,
				ContextLines: make(map[int]*api.ResultLine),
				NumMatches:   0,
			}
		}
		existingResult.NumMatches += int(r.NumMatches)

		var contextLinesInit []string
		contextLinesInit = append(contextLinesInit, reverse(r.ContextBefore)...)
		contextLinesInit = append(contextLinesInit, r.Line)
		contextLinesInit = append(contextLinesInit, r.ContextAfter...)

		for idx, line := range contextLinesInit {
			contexLno := idx + lineNumber - len(r.ContextBefore)

			var bounds [][2]int
			if contexLno == lineNumber {
				bounds = convertBounds(r.Bounds)
			}

			// defer to the existing bounds information
			if present {
				if existingContextLine, exist := existingResult.ContextLines[contexLno]; exist {
					if len(existingContextLine.Bounds) > len(bounds) {
						bounds = existingContextLine.Bounds
					}
				}
			}

			existingResult.ContextLines[contexLno] = &api.ResultLine{
				LineNumber: contexLno,
				Bounds:     bounds,
				Line:       line,
			}
		}

		if !present {
			dedupedResults[key] = existingResult
		}

	}

	extensionCounts := make(map[string]int, len(dedupedResults))
	for treeAndPath, dededupedResult := range dedupedResults {
		ext := filepath.Ext(treeAndPath)
		extensionCounts[ext] += 1

		// Change the lines over to an array then sort by LineNumber
		dededupedResult.Lines = make([]*api.ResultLine, 0)
		for _, line := range dededupedResult.ContextLines {
			dededupedResult.Lines = append(dededupedResult.Lines, line)
		}
		// It's faster to sort after the fact than trying to maintain sort
		// order I believe
		sort.Slice(dededupedResult.Lines, func(i, j int) bool {
			lines := dededupedResult.Lines
			return lines[i].LineNumber < lines[j].LineNumber
		})

		reply.Results = append(reply.Results, dededupedResult)
	}

	// sort based on the repo name first, then file path
	sort.Slice(reply.Results, func(i, j int) bool {
		if reply.Results[i].Tree != reply.Results[j].Tree {
			return reply.Results[i].Tree < reply.Results[j].Tree
		}

		return reply.Results[i].Path < reply.Results[j].Path
	})

	extensionArray := make([]*api.FileExtension, 0, len(extensionCounts))
	idx := 0
	for ext, count := range extensionCounts {
		if ext == "" {
			continue
		}
		extensionArray = append(extensionArray, &api.FileExtension{Ext: ext, Count: count})
		idx += 1
	}

	sort.Slice(extensionArray, func(i, j int) bool {
		return extensionArray[i].Count > extensionArray[j].Count
	})
	// we want at most 5 exts, but if there are less than 2, no exts
	c := 5
	if len(extensionArray) < 5 {
		c = len(extensionArray)
	}
	if c < 2 {
		c = 0
	}
	reply.PopExts = extensionArray[:c]

	for _, r := range search.FileResults {
		reply.FileResults = append(reply.FileResults, &api.FileResult{
			Tree:    r.Tree,
			Version: r.Version,
			Path:    r.Path,
			Bounds:  [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
		})
	}

	for _, r := range search.TreeResults {
		reply.TreeResults = append(reply.TreeResults, &api.TreeResult{
			Name:    r.Name,
			Version: r.Version,
			Bounds:  [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
			// Only GitHub links are enabled atm.
			Metadata: &api.Metadata{
				Labels:      r.Metadata.Labels,
				ExternalUrl: r.Metadata.Github + "/tree/" + r.Version,
			},
		})
	}

	reply.Info = &api.Stats{
		RE2Time:     search.Stats.Re2Time,
		GitTime:     search.Stats.GitTime,
		SortTime:    search.Stats.SortTime,
		IndexTime:   search.Stats.IndexTime,
		AnalyzeTime: search.Stats.AnalyzeTime,
		TotalTime:   int64(time.Since(start) / time.Millisecond),
		ExitReason:  search.Stats.ExitReason.String(),
		NumMatches:  int(search.Stats.NumMatches),
	}
	return reply, nil
}

// TODO:(xvandish) Rename this to indicate that if not in query, pick first
func getBackendFromQuery(s *server, r *http.Request) (string, *Backend) {
	backendName := r.URL.Query().Get(":backend")
	var backend *Backend
	if backendName != "" {
		backend = s.bk[backendName]
	} else {
		for _, backend = range s.bk {
			break
		}
	}

	return backendName, backend
}

// we provide users a "show more" link that just multiples the current
// max_matches value by 3. Being careful not to modify max_matches if
// it is wrapped in () to escape it
func getNextPageUrl(searchQ url.Values, q *pb.Query, exitReason string) string {
	if exitReason == "NONE" {
		return ""
	}

	currQuery := searchQ.Get("q")
	newQuery := ""
	nextMaxMatches := int(q.MaxMatches) * 3

	// if the ` max_matches:` string already exists, replace its value
	if strings.Contains(currQuery, " max_matches:") {
		newQuery = strings.Replace(currQuery, fmt.Sprintf(" max_matches:%d", q.MaxMatches), fmt.Sprintf(" max_matches:%d", nextMaxMatches), 1)

	} else {
		newQuery = fmt.Sprintf("%s max_matches:%d", currQuery, nextMaxMatches)
	}

	searchQ.Set("q", newQuery)

	return "?" + searchQ.Encode()
}

// This function is internal to the app and not exposed.
// It is used to perform a search, and those results are then rendered to HTML
func (s *server) ServerSideAPISearchV2(ctx context.Context, w http.ResponseWriter, r *http.Request) (reply *api.ReplySearchV2, errCode int, errorMsg string, errorMsgLong string) {
	backendName, backend := getBackendFromQuery(s, r)

	if backend == nil {
		return nil, 400, "bad_backend", fmt.Sprintf("Unknown backend: %s", backendName)
	}

	q, is_regex, err := extractQuery(ctx, r)

	if err != nil {
		return nil, 400, "bad_query", err.Error()
	}

	if q.Line == "" {
		kind := "string"
		if is_regex {
			kind = "regex"
		}
		msg := fmt.Sprintf("You must specify a %s to match", kind)
		return nil, 400, "bad_query", msg
	}

	if q.MaxMatches == 0 {
		q.MaxMatches = s.config.DefaultMaxMatches
	}

	reply, err = s.doSearchV2(ctx, backend, &q)

	if err != nil {
		log.Printf(ctx, "error in search err=%s", err)
		errCode, errorMsg, errorMsgLong = getQueryError(err)
		return nil, errCode, errorMsg, errorMsgLong
	}

	reply.NextUrl = getNextPageUrl(r.URL.Query(), &q, reply.Info.ExitReason)

	if s.statsd != nil {
		s.statsd.Increment("api.search.v2.invocations")
		s.statsd.Increment("api.search.v2.exit_reason." + reply.Info.ExitReason)
		s.statsd.Timing("api.search.v2.re2_time", reply.Info.RE2Time)
		s.statsd.Timing("api.search.v2.git_time", reply.Info.GitTime)
		s.statsd.Timing("api.search.v2.sort_time", reply.Info.SortTime)
		s.statsd.Timing("api.search.v2.index_time", reply.Info.IndexTime)
		s.statsd.Timing("api.search.v2.analyze_time", reply.Info.AnalyzeTime)
		s.statsd.Timing("api.search.v2.total_time", reply.Info.TotalTime)
	}

	log.Printf(ctx,
		"responding success results=%d why=%s stats=%s",
		reply.Info.NumMatches,
		reply.Info.ExitReason,
		asJSON{reply.Info})

	return reply, 200, "", ""
}

func (s *server) ServeAPISearch(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	backendName := r.URL.Query().Get(":backend")
	var backend *Backend
	if backendName != "" {
		backend = s.bk[backendName]
		if backend == nil {
			writeError(ctx, w, 400, "bad_backend",
				fmt.Sprintf("Unknown backend: %s", backendName))
			return
		}
	} else {
		for _, backend = range s.bk {
			break
		}
	}

	q, is_regex, err := extractQuery(ctx, r)

	if err != nil {
		writeError(ctx, w, 400, "bad_query", err.Error())
		return
	}

	if q.Line == "" {
		kind := "string"
		if is_regex {
			kind = "regex"
		}
		msg := fmt.Sprintf("You must specify a %s to match", kind)
		writeError(ctx, w, 400, "bad_query", msg)
		return
	}

	if q.MaxMatches == 0 {
		q.MaxMatches = s.config.DefaultMaxMatches
	}

	reply, err := s.doSearch(ctx, backend, &q)

	if err != nil {
		log.Printf(ctx, "error in search err=%s", err)
		writeQueryError(ctx, w, err)
		return
	}

	if s.statsd != nil {
		s.statsd.Increment("api.search.v1.invocations")
		s.statsd.Increment("api.search.v1.exit_reason." + reply.Info.ExitReason)
		s.statsd.Timing("api.search.v1.re2_time", reply.Info.RE2Time)
		s.statsd.Timing("api.search.v1.git_time", reply.Info.GitTime)
		s.statsd.Timing("api.search.v1.sort_time", reply.Info.SortTime)
		s.statsd.Timing("api.search.v1.index_time", reply.Info.IndexTime)
		s.statsd.Timing("api.search.v1.analyze_time", reply.Info.AnalyzeTime)
		s.statsd.Timing("api.search.v1.total_time", reply.Info.TotalTime)
	}

	log.Printf(ctx,
		"responding success results=%d why=%s stats=%s",
		len(reply.Results),
		reply.Info.ExitReason,
		asJSON{reply.Info})

	replyJSON(ctx, w, 200, reply)
}

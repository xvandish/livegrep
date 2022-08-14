package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func reverseContext(context []*pb.Context) []*pb.Context {
	newContext := make([]*pb.Context, 0, len(context))
	for i := len(context) - 1; i >= 0; i-- {
		newContext = append(newContext, context[i])
	}
	return newContext
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
			Bounds:        [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
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

	search, err = backend.Codesearch.Search(
		ctx, q,
		grpc.FailFast(false),
	)

	if err != nil {
		log.Printf(ctx, "error talking to backend err=%s", err)
		return nil, err
	}

	reply := &api.ReplySearchV2{
		Results:     make([]*api.ResultV2, 0),
		FileResults: make([]*api.FileResult, 0),
		TreeResults: make([]*api.TreeResult, 0),
		SearchType:  "normal",
	}

	if q.FilenameOnly {
		reply.SearchType = "filename_only"
	} else if q.TreenameOnly {
		reply.SearchType = "treename_only"
	}

	// https://source.static.kevinlin.info/webgrep/file/src/server/logic/search.js#l130
	// the following logic is mostly the same as the function linked above, and should be attributed to Kevin Lin
	dedupedResults := make(map[string]*api.ResultV2)
	codeMatches := 0
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
				// Since any line can now be a resultLine, it doesn't make sense
				// to call these "contextLines"
			}
		}

		var contextLinesInit []*pb.Context
		contextLinesInit = append(contextLinesInit, reverseContext(r.ContextBeforeV2)...)
		contextLinesInit = append(contextLinesInit, &pb.Context{
			Line:   r.Line,
			Bounds: r.NewBounds,
		})
		contextLinesInit = append(contextLinesInit, r.ContextAfterV2...)

		for idx, line := range contextLinesInit {
			contexLno := idx + lineNumber - len(r.ContextBeforeV2)

			if len(line.Bounds) > 0 {
				codeMatches += len(line.Bounds)
			}

			bounds := line.Bounds
			// defer to the existing bounds information
			if present {
				if existingContextLine, exist := existingResult.ContextLines[contexLno]; exist {
					codeMatches -= len(line.Bounds)
					log.Printf(ctx, "line already existing. Checking whether it's worth overwriting")
					if len(existingContextLine.Bounds) > len(line.Bounds) {
						log.Printf(ctx, "the old line - %+v had more bounds than newLine %+v", existingContextLine, line)
						copy(existingContextLine.Bounds, bounds)
					}
					if line.Line != existingContextLine.Line {
						log.Printf(ctx, "unxecpted! line=%s existingContextLine=%s", line.Line, existingContextLine.Line)
					}
				}
			}

			existingResult.ContextLines[contexLno] = &api.ResultLine{
				LineNumber: contexLno,
				Bounds:     bounds,
				Line:       line.Line,
			}
		}

		// contextLno := r.LineNumber - len(r.ContextBefore_V2)

		// every context line should have been searched for matches (at least for literals)
		// so the only thing we really need to do is add lineNumbers

		// for int i = 0 range cont
		// contextLno := r.LineNumber - len(r.ContextBefore_V2)

		// for i := len(r.ContextBeforeV2) - 1; i > 0; i-- {
		// 	line := r.ContextBeforeV2[i]
		// 	contextLno := lineNumber - i
		// 	rl := api.ResultLine{
		// 		Line:       line.Line,
		// 		LineNumber: contextLno,
		// 		Bounds:     line.Bounds,
		// 	}
		// 	if prevLine, exist := existingResult.ContextLines[contextLno]; !exist {
		// 		existingResult.ContextLines[contextLno] = &rl
		// 	} else {
		// 		// take the line that actually represents something (not sure why)
		// 		// this is happening just yet - all lines should have identical text content

		// 		if len(rl.Line) > len(prevLine.Line) || len(rl.Bounds) > len(prevLine.Bounds) {
		// 			log.Printf(ctx, "overwriting %+v for %+v\n", prevLine, rl)
		// 			existingResult.ContextLines[contextLno] = &rl
		// 		}
		// 	}
		// }

		// now append the
		// actual line
		// matchLine := api.ResultLine{
		// 	Line:       r.Line,
		// 	LineNumber: lineNumber,
		// 	Bounds:     r.NewBounds,
		// }
		// codeMatches += 1
		// existingResult.ContextLines[lineNumber] = &matchLine

		// now append the afterContext
		// for i := 0; i < len(r.ContextAfterV2); i++ {
		// 	line := r.ContextAfterV2[i]
		// 	contextLno := lineNumber + i + 1
		// 	rl := api.ResultLine{
		// 		Line:       line.Line,
		// 		LineNumber: contextLno,
		// 		Bounds:     line.Bounds,
		// 	}
		// 	if _, exist := existingResult.ContextLines[contextLno]; exist {
		// 		log.Printf(ctx, "overwriting some context line\n")

		// 	}
		// 	existingResult.ContextLines[contextLno] = &rl
		// }

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

	exitReason := search.Stats.ExitReason.String()
	var numMatches int

	if q.FilenameOnly {
		numMatches = len(search.FileResults)
	} else if q.TreenameOnly {
		numMatches = len(search.TreeResults)
	} else {
		numMatches = codeMatches
	}

	reply.Info = &api.Stats{
		RE2Time:     search.Stats.Re2Time,
		GitTime:     search.Stats.GitTime,
		SortTime:    search.Stats.SortTime,
		IndexTime:   search.Stats.IndexTime,
		AnalyzeTime: search.Stats.AnalyzeTime,
		TotalTime:   int64(time.Since(start) / time.Millisecond),
		ExitReason:  exitReason,
		NumMatches:  numMatches,
		MoreAvail:   exitReason != "NONE",
	}

	log.Printf(ctx, "re2 time: %d\n", search.Stats.Re2Time)
	return reply, nil
}

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
		len(reply.Results),
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

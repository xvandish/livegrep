package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
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

	// New-style repo multiselect, only if "repo:" is not in the query.
	if query.Repo == "" {
		if newRepos, ok := params["repo[]"]; ok {
			for i := range newRepos {
				newRepos[i] = "^" + regexp.QuoteMeta(newRepos[i]) + "$"
			}
			query.Repo = strings.Join(newRepos, "|")
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
		SearchType:  "normal",
	}

	if q.FilenameOnly {
		reply.SearchType = "filename_only"
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

	for _, r := range search.FileResults {
		reply.FileResults = append(reply.FileResults, &api.FileResult{
			Tree:    r.Tree,
			Version: r.Version,
			Path:    r.Path,
			Bounds:  [2]int{int(r.Bounds.Left), int(r.Bounds.Right)},
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

	if s.honey != nil {
		e := s.honey.NewEvent()
		reqid, ok := reqid.FromContext(ctx)
		if ok {
			e.AddField("request_id", reqid)
		}
		e.AddField("backend", backend.Id)
		e.AddField("query_line", q.Line)
		e.AddField("query_file", q.File)
		e.AddField("query_repo", q.Repo)
		e.AddField("query_foldcase", q.FoldCase)
		e.AddField("query_not_file", q.NotFile)
		e.AddField("query_not_repo", q.NotRepo)
		e.AddField("max_matches", q.MaxMatches)

		e.AddField("result_count", len(reply.Results))
		e.AddField("re2_time", reply.Info.RE2Time)
		e.AddField("git_time", reply.Info.GitTime)
		e.AddField("sort_time", reply.Info.SortTime)
		e.AddField("index_time", reply.Info.IndexTime)
		e.AddField("analyze_time", reply.Info.AnalyzeTime)

		e.AddField("exit_reason", reply.Info.ExitReason)
		e.Send()
	}

	// I can log these things, but they have no context -
	// like I can't log a query {} took x seconds and so on
	if s.statsd != nil {
		s.statsd.Increment("search.invocations")

		// s.statsd.

		// I can easly log out the reply.Info stuff - but it's missing the context of what
		// triggered it. What point is there in knowning that search results are taking a
		// super long time if you can't tell what query it is thats giving you so much trouble?

		// StatsD isn't really designed (as far as I can tell) to support
		// a) tags
		// b) if tags are supported by a specific implementation, unbounded sources for tags,
		//	like request_id's aren't recommended.
		// As a result, we stick with the simplest metrics we can send accross. If you wan't
		// more functionality, you can switch to a provider-centric library, like a
		// DataDog specific libary, that will let you send events or different tags per
		// metric
		// If you leave it as is, if you notice a spike in

		// result_count doesn't really map super cleanly

		// Timing values are expected in milliseconds.
		s.statsd.Timing("search.re2_time")
		s.statsd.Timing("search.git_time")
		s.statsd.Timing("search.sort_time")
		s.statsd.Timing("search.index_time") // TODO: This isn't actually a timing value
		s.statsd.Timing("search.analyze_time")
		s.statsd.Timing("search.total_time")
	}

	log.Printf(ctx,
		"responding success results=%d why=%s stats=%s",
		len(reply.Results),
		reply.Info.ExitReason,
		asJSON{reply.Info})

	replyJSON(ctx, w, 200, reply)
}

package server

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
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

type ObjWithStatus struct {
	Obj    interface{} `json:"obj"`
	Status int         `json:"status"`
}

// I can have this function take a cache
func replyJSON(ctx context.Context, w http.ResponseWriter, status int, obj interface{}, cacheKey string) {
	// if cacheKey is present, we want to try to write to cache, so don't directly encode to w
	objToCache := ObjWithStatus{Obj: obj, Status: status}

	jData, err := json.Marshal(objToCache)

	if err != nil {
		log.Printf(ctx, "marshaling cache obj, data=%s err=%q",
			asJSON{jData},
			err.Error())
		return
	}

	// I should store the status with the redis key

	w.WriteHeader(status)
	enc := json.NewEncoder(w) // a double encode that I don't think we can get around
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

	// it makes the most sense to cache at the very top level
	// but, if I cache at the top level I can have several different
	// result types
	// a) error "bad_query"
	// b) error talking to codesearch backend - should retry?
	// c) *ApiReplySearch
	// the cached result should have a shape of *ApiReplySearch

	cacheParts := []string{backend.I.Name, strconv.Itoa(backend.I.IndexTime), r.URL.String()}
	cacheKey := strings.Join(cacheParts, "-")
	h := sha1.New()
	h.Write([]byte(cacheKey))
	cacheKey = string(h.Sum(nil))

	// check the cache to see if this is available
	// how should we store the cache key since it's possible it has multiple shapes

	// if not, then -
	// cache is always write safe if it's a bad query error
	// const isCacheWriteSafe = params.indexIdentity &&
	// 	data.indexName === params.indexIdentity.name &&
	// 	parseInt(data.indexTime, 10) === params.indexIdentity.timestamp;

	// p, err := c.Get(key)
	// if err != nil {
	// return err
	// }
	var cacheObj ObjWithStatus
	err := json.Unmarshal(p, &cacheObj)

	if err != nil {
		log.Printf("error reading cache entry. Key=%s err=%s", cacheKey, err)
	} else {
		log.Printf("cache hit on key: %s", cacheKey)
		w.WriteHeader(cacheObj.Status)
		w.Write(cacheObj.Obj)
		return
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

	log.Printf(ctx,
		"responding success results=%d why=%s stats=%s",
		len(reply.Results),
		reply.Info.ExitReason,
		asJSON{reply.Info})

	replyJSON(ctx, w, 200, reply)
}

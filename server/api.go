package server

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/go-redis/redis/v8"
	"golang.org/x/net/context"

	"github.com/livegrep/livegrep/server/api"
	"github.com/livegrep/livegrep/server/log"
	"github.com/livegrep/livegrep/server/reqid"

	pb "github.com/livegrep/livegrep/src/proto/go_proto"
)

type CachedResponse struct {
	ResBytes []byte `json:"res_bytes"`
	Status   int    `json:"status"`
}

func isRedisCacheEnabled(s *server) bool {
	return s.redis != nil
}

func getCacheKeyForSearch(s *server, bk *Backend, url string) string {
	if !isRedisCacheEnabled(s) {
		return ""
	}

	cacheParts := []string{s.config.RedisCacheConfig.KeyPrefix, bk.I.Name, bk.I.IndexTime.String(), url}
	cacheKey := strings.Join(cacheParts, "-")
	h := sha1.New()
	h.Write([]byte(cacheKey))
	return string(h.Sum(nil))
}

func timeTrack(ctx context.Context, start time.Time, name string) {
	elapsed := time.Since(start)
	log.Printf(ctx, "%s took %s", name, elapsed)
}

func checkCacheForSearchResult(ctx context.Context, s *server, cacheKey string) (result *CachedResponse) {
	defer timeTrack(ctx, time.Now(), "checkCacheForSearchResult")
	if !isRedisCacheEnabled(s) {
		return nil
	}

	val, redisErr := s.redis.Get(ctx, cacheKey).Result()

	if redisErr == redis.Nil {
		log.Printf(ctx, "cache miss")
	} else if redisErr != nil {
		log.Printf(ctx, "error reading cache entry. Key=%s err=%s", cacheKey, redisErr)
	} else {
		log.Printf(ctx, "cache hit")
		var cacheObj CachedResponse
		err := json.Unmarshal([]byte(val), &cacheObj)

		if err != nil {
			log.Printf(ctx, "error unmarshaling cache response: %v\n", err)
		}

		return &cacheObj
	}

	return nil
}

func writeToCache(ctx context.Context, s *server, status int, cacheKey string, resBytes []byte) {
	defer timeTrack(ctx, time.Now(), "writeToCache")
	if cacheKey != "" && s.redis != nil {
		objToCache := CachedResponse{ResBytes: resBytes, Status: status}
		jData, err := json.Marshal(objToCache)

		if err != nil {
			log.Printf(ctx, "marshaling cache obj, data=%s err=%q",
				asJSON{jData},
				err.Error())
		} else {
			log.Printf(ctx, "keyTTL: %v", s.config.RedisCacheConfig)
			redisErr := s.redis.Set(ctx, cacheKey, jData, s.config.RedisCacheConfig.KeyTTLD).Err()
			if redisErr != nil {
				log.Printf(ctx, "failed to write to redis cache: %v", redisErr)
			} else {
				log.Printf(ctx, "cache write succeeded")
			}
		}
	}

}

// I can have this function take a cache
func replyJSON(ctx context.Context, w http.ResponseWriter, status int, obj interface{}, cacheKey string, s *server) {
	// if cacheKey is present, we want to try to write to cache, so don't directly encode to w
	objBytes, err := json.Marshal(obj)

	if err != nil {
		log.Printf(ctx, "marshaling http response, data=%s err=%q",
			asJSON{obj},
			err.Error())
		return
	}

	writeToCache(ctx, s, status, cacheKey, objBytes)

	w.WriteHeader(status)
	w.Header().Set("Content-Type", "application/json") // otherwise Go looks at first 512 bytes of w.Write contents
	_, err = w.Write(objBytes)

	if err != nil {
		log.Printf(ctx, "writing http response, data=%s err=%q",
			asJSON{obj},
			err.Error())
	}
}

func writeError(ctx context.Context, w http.ResponseWriter, status int, code, message string, cacheKey string, s *server) {
	log.Printf(ctx, "error status=%d code=%s message=%q",
		status, code, message)
	replyJSON(ctx, w, status, &api.ReplyError{Err: api.InnerError{Code: code, Message: message}}, cacheKey, s)
}

func writeQueryError(ctx context.Context, w http.ResponseWriter, err error, cacheKey string, s *server) {
	if code := grpc.Code(err); code == codes.InvalidArgument {
		writeError(ctx, w, 400, "query", grpc.ErrorDesc(err), cacheKey, s)
	} else {
		writeError(ctx, w, 500, "internal_error",
			fmt.Sprintf("Talking to backend: %s", err.Error()), "", s)
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

func reverse(strings []string) []string {
	newSstrings := make([]string, 0, len(strings))
	for i := len(strings) - 1; i >= 0; i-- {
		newSstrings = append(newSstrings, strings[i])
	}
	return newSstrings
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
		FileResults: make([]*api.FileResultV2, 0),
		SearchType:  "normal",
	}

	if q.FilenameOnly {
		reply.SearchType = "filename_only"
	}

	// https://source.static.kevinlin.info/webgrep/file/src/server/logic/search.js#l130
	// the following logic is mostly the same as the function linked above, and should be attributed to Kevin Lin
	dedupedResults := make(map[string]*api.ResultV2)
	codeMatches := 0
	dedupStart := time.Now()
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
			}
		}

		var contextLinesInit []string
		// There has to be a better way?
		contextLinesInit = append(contextLinesInit, reverse(r.ContextBefore)...)
		contextLinesInit = append(contextLinesInit, r.Line)
		contextLinesInit = append(contextLinesInit, r.ContextAfter...)

		// Now for every contextLine, transform it into a resultLines
		for idx, line := range contextLinesInit {
			contextLno := idx + lineNumber - len(r.ContextBefore)
			var bounds []int

			if contextLno == lineNumber {
				codeMatches += 1
				bounds = append(bounds, int(r.Bounds.Left), int(r.Bounds.Right))
			}

			// Defer to the existing bounds information
			if present {
				if existingContextLine, exist := existingResult.ContextLines[contextLno]; exist {
					if len(existingContextLine.Bounds) == 2 {
						copy(existingContextLine.Bounds, bounds)
					}
				}
			}
			existingResult.ContextLines[contextLno] = &api.ResultLine{
				LineNumber: contextLno,
				Bounds:     bounds,
				Line:       line}
		}

		if !present {
			dedupedResults[key] = existingResult
		}

	}

	log.Printf(ctx, "dedup took %s", time.Since(dedupStart))

	// Set the number of matches we've found
	reply.CodeMatches = codeMatches

	for _, dededupedResult := range dedupedResults {
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

	// We don't need to de-duplicate fileResults, there is already
	// code that checks for that
	for _, r := range search.FileResults {
		reply.FileResults = append(reply.FileResults, &api.FileResultV2{
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
	defer timeTrack(ctx, time.Now(), "ServeAPISearch")
	backendName := r.URL.Query().Get(":backend")
	var backend *Backend
	if backendName != "" {
		backend = s.bk[backendName]
		if backend == nil {
			writeError(ctx, w, 400, "bad_backend",
				fmt.Sprintf("Unknown backend: %s", backendName), "", s)
			return
		}
	} else {
		for _, backend = range s.bk {
			break
		}
	}

	cacheKey := getCacheKeyForSearch(s, backend, r.URL.String())
	if cachedRes := checkCacheForSearchResult(ctx, s, cacheKey); cachedRes != nil {
		defer timeTrack(ctx, time.Now(), "writeCacheRes")
		w.WriteHeader(cachedRes.Status)
		w.Header().Set("Content-Type", "application/json") // otherwise Go looks at first 512 bytes of w.Write contents
		w.Write(cachedRes.ResBytes)
		return
	}

	q, is_regex, err := extractQuery(ctx, r)

	if err != nil {
		writeError(ctx, w, 400, "bad_query", err.Error(), cacheKey, s)
		return
	}

	if q.Line == "" {
		kind := "string"
		if is_regex {
			kind = "regex"
		}
		msg := fmt.Sprintf("You must specify a %s to match", kind)
		writeError(ctx, w, 400, "bad_query", msg, cacheKey, s)
		return
	}

	if q.MaxMatches == 0 {
		q.MaxMatches = s.config.DefaultMaxMatches
	}

	reply, err := s.doSearch(ctx, backend, &q)

	if err != nil {
		log.Printf(ctx, "error in search err=%s", err)
		writeQueryError(ctx, w, err, cacheKey, s)
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

	replyJSON(ctx, w, 200, reply, cacheKey, s)
}

// Maybe later we can abstract out common parts of this?
func (s *server) ServeAPISearchV2(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	defer timeTrack(ctx, time.Now(), "ServeAPISearch")
	backendName := r.URL.Query().Get(":backend")
	var backend *Backend
	if backendName != "" {
		backend = s.bk[backendName]
		if backend == nil {
			writeError(ctx, w, 400, "bad_backend",
				fmt.Sprintf("Unknown backend: %s", backendName), "", s)
			return
		}
	} else {
		for _, backend = range s.bk {
			break
		}
	}

	cacheKey := getCacheKeyForSearch(s, backend, r.URL.String())
	if cachedRes := checkCacheForSearchResult(ctx, s, cacheKey); cachedRes != nil {
		defer timeTrack(ctx, time.Now(), "writeCacheRes")
		w.WriteHeader(cachedRes.Status)
		w.Header().Set("Content-Type", "application/json") // otherwise Go looks at first 512 bytes of w.Write contents
		w.Write(cachedRes.ResBytes)
		return
	}

	q, is_regex, err := extractQuery(ctx, r)

	if err != nil {
		writeError(ctx, w, 400, "bad_query", err.Error(), cacheKey, s)
		return
	}

	if q.Line == "" {
		kind := "string"
		if is_regex {
			kind = "regex"
		}
		msg := fmt.Sprintf("You must specify a %s to match", kind)
		writeError(ctx, w, 400, "bad_query", msg, cacheKey, s)
		return
	}

	if q.MaxMatches == 0 {
		q.MaxMatches = s.config.DefaultMaxMatches
	}

	reply, err := s.doSearchV2(ctx, backend, &q)

	if err != nil {
		log.Printf(ctx, "error in search err=%s", err)
		writeQueryError(ctx, w, err, cacheKey, s)
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

	replyJSON(ctx, w, 200, reply, cacheKey, s)
}

func (s *server) APISearchV2(ctx context.Context, w http.ResponseWriter, r *http.Request) (reply *api.ReplySearchV2, errCode int, errorMsg string, errorMsgLong string) {
	defer timeTrack(ctx, time.Now(), "ServeAPISearch")
	backendName := r.URL.Query().Get(":backend")
	var backend *Backend
	if backendName != "" {
		backend = s.bk[backendName]
		if backend == nil {
			return nil, 400, "bad_backend", fmt.Sprintf("Unknown backend: %s", backendName)
		}
	} else {
		for _, backend = range s.bk {
			break
		}
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

	log.Printf(ctx,
		"responding success results=%d why=%s stats=%s",
		len(reply.Results),
		reply.Info.ExitReason,
		asJSON{reply.Info})

	return reply, 200, "", ""
}

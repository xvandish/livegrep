package api

type InnerError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ReplyError is returned along with any non-200 status reply
type ReplyError struct {
	Err InnerError `json:"error"`
}

// ReplySearch is returned to /api/v1/search/:backend
type ReplySearch struct {
	Info               *Stats               `json:"info"`
	Results            []*Result            `json:"results"`
	DedupedResults     []*DedupedResult     `json:"dedupedResult"`
	DedupedFileResults []*DedupedFileResult `json:"dedupedFileResults"`
	FileResults        []*FileResult        `json:"file_results"`
	SearchType         string               `json:"search_type"`
}

type Stats struct {
	RE2Time     int64  `json:"re2_time"`
	GitTime     int64  `json:"git_time"`
	SortTime    int64  `json:"sort_time"`
	IndexTime   int64  `json:"index_time"`
	AnalyzeTime int64  `json:"analyze_time"`
	TotalTime   int64  `json:"total_time"`
	ExitReason  string `json:"why"`
}

type Result struct {
	Tree          string   `json:"tree"`
	Version       string   `json:"version"`
	Path          string   `json:"path"`
	LineNumber    int      `json:"lno"`
	ContextBefore []string `json:"context_before"`
	ContextAfter  []string `json:"context_after"`
	Bounds        [2]int   `json:"bounds"`
	Line          string   `json:"line"`
}

type ResultLine struct {
	LineNumber int `json:"lno"`
	// Bounds may or may not be defined. If they are,
	// then this line is a match. Otherwise it's contex
	Bounds []int  `json:"bounds"`
	Line   string `json:"line"`
}

type DedupedResult struct {
	Tree    string        `json:"repo"` // tree -> repo
	Version string        `json:"version"`
	Path    string        `json:"path"`
	Lines   []*ResultLine `json:"lines"`
	// Will never be sent over wire, used to deduplicate
	LinesByContext map[int]*ResultLine `json:"linesByContext"`
}

type FileResult struct {
	Tree    string `json:"tree"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Bounds  [2]int `json:"bounds"`
}

type DedupedFileResult struct {
	Tree    string `json:"repo"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Bounds  [2]int `json:"bounds"`
}

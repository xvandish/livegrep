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
	Info        *Stats        `json:"info"`
	Results     []*Result     `json:"results"`
	FileResults []*FileResult `json:"file_results"`
	SearchType  string        `json:"search_type"`
	CodeMatches int           `json:"code_matches"`
}

type ReplySearchV2 struct {
	Info        *Stats        `json:"info"`
	Results     []*ResultV2   `json:"results"`
	FileResults []*FileResult `json:"file_results"`
	SearchType  string        `json:"search_type"`
	CodeMatches int           `json:"code_matches"`
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

type ResultV2 struct {
	Tree    string        `json:"repo"` // tree -> repo
	Version string        `json:"version"`
	Path    string        `json:"path"`
	Lines   []*ResultLine `json:"lines"`
	// Will never be sent over wire, used to deduplicate
	ContextLines map[int]*ResultLine
}

type ResultLine struct {
	LineNumber int `json:"lno"`
	// Bounds may or may not be defined. If they are,
	// then this line is a match. Otherwise it's contex
	Bounds []int  `json:"bounds"`
	Line   string `json:"line"`
}

type FileResult struct {
	Tree    string `json:"tree"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Bounds  [2]int `json:"bounds"`
}

package api

import (
	"io/fs"

	"github.com/sergi/go-diff/diffmatchpatch"
)

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
	TreeResults []*TreeResult `json:"tree_results"`
	SearchType  string        `json:"search_type"`
}

// api/v2/search/:backend
type ReplySearchV2 struct {
	Info           *Stats           `json:"info"`
	Results        []*ResultV2      `json:"results"`
	FileResults    []*FileResult    `json:"file_results"`
	TreeResults    []*TreeResult    `json:"tree_results"`
	SearchType     string           `json:"search_type"`
	PopExts        []*FileExtension `json:"popular_extensions"` // at most 5 common extensions in search
	IndexAge       string           `json:"index_age"`
	LastIndexed    string           `json:"last_indexed"`
	BackupIdxUsed  bool             `json:"backup_idx_used"`
	CurrMaxMatches int              `json:"curr_max_matches"`
	NextMaxMatches int              `json:"next_max_matches"`
	NextUrl        string           `json:"next_url"`
}

type FileExtension struct {
	Ext   string
	Count int
}

type Stats struct {
	RE2Time     int64  `json:"re2_time"`
	GitTime     int64  `json:"git_time"`
	SortTime    int64  `json:"sort_time"`
	IndexTime   int64  `json:"index_time"`
	AnalyzeTime int64  `json:"analyze_time"`
	TotalTime   int64  `json:"total_time"`
	ExitReason  string `json:"why"`
	NumMatches  int    `json:"num_matches"`
}

type Metadata struct {
	Labels      []string `json:"labels"`
	ExternalUrl string   `json:"external_url"`
}

type Result struct {
	Tree          string   `json:"tree"`
	Version       string   `json:"version"`
	Path          string   `json:"path"`
	LineNumber    int      `json:"lno"`
	ContextBefore []string `json:"context_before"`
	ContextAfter  []string `json:"context_after"`
	Bounds        [][2]int `json:"bounds"`
	Line          string   `json:"line"`
}

type ResultV2 struct {
	Tree    string        `json:"tree"`
	Version string        `json:"version"`
	Path    string        `json:"path"`
	Lines   []*ResultLine `json:"lines"`
	// Will never be sent over wire, used to deduplicate
	ContextLines map[int]*ResultLine `json:"-"`
	NumMatches   int                 `json:"num_matches"`
}

type Bounds struct {
	Left  int
	Right int
}

type Context struct {
	Line   string   `json:"line"`
	Bounds []Bounds `json:"bounds"`
}

type ResultLine struct {
	LineNumber int `json:"lno"`
	// Bounds may or may not be defined. If they are,
	// then this line is a match. Otherwise it's contex
	Bounds [][2]int `json:"bounds"`
	Line   string   `json:"line"`
}

type FileResult struct {
	Tree    string `json:"tree"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Bounds  [2]int `json:"bounds"`
}

type TreeResult struct {
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Metadata *Metadata `json:"metadata"`
	Bounds   [2]int    `json:"bounds"`
}

// TODO:(xvandish) Find a way to colocate this type closer to the fileview.
// The server has access to fileview structs, but template.go does not. If we
// decide to JS render instead of server render that fixes our problems...
// When this is a treeNode

// TODO(xvandish): Going to to to tie a set of TreeNodes to a spefici repo/commit
type DirTree struct {
	RootDir *TreeNode
	Commit  string
	Repo    string
}

type TreeNode struct {
	Name      string // like service.go
	Path      string // like src/service.go
	Mode      fs.FileMode
	Hash      string
	ParentDir *TreeNode
	Type      string
	Children  []*TreeNode
}

type DiffLine2 struct {
	// A line can be made of up of eqalities, inserts, deletes or any combination
	Line []*DiffPart
	Lno  uint32
}

type DiffPart struct {
	Text string
	Type diffmatchpatch.Operation
}

// either a left or right side of the diff
type SplitDiffHalf struct {
	Lines []*DiffLine2
	// LinesMap map[uint32]*DiffLine2
	LinesMap map[uint32]uint32

	// We use a line map. That, however, removes our ability to insert blank lines
	// so what we'll need to do is have the keys of LinesMap point to indices in in .Lines

	// when appending parts of a line together, this line
	// is a "buffer" per-se, of previously seen parts that all belong
	// on the same line. Once we determine that the buffer should be
	// flushed, it is written to lines
	LineUnderConstruction *DiffLine2
	// contentLength, type, filename?
}

type SplitDiff struct {
	LeftDiff  *SplitDiffHalf
	RightDiff *SplitDiffHalf
}

type GitBranch struct {
	Name   string
	IsHead bool
}

type GitTag struct {
	Name   string
	IsHead bool
}

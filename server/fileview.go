package server

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/livegrep/livegrep/server/api"
	"github.com/livegrep/livegrep/server/config"
)

// Mapping from known file extensions to filetype hinting.
var filenameToLangMap map[string]string = map[string]string{
	"BUILD":       "python",
	"BUILD.bazel": "python",
	"WORKSPACE":   "python",
}
var extToLangMap map[string]string = map[string]string{
	".adoc":        "AsciiDoc",
	".asc":         "AsciiDoc",
	".asciidoc":    "AsciiDoc",
	".AppleScript": "applescript",
	".bzl":         "python",
	".c":           "c",
	".coffee":      "coffeescript",
	".cpp":         "cpp",
	".css":         "css",
	".go":          "go",
	".h":           "cpp",
	".hs":          "haskell",
	".html":        "html",
	".java":        "java",
	".js":          "javascript",
	".json":        "json",
	".jsx":         "jsx",
	".m":           "objectivec",
	".markdown":    "markdown",
	".md":          "markdown",
	".mdown":       "markdown",
	".mkdn":        "markdown",
	".mediawiki":   "markdown",
	".nix":         "nix",
	".php":         "php",
	".pl":          "perl",
	".proto":       "go",
	".py":          "python",
	".pyst":        "python",
	".rb":          "ruby",
	".rdoc":        "markdown",
	".rs":          "rust",
	".scala":       "scala",
	".scpt":        "applescript",
	".scss":        "scss",
	".sh":          "bash",
	".sky":         "python",
	".sql":         "sql",
	".swift":       "swift",
	".textile":     "markdown",
	".ts":          "typescript",
	".tsx":         "tsx",
	".wiki":        "markdown",
	".xml":         "xml",
	".yaml":        "yaml",
	".yml":         "yaml",
}

// Grabbed from the extensions GitHub supports here - https://github.com/github/markup
var supportedReadmeExtensions = []string{
	"markdown", "mdown", "mkdn", "md", "textile", "rdoc", "org", "creole", "mediawiki", "wiki",
	"rst", "asciidoc", "adoc", "asc", "pod",
}

var supportedReadmeRegex = buildReadmeRegex(supportedReadmeExtensions)

type breadCrumbEntry struct {
	Name string
	Path string
}

type directoryListEntry struct {
	Name          string
	Path          string
	IsDir         bool
	SymlinkTarget string
}

type fileViewerContext struct {
	PathSegments    []breadCrumbEntry
	Repo            config.RepoConfig
	RepoRev         string // the commit/rev the repo is being viewed at (branch, commit, tag, etc)
	Commit          string
	CommitHash      string
	ShortCommitHash string
	DirContent      *directoryContent
	FileContent     *sourceFileContent
	ExternalDomain  string
	Permalink       string
	Headlink        string
	LogLink         string
	BlameData       *BlameResult

	// the following two are sourced from either FileContent or
	// DirContent.ReadmeContent. If both are nil, then Path is path
	// but FileName is empty
	FilePath string
	FileName string

	DirectoryTree *api.TreeNode
	Branches      []api.GitBranch
	Tags          []api.GitTag
}

type sourceFileContent struct {
	Content   string
	LineCount int
	Language  string
	FileName  string
	FilePath  string
	BlameData *BlameResult
	Invalid   bool
}

type directoryContent struct {
	Entries       []directoryListEntry
	ReadmeContent *sourceFileContent
}

type DirListingSort []directoryListEntry

func timeTrack(start time.Time, name string) {
	fmt.Printf("%s took %s\n", name, time.Since(start))
}

func (s DirListingSort) Len() int {
	return len(s)
}

func (s DirListingSort) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s DirListingSort) Less(i, j int) bool {
	if s[i].IsDir != s[j].IsDir {
		return s[i].IsDir
	}
	return s[i].Name < s[j].Name
}

func gitCommitHash(ref string, repoPath string) (string, error) {
	out, err := exec.Command(
		"git", "-C", repoPath, "rev-parse", ref,
	).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitObjectType(obj string, repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "cat-file", "-t", obj)
	fmt.Printf("cmd=%s\n", cmd.String())
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCatBlob(obj string, repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "cat-file", "blob", obj).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// used to get the "real" name of "HEAD"
func gitRevParseAbbrev(rev string, repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", rev).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

type gitTreeEntry struct {
	Mode       string
	ObjectType string
	ObjectId   string
	ObjectName string
}

func gitParseTreeEntry(line string) gitTreeEntry {
	dataAndPath := strings.SplitN(line, "\t", 2)
	dataFields := strings.Split(dataAndPath[0], " ")
	return gitTreeEntry{
		Mode:       dataFields[0],
		ObjectType: dataFields[1],
		ObjectId:   dataFields[2],
		ObjectName: dataAndPath[1],
	}
}

func gitListDir(obj string, repoPath string) ([]gitTreeEntry, error) {
	out, err := exec.Command("git", "-C", repoPath, "cat-file", "-p", obj).Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	lines = lines[:len(lines)-1]
	result := make([]gitTreeEntry, len(lines))
	for i, line := range lines {
		result[i] = gitParseTreeEntry(line)
	}
	return result, nil
}

func viewUrl(repo string, path string, isDir bool) string {
	entryType := "blob"
	if isDir {
		entryType = "tree"
	}
	return "/delve/" + repo + "/" + entryType + "/" + "HEAD/" + path
}

func getFileUrl(repo string, pathFromRoot string, name string, isDir bool) string {
	fileUrl := viewUrl(repo, filepath.Join(pathFromRoot, path.Clean(name)), isDir)
	if isDir {
		fileUrl += "/"
	}
	return fileUrl
}

func buildReadmeRegex(supportedReadmeExtensions []string) *regexp.Regexp {
	// Sort in descending order of length so most specific match is selected by regex engine
	sort.Slice(supportedReadmeExtensions, func(i, j int) bool {
		return len(supportedReadmeExtensions[i]) >= len(supportedReadmeExtensions[j])
	})

	// Build regex of form "README.(ext1|ext2)" README case insensitive
	var buf bytes.Buffer
	for i, ext := range supportedReadmeExtensions {
		buf.WriteString(regexp.QuoteMeta(ext))
		if i < len(supportedReadmeExtensions)-1 {
			buf.WriteString("|")
		}
	}
	repoRegexAlt := buf.String()
	repoFileRegex := regexp.MustCompile(fmt.Sprintf("((?i)readme)\\.(%s)", repoRegexAlt))

	return repoFileRegex
}

func buildDirectoryListEntry(treeEntry gitTreeEntry, pathFromRoot string, repo config.RepoConfig) directoryListEntry {
	var fileUrl string
	var symlinkTarget string
	if treeEntry.Mode == "120000" {
		resolvedPath, err := gitCatBlob(treeEntry.ObjectId, repo.Path)
		if err == nil {
			symlinkTarget = resolvedPath
		}
	} else {
		fileUrl = getFileUrl(repo.Name, pathFromRoot, treeEntry.ObjectName, treeEntry.ObjectType == "tree")
	}
	return directoryListEntry{
		Name:          treeEntry.ObjectName,
		Path:          fileUrl,
		IsDir:         treeEntry.ObjectType == "tree",
		SymlinkTarget: symlinkTarget,
	}
}

/*
* The format below outputs
* commit someCommit <shortHash>
* author <SomeName> <someEmail>
* subject ......
* date authorDate in iso8601
* body ............
* \x00 (null seperator from the -z option)
 */
var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ah%nbody %b"

// The named capture groups are just for human readability
var gitLogRegex = regexp.MustCompile("(?ms)" + `commit\s(?P<commitHash>\w*)\s<(?P<shortHash>\w*)>\nauthor\s<(?P<authorName>[^>]*)>\s<(?P<authorEmail>[^>]*)>\nsubject\s(?P<commitSubject>[^\n]*)\ndate\s(?P<commitDate>[^\n]*)\nbody\s(?P<commitBody>[\s\S]*?)\x00`)

// Later on when we add support for CommitCommiter we can abstract Author to it's own struct
type Commit struct {
	Hash              string
	ShortHash         string
	ParentHashes      []string
	ParentShortHashes []string
	AuthorName        string
	AuthorEmail       string
	Date              string
	Subject           string
	Body              string
}

// Add more as we need it
// Next parent needs to be fixed up so that we don't get the first commit of a paged
// response with the same commit as the last commit as the prev response: e.g.
// commit x
// commit y
// commit y
// commit z
type SimpleGitLog struct {
	Commits          []*Commit
	MaybeLastPage    bool
	IsPaginationReq  bool
	NextParent       string // hash of the commit
	CommitLinkPrefix string // like xvandish/livegrep xvandish=parent livegrep=repo
	Repo             config.RepoConfig
	PathSegments     []breadCrumbEntry
	Path             string
}

func getPathSegments(pathSplits []string, repo config.RepoConfig) []breadCrumbEntry {
	segments := make([]breadCrumbEntry, len(pathSplits))
	for i, name := range pathSplits {
		parentPath := path.Clean(strings.Join(pathSplits[0:i], "/"))
		segments[i] = breadCrumbEntry{
			Name: name,
			Path: getFileUrl(repo.Name, parentPath, name, true),
		}
	}

	return segments
}

// We should add a bound for this - make it max at 3 seconds (use project-vi as reference)
func buildSimpleGitLogData(relativePath string, firstParent string, repo config.RepoConfig) (*SimpleGitLog, error) {
	cleanPath := path.Clean(relativePath)
	start := time.Now()
	out, err := exec.Command("git", "-C", repo.Path, "log", "-n", "1000", "-z", "--pretty="+customGitLogFormat, firstParent, "--", cleanPath).Output()
	fmt.Printf("took %s to get git log\n", time.Since(start))
	if err != nil {
		fmt.Printf("err=%s\n", err.Error())
		return nil, err
	}
	// Null terminate our thing
	start = time.Now()
	out = append(out, byte(rune(0)))
	err = os.WriteFile("./tmp-log", out, 0644)
	if err != nil {
		fmt.Printf("err=%s\n", err.Error())
		return nil, err
	}

	matches := gitLogRegex.FindAllSubmatch(out, -1)

	simpleGitLog := SimpleGitLog{}
	simpleGitLog.Commits = make([]*Commit, len(matches))
	// fmt.Printf("git log out=%s\n", out)
	// fmt.Printf("git log matches=%+v\n", matches)

	for i, match := range matches {
		fmt.Printf("match_matches_len=%d\n", len(match))
		if len(match) != 8 {
			log.Fatalf("GIT_LOG_ERROR: match len < 8: %+v\n", match)
			continue
		}
		simpleGitLog.Commits[i] = &Commit{
			Hash:        string(match[1]),
			ShortHash:   string(match[2]),
			AuthorName:  string(match[3]),
			AuthorEmail: string(match[4]),
			Subject:     string(match[5]),
			Date:        string(match[6]),
			Body:        string(match[7]),
		}
	}

	simpleGitLog.MaybeLastPage = len(simpleGitLog.Commits) < 1000
	simpleGitLog.IsPaginationReq = firstParent != "HEAD"
	simpleGitLog.NextParent = simpleGitLog.Commits[len(simpleGitLog.Commits)-1].Hash
	simpleGitLog.Repo = repo
	simpleGitLog.PathSegments = getPathSegments(strings.Split(cleanPath, "/"), repo)
	simpleGitLog.Path = cleanPath

	return &simpleGitLog, nil
}

// Add more as we need it
// Next parent needs to be fixed up so that we don't get the first commit of a paged
// response with the same commit as the last commit as the prev response: e.g.
// commit x
// commit y
// commit y
// commit z

// When we get fancier/decide what to do, we can make add to this

type DiffLine struct {
	Line     string
	LineType string // can be one of "context", "insert", "delete"
}

type Diff struct {
	Header      string
	HeaderLines []string
	ChunkLine   string // may not be necessary to have a special ref to it
	Lines       []*DiffLine
	HunkNum     int
}

// src/whatever/whatever.c | 15 +++++++-----
type StatLine struct {
	Path             string // src/whatever/whatever.c
	LinesChanged     string // 15
	GraphStringPlus  string // +++++
	GraphStringMinus string // ----
	HunkNum          int    // used to link to say, #h0, which is the diff of this path
}

type DiffStat struct {
	StatLines   []*StatLine
	SummaryLine string // 4 files changed, 50 insertions(+), 6 deletions(-)
}

type GitShow struct {
	Commit   *Commit // basic commit info
	Diffs    []*Diff
	DiffStat *DiffStat
	Repo     config.RepoConfig
}

// var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ai%nbody %b"
var customShowFormat = "format:%H%x00" +
	"%h%x00" +
	"%P%x00" +
	"%p%x00" +
	"%an%x00" +
	"%ae%x00" +
	"%s%x00" +
	"%ai%x00" +
	"%b%x00"

// var gitShowRegex = regexp.MustCompile("(?ms)" + `commit\s(?P<commitHash>\w*)\s<(?P<shortHash>\w*)>\nparent\s(?P<parentHash>\w*)\s<(?P<shortParentHash>\w*)>\nauthor\s<(?P<authorName>[^>]*)>\s<(?P<authorEmail>[^>]*)>\nsubject\s(?P<commitSubject>[^\n]*)\ndate\s(?P<commitDate>[^\n]*)\nbody\s(?P<commitBody>[\s\S]*?)\n?---\n(?P<diffStat>.*)\x00(?P<diffText>.*)`)

// used to parse src/whatever/whatever.c | 15 +++++++-----
var diffStatLineRegex = regexp.MustCompile("([^\\s]*)\\s*\\|\\s*(\\d*)\\s*(.*)")

// dropCR drops a terminal \r from the data.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}

func ScanGitShowEntry(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\x00'); i >= 0 {
		// we have a full non-terminated line
		return i + 1, dropCR(data[0:i]), nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it
	if atEOF {
		return len(data), dropCR(data), nil
	}
	// request more data
	return 0, nil, nil
}

// Given a specific commitHash, get detailed info (--numstat or --shortstat)
func gitShowCommit(repo config.RepoConfig, commit string) (*GitShow, error) {
	defer timeTrack(time.Now(), "gitShowCommit")

	// git show 74846d35b24b6efd61bb88a0a750b6bb257e6e78 --patch-with-stat -z > out.txt
	cmd := exec.Command("git", "-C", repo.Path, "show", commit,
		// this is a shorthand for --patch and --stat
		"--patch-with-stat",
		"--pretty="+customShowFormat,

		// print a null byte to seperate the initial information from the diffs
		"-z",

		// treat a merge commit as a diff against the first parent
		"--first-parent",

		"--diff-algorithm=histogram",
	)

	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)
	scanner.Split(ScanGitShowEntry) // read null byte delimited

	var gitCommit Commit
	var gitShow GitShow

	scanner.Scan()
	gitCommit.Hash = string(scanner.Bytes())

	scanner.Scan()
	gitCommit.ShortHash = string(scanner.Bytes())

	scanner.Scan()
	parentCommits := bytes.Split(scanner.Bytes(), []byte(" "))
	for _, pc := range parentCommits {
		gitCommit.ParentHashes = append(gitCommit.ParentHashes, string(pc))
	}

	scanner.Scan()
	parentShortCommits := bytes.Split(scanner.Bytes(), []byte(" "))
	for _, psc := range parentShortCommits {
		gitCommit.ParentShortHashes = append(gitCommit.ParentShortHashes, string(psc))
	}

	scanner.Scan()
	gitCommit.AuthorName = string(scanner.Bytes())

	scanner.Scan()
	gitCommit.AuthorEmail = string(scanner.Bytes())

	scanner.Scan()
	gitCommit.Subject = string(scanner.Bytes())

	scanner.Scan()
	gitCommit.Date = string(scanner.Bytes())

	scanner.Scan()
	gitCommit.Body = string(scanner.Bytes())

	// Add the commit in
	gitShow.Commit = &gitCommit

	scanner.Scan()

	diffStat := DiffStat{}
	diffStatBuff := bytes.NewBuffer(scanner.Bytes())
	diffStatBuff.ReadBytes('\n') // we read the first useless line, which is ---\n
	hunkNum := 0
	for {
		line, err := diffStatBuff.ReadBytes('\n')

		if err != nil {
			break
		}

		match := diffStatLineRegex.FindSubmatch(line)

		if len(match) == 0 {
			diffStat.SummaryLine = string(line)
			break
		}

		graphString := string(match[3])
		var graphStringPlus, graphStringMinus string
		fIdxOfPlus := strings.Index(graphString, "+")
		fIdxOfMinus := strings.Index(graphString, "-")

		if fIdxOfPlus > -1 {
			graphStringPlus = graphString[fIdxOfPlus : strings.LastIndex(graphString, "+")+1]
		}

		if fIdxOfMinus > -1 {
			graphStringMinus = graphString[fIdxOfMinus : strings.LastIndex(graphString, "-")+1]
		}

		statLine := StatLine{
			HunkNum:          hunkNum,
			Path:             string(match[1]),
			LinesChanged:     string(match[2]),
			GraphStringPlus:  graphStringPlus,
			GraphStringMinus: graphStringMinus,
		}

		diffStat.StatLines = append(diffStat.StatLines, &statLine)
		hunkNum += 1
	}

	scanner.Scan()

	// We'll have to see how this behaves with long lines
	diffBuf := bytes.NewBuffer(scanner.Bytes())
	var currDif *Diff
	hunkNum = 0

	// 	diff --git a/arch/x86/kernel/cpu/perf_event_intel.c b/arch/x86/kernel/cpu/perf_event_intel.
	// index 224c952071f9..c135ed735b22 100644
	// --- a/arch/x86/kernel/cpu/perf_event_intel.c
	// +++ b/arch/x86/kernel/cpu/perf_event_intel.c
	// @@ -767,
	for {
		line, err := diffBuf.ReadBytes('\n')

		if err != nil {
			// assuming we've hit an EOL
			if currDif != nil {
				gitShow.Diffs = append(gitShow.Diffs, currDif)
			}
			break
		}

		s := string(line)
		if strings.HasPrefix(s, "diff") {
			if currDif != nil { // end the prev diff
				gitShow.Diffs = append(gitShow.Diffs, currDif)
				hunkNum += 1
			}
			currDif = &Diff{
				Header:  s,
				HunkNum: hunkNum,
			}
			continue
		} else if strings.HasPrefix(s, "@@") {
			currDif.ChunkLine = s
			continue
		}

		// If we haven't seen the @@ line yet, then add to header info
		if currDif.ChunkLine == "" {
			currDif.HeaderLines = append(currDif.HeaderLines, s)
		} else {
			firstChar := s[0:1]
			var diffLine DiffLine
			if firstChar == "+" {
				diffLine.LineType = "insert"
			} else if firstChar == "-" {
				diffLine.LineType = "delete"
			} else {
				diffLine.LineType = "context"
			}
			diffLine.Line = s
			currDif.Lines = append(currDif.Lines, &diffLine)
		}

	}

	gitShow.DiffStat = &diffStat
	gitShow.Commit = &gitCommit
	gitShow.Repo = repo

	return &gitShow, nil
}

type BlameResult struct {
	Path               string              // the filepath of the file being blamed
	Commit             string              // the commit of the file being blamed
	LinesToBlameChunk  map[int]*BlameChunk `json:"-"`
	BlameChunks        []*BlameChunk       `json:"blame_chunks"`
	LineNumsToBlameIdx map[int]int         `json:"linenums_to_blame_idx"`
}

type LineRange struct {
	StartLine int
	EndLine   int
}

// Blame chunk represents `n` contigous BlameLines that are from the same commit
type BlameChunk struct {
	CommitHash         string // the SHA that all lines within this chunk represent
	ShortHash          string
	CommitLink         string
	PrevCommitHash     string
	AuthorName         string
	AuthorEmail        string
	AuthorTime         int64 // ?
	CommitterName      string
	CommitterEmail     string
	CommitterTime      int64
	CommitSummary      string
	Filename           string
	PreviousFilename   string
	PreviousCommitHash string
	LineRanges         []*LineRange
	alreadyFilled      bool
}

var BlameChunkHeader = regexp.MustCompile(`\A([0-9a-f]{40})\s(\d+)\s(\d+)\s(\d+)\z`)
var LineInChunkHeader = regexp.MustCompile(`\A[0-9a-f]{40}\s\d+\s(\d+)\z`)

const (
	AuthorKey        = "author "
	AuthorMailKey    = "author-mail "
	AuthorTimeKey    = "author-time "
	CommitterKey     = "committer "
	CommitterMailKey = "committer-mail "
	CommitterTimeKey = "committer-time " // TODO(xvandish): Committer TZ
	SummaryKey       = "summary "
	PreviousKey      = "previous "
	FilenameKey      = "filename "
)

// Given a repo, a file in that repo and a commit, get the git blame for that file
//

func deleteKey(line, key string) string {
	return strings.Replace(line, key, "", 1)
}

func processNextChunk(scanner *bufio.Scanner, commitHashToChunkMap map[string]*BlameChunk, lineNumberToChunkMap map[int]*BlameChunk, repoPath string, filePath string) (moreChunkLeft bool, err error) {
	// read the first line. This will be in the following format
	// <gitCommitHash> <lnoInOriginalFile> <lnoInFinalFile> <linesInChunk>
	// like:
	// 549be0aad5faaa57160cdb5d3d4c75feee29ceed 1 1 6
	// so for example, the header above says:
	//   1. Line 1 came from commit 549be0aad5faaa57160cdb5d3d4c75feee29ceed
	//   2. The following 5 lines (6 - 1) are also from that commit
	moreLeft := scanner.Scan()
	if !moreLeft {
		return false, nil
	}

	// TODO: check if hit EOF
	headerLine := scanner.Text()

	matches := BlameChunkHeader.FindStringSubmatch(headerLine)
	if matches == nil {
		return false, fmt.Errorf("unexpected format of line %#v in git blame output.", headerLine)
	}

	commitHash := matches[1]

	currLineNumber, err := strconv.Atoi(matches[3])
	linesInChunk, err := strconv.Atoi(matches[4])
	if err != nil {
		return false, err
	}

	// Get or create the BlameChunk for this commitHash
	chunk := commitHashToChunkMap[commitHash]
	if chunk == nil {
		chunk = &BlameChunk{}
		chunk.CommitHash = commitHash
		chunk.ShortHash = commitHash[:8]
		chunk.CommitLink = fmt.Sprintf("/delve/%s/commit/%s", repoPath, commitHash)
		chunk.alreadyFilled = false
		// chunk.LineRanges = append(chunk.LineRanges, LineRange{StartLine: currLineNumber, EndLine: currLineNumber + (linesInChunk - 1)})
		// chunk.StartLine = currLineNumber
		// chunk.EndLine = currLineNumber + (linesInChunk - 1)
		commitHashToChunkMap[commitHash] = chunk
	}

	// attempt to merge this chunk interval with the previous, if they're consecutive. Sometimes blame
	// doesn't do this for us
	startLine := currLineNumber
	endLine := currLineNumber + (linesInChunk - 1)
	lastIdx := len(chunk.LineRanges) - 1
	// if chunk.ShortHash == "8aba1988" {
	// 	fmt.Printf("%s - currLineNumber=%d linesInChunk=%d\n", commitHash[:8], currLineNumber, linesInChunk)
	// 	fmt.Printf("headerLine=%s\n", headerLine)
	// }
	// if lastIdx >= 0 && chunk.ShortHash == "8aba1988" {
	// 	prevRange := chunk.LineRanges[lastIdx]
	// 	fmt.Printf("prevRange=%+v\n", prevRange)
	// 	fmt.Printf("startLine=%d endLine=%d\n", startLine, endLine)
	// 	fmt.Printf("wouldMerge=%t\n", endLine-1 == prevRange.EndLine)
	// }
	if lastIdx >= 0 && endLine-1 == chunk.LineRanges[lastIdx].EndLine {
		chunk.LineRanges[lastIdx].EndLine = endLine
		// if chunk.ShortHash == "8aba1988" {
		// 	fmt.Printf("merged interval\n")
		// }
	} else {
		chunk.LineRanges = append(chunk.LineRanges, &LineRange{StartLine: startLine, EndLine: endLine})
	}

	// now, keep scanning until we hit `linesInChunk` codeLines (`\t` lines
	for linesInChunk != 0 {
		scanner.Scan()
		line := scanner.Text()

		// if chunk.ShortHash == "8aba1988" {
		// 	fmt.Printf("chunk-line=%s\n", line)
		// }

		if matches := LineInChunkHeader.FindStringSubmatch(line); matches != nil {
			currLineNumber, err = strconv.Atoi(matches[1])
		} else if strings.HasPrefix(line, "\t") {
			if !chunk.alreadyFilled {
				chunk.alreadyFilled = true
			}
			lineNumberToChunkMap[currLineNumber] = chunk
			linesInChunk -= 1
		}

		// if we've already input this info, don't redo
		if chunk.alreadyFilled {
			continue
		}

		if strings.HasPrefix(line, AuthorKey) {
			chunk.AuthorName = deleteKey(line, AuthorKey)
		} else if strings.HasPrefix(line, AuthorMailKey) {
			chunk.AuthorEmail = deleteKey(line, AuthorMailKey)
		} else if strings.HasPrefix(line, AuthorTimeKey) {
			authorTime := deleteKey(line, AuthorTimeKey)
			timestamp, err := strconv.ParseInt(authorTime, 10, 64)
			if err != nil {
				return true, nil
			}
			chunk.AuthorTime = timestamp
		} else if strings.HasPrefix(line, CommitterKey) {
			chunk.CommitterName = deleteKey(line, CommitterKey)
		} else if strings.HasPrefix(line, CommitterMailKey) {
			chunk.CommitterEmail = deleteKey(line, CommitterMailKey)
		} else if strings.HasPrefix(line, CommitterTimeKey) {
			committerTime := deleteKey(line, CommitterTimeKey)
			timestamp, err := strconv.ParseInt(committerTime, 10, 64)
			if err != nil {
				return true, nil
			}
			chunk.CommitterTime = timestamp
		} else if strings.HasPrefix(line, SummaryKey) {
			chunk.CommitSummary = deleteKey(line, SummaryKey)
		} else if strings.HasPrefix(line, FilenameKey) {
			chunk.Filename = deleteKey(line, FilenameKey)
		} else if strings.HasPrefix(line, PreviousKey) {
			chunk.PreviousCommitHash = line[:40]
			chunk.PreviousFilename = line[41:]
		}
	}

	return true, nil
}

func gitBlameBlob(relativePath string, repo config.RepoConfig, commit string) (*BlameResult, error) {
	defer timeTrack(time.Now(), "gitBlameBlob")

	// technically commiId isn't required, but we always blame with a commit
	// git -C <repo> blame --porcelain <filename> [<commitId>]
	start := time.Now()
	cleanPath := path.Clean(relativePath)
	cmd := exec.Command("git", "-C", repo.Path, "blame", cleanPath, commit, "--porcelain")

	stdout, err := cmd.StdoutPipe()
	fmt.Printf("took %s to do command\n", time.Since(start))

	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	var blameRes BlameResult

	commitHashToChunkMap := make(map[string]*BlameChunk)
	lnoToChunkMap := make(map[int]*BlameChunk)

	for {
		hasMore, err := processNextChunk(scanner, commitHashToChunkMap, lnoToChunkMap, repo.Name, cleanPath)
		if !hasMore {
			break
		} else if err != nil {
			return nil, err
		}

	}
	// fmt.Printf("chunkMap: %+v\n", lnoToChunkMap)
	// fmt.Printf("chunkMap hash: %+v\n", commitHashToChunkMap)

	blameChunks := make([]*BlameChunk, 0, len(commitHashToChunkMap))
	for _, chunk := range commitHashToChunkMap {
		blameChunks = append(blameChunks, chunk)
	}
	// sort.Slice(blameChunks, func(i, j int) bool {
	// 	return blameChunks[i].StartLine < blameChunks[j].StartLine
	// })
	fmt.Printf("there are %d commits in map, and len of chunks is %d\n", len(commitHashToChunkMap), len(blameChunks))
	fmt.Printf("blameRes: %+v\n", blameRes)
	blameRes.LinesToBlameChunk = lnoToChunkMap
	blameRes.BlameChunks = blameChunks

	return &blameRes, nil
}

var fileDoesNotExistError = errors.New("This file does not exist at this point in history")

func buildFileData(relativePath string, repo config.RepoConfig, commit string) (*fileViewerContext, error) {
	fmt.Printf("buildFileData - commit=%s\n", commit)
	commitHash := commit
	out, err := gitCommitHash(commit, repo.Path)
	if err == nil {
		commitHash = out[:strings.Index(out, "\n")]
	}
	cleanPath := path.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}
	obj := commitHash + ":" + cleanPath
	pathSplits := strings.Split(cleanPath, "/")

	var fileContent *sourceFileContent
	var dirContent *directoryContent

	objectType, err := gitObjectType(obj, repo.Path)

	// if there is an error here, most likely this file does not exist at obj
	// we still want the fileviewer to load, and we want to display a message like
	// "The file does not exist at the commit"
	if err != nil {
		log.Printf("error getting object type: %v\n", err)
		return nil, err
	}

	if objectType == "tree" {
		fmt.Printf("objectType is tree\n")
		treeEntries, err := gitListDir(obj, repo.Path)
		if err != nil {
			return nil, err
		}

		dirEntries := make([]directoryListEntry, len(treeEntries))
		var readmePath, readmeLang, readmeName string
		for i, treeEntry := range treeEntries {
			dirEntries[i] = buildDirectoryListEntry(treeEntry, cleanPath, repo)
			// Git supports case sensitive files, so README.md & readme.md in the same tree is possible
			// so in this case we just grab the first matching file
			if readmePath != "" {
				continue
			}

			parts := supportedReadmeRegex.FindStringSubmatch(dirEntries[i].Name)
			if len(parts) != 3 {
				continue
			}
			readmeName = parts[0]
			readmePath = obj + parts[0]
			readmeLang = parts[2]
		}

		var readmeContent *sourceFileContent
		if readmePath != "" {
			fmt.Printf("readmePath != empty\n")
			if content, err := gitCatBlob(readmePath, repo.Path); err == nil {
				readmeContent = &sourceFileContent{
					Content:   content,
					LineCount: strings.Count(content, "\n"),
					Language:  extToLangMap["."+readmeLang],
					FileName:  readmeName,
					FilePath:  relativePath,
				}
			}
		}

		sort.Sort(DirListingSort(dirEntries))
		dirContent = &directoryContent{
			Entries:       dirEntries,
			ReadmeContent: readmeContent,
		}
	} else if objectType == "blob" {
		fmt.Printf("objectType is blob\n")
		content, err := gitCatBlob(obj, repo.Path)
		if err != nil {
			return nil, err
		}
		filename := filepath.Base(cleanPath)
		language := filenameToLangMap[filename]
		if language == "" {
			language = extToLangMap[filepath.Ext(cleanPath)]
		}
		fileContent = &sourceFileContent{
			Content: content,
			// LineCount: strings.Count(string(content), "\n"),
			LineCount: 0,
			Language:  language,
			FileName:  filename,
			FilePath:  relativePath,
		}
	}

	segments := make([]breadCrumbEntry, len(pathSplits))
	for i, name := range pathSplits {
		parentPath := path.Clean(strings.Join(pathSplits[0:i], "/"))
		segments[i] = breadCrumbEntry{
			Name: name,
			Path: getFileUrl(repo.Name, parentPath, name, true),
		}
	}

	externalDomain := "external viewer"
	if url, err := url.Parse(repo.Metadata["url_pattern"]); err == nil {
		externalDomain = url.Hostname()
	}

	permalink := ""
	headlink := ""
	if !strings.HasPrefix(commitHash, commit) {
		permalink = "?commit=" + commitHash[:16]
	} else {
		if dirContent != nil {
			headlink = "."
		} else {
			headlink = segments[len(segments)-1].Name
		}
	}

	fmt.Printf("FileContetn: %+v\n", fileContent)

	normalizedName, normalizedPath := getFileNameAndPathFromContent(fileContent, dirContent)
	return &fileViewerContext{
		PathSegments:    segments,
		Repo:            repo,
		Commit:          commit,
		CommitHash:      commitHash,
		ShortCommitHash: commitHash[:8],
		DirContent:      dirContent,
		FileContent:     fileContent,
		ExternalDomain:  externalDomain,
		Permalink:       permalink,
		Headlink:        headlink,
		FilePath:        normalizedPath,
		FileName:        normalizedName,
	}, nil
}

/*
 * Gets the name from either FileContent or DirectoryContent.ReadmeContent, depending
 * on which is not nil
 */
func getFileNameAndPathFromContent(fc *sourceFileContent, dc *directoryContent) (string, string) {
	if fc != nil {
		return fc.FileName, fc.FilePath
	}

	return dc.ReadmeContent.FileName, dc.ReadmeContent.FilePath
}

// TODO: add capability to diff files
func buildDiffData(relativePath string, repo config.RepoConfig, commitA, commitB string) {}

const (
	maxTreeDepth      = 1024
	startingStackSize = 8
)

var (
	ErrMaxTreeDepth      = errors.New("maximum tree depth exceeded")
	ErrFileNotFound      = errors.New("file not found")
	ErrDirectoryNotFound = errors.New("directory not found")
	ErrEntryNotFound     = errors.New("entry not found")
)

// type DirTree struct {
// 	Entries []*TreeEntry
// 	Hash string

// 	m map[string]*TreeEntry
// 	t map[string]*Tree // tree path cache
// }

// type Dir struct {
// 	Entries []*TreeEntry
// }

// type TreeNode struct {
// 	Name      string
// 	Mode      fs.FileMode
// 	Hash      string
// 	ParentDir *TreeNode
// 	Type      string
// 	Children  []*TreeNode
// }

/*
Given
blob    text
dir    hello
blob    hello/text
blob    me
dir    yo
blob   yo/hello
dir    text/

I want to parse it into a tree like so

TreeNode {
  Children = {
	TreeNode{ Name=text, Type=blob },
	TreeNode{ Name=hello, Type=dir
		Children = [
			TreeNode{ Name=text, Type=blob}
		]
	},
	TreeNode{ Name=me, Type=blob },
	TreeNode{ Name=yo, Type=dir
		Children = [
			TreeNode{ Name=hello, Type=blob}
		]
	},
	TreeNode{ Name=yo, Type=dir
		Children = [
			TreeNode{ Name=hello, Type=blob}
		]
	},
	TreeNode{ Name=yo, Type=dir Children = []},


  }
}
*/

// At a given commit, build the directory tree
// The frontend will have to be responsible for traversing it and finding/opening the current
func buildDirectoryTree(relativePath string, repo config.RepoConfig, commit string) *api.TreeNode {
	// cleanPath := path.Clean(relativePath)
	// to start out, we always compute the tree for the root.
	defer timeTrack(time.Now(), "buildDirectoryTree")
	cmd := exec.Command("git", "-C", repo.Path, "ls-tree",
		"--long", // show size
		"--full-name",
		"-z",
		"-r", // for recursion
		"-t",
		commit,
	)
	fmt.Printf("cmd=%s\n", cmd.String())

	out, err := cmd.CombinedOutput()

	if err != nil {
		log.Fatal(err)
	}

	lines := strings.Split(string(out), "\x00")
	rootDir := &api.TreeNode{Name: "root"}
	currDir := rootDir
	prevDepth := 0

	for i, line := range lines {
		// fmt.Printf("line=%s\n", line)
		if i == len(lines)-1 {
			// last entry is empty
			continue
		}
		tabPos := strings.IndexByte(line, '\t')
		if tabPos == -1 {
			// return nil, errors.Errorf("invalid `git ls-tree` output: %q", out)
			log.Fatalf("invalid ls-tree output")
		}

		info := strings.SplitN(line[:tabPos], " ", 4)
		name := line[tabPos+1:]

		if len(info) != 4 {

			log.Fatalf("invalid ls-tree output")
			// return nil, errors.Errorf("invalid `git ls-tree` output: %q", out)
		}

		typ := info[1] // blob,commit,tree
		sha := info[2]

		// TODO(xvandish): Check that the sha is a valid git sha

		sizeStr := strings.TrimSpace(info[3])
		var size int64
		if sizeStr != "-" {
			// Size of "-" indicates a dir or submodule.
			size, err = strconv.ParseInt(sizeStr, 10, 64)
			if err != nil || size < 0 {
				// return nil, errors.Errorf("invalid `git ls-tree` size output: %q (error: %s)", sizeStr, err)
				log.Fatalf("invalid ls-tree output")
			}
		}

		modeVal, err := strconv.ParseInt(info[0], 8, 32)
		if err != nil {
			log.Fatalf(err.Error())
			// return nil, err
		}

		mode := os.FileMode(modeVal)

		treeEntry := &api.TreeNode{
			Name: name,
			Path: name,
			Mode: mode,
			Hash: sha,
			Type: typ,
		}

		// oh no, what about files with a slash in them..
		pathDepth := strings.Count(name, "/")
		// fmt.Printf("pathDepth=%d\n", pathDepth)

		// 1777b4d56ea1471f155fa21fbf8d2969dcc3ce9e     600       cmd/server/main.go
		// 60c6f7580d7e6651739c86865e3c012a04650e4d       -       creds (prevDepth == 2)
		for prevDepth > pathDepth {
			currDir = currDir.ParentDir
			prevDepth -= 1
		}

		// fmt.Printf("appending %s to %s children\n", treeEntry.Name, currDir.Name)
		currDir.Children = append(currDir.Children, treeEntry)

		// now that we've backuped up to the correct location, we "correct" name so that
		// /folder/file
		// is stored as
		// /folder
		//    /file
		// instead of
		// /folder
		//    /folder/file
		treeEntry.Name = filepath.Base(treeEntry.Name)

		// if this entry is a directory, set currDir to ourselves, and up prevDepth
		if typ == "tree" {
			// fmt.Printf("nesting to dir with name=%s\n", treeEntry.Name)
			treeEntry.ParentDir = currDir
			currDir = treeEntry
			prevDepth += 1
		}
	}

	// fmt.Printf("%+v\n", rootDir)
	return rootDir
}

// func (r *singleStringReplacer) Replace(s string) string {\n
//     var buf
//     []byte as delete to append to prevLine, `Builder` as an insert to append to prevLine
//     i, matched := 0, false to insert.. How do we determin that this belongs on a newLine?
// 	   for {
// match := r.finder.next(s[i:])
// if match == -1 {
// 	break
// }
// matched = true
//

// take the lineUnderConstruction, if any, append it to Lines, and clear buffer
func flushBuffer(sd *api.SplitDiffHalf) {
	if sd.LineUnderConstruction == nil {
		return
	}

	sd.Lines = append(sd.Lines, sd.LineUnderConstruction)
	// undo the pointer
	sd.LineUnderConstruction = nil
}

func addDiff(sd *api.SplitDiffHalf, text string, diffType diffmatchpatch.Operation, lno uint32) {
	diffPart := &api.DiffPart{Text: text, Type: diffType}

	// line, present := sd.LinesMap[lno]
	lineIdx, present := sd.LinesMap[lno]
	var line *api.DiffLine2

	fmt.Printf("adding %+v to line=%d\n", diffPart, lno)

	// we have an existing line, so we should append whatever text we have to it
	if present {
		line = sd.Lines[lineIdx]
		line.Line = append(line.Line, diffPart)
		return
	}

	line = &api.DiffLine2{}
	line.Line = make([]*api.DiffPart, 0)
	line.Line = append(line.Line, diffPart)
	line.Lno = lno + 1 // for 1 based line numbers

	// append to .Lines
	sd.Lines = append(sd.Lines, line)

	// store the index where we appended into LinesMap
	sd.LinesMap[lno] = uint32(len(sd.Lines) - 1)

}

func addBlankLine(sd *api.SplitDiffHalf) {
	newLine := &api.DiffLine2{
		// Lno:  -1,
		Line: []*api.DiffPart{&api.DiffPart{Text: "----------------------------------", Type: diffmatchpatch.DiffEqual}},
	}

	sd.Lines = append(sd.Lines, newLine)
}

func addRealBlankLine(sd *api.SplitDiffHalf, lno uint32) {
	newLine := &api.DiffLine2{
		Lno:  lno + 1,
		Line: []*api.DiffPart{&api.DiffPart{Text: "\n", Type: diffmatchpatch.DiffEqual}},
	}

	sd.Lines = append(sd.Lines, newLine)

}

type newlines struct {
	// locs is the sorted set of byte offsets of the newlines in the file
	locs []uint32

	// fileSize is just the number of bytes in the file. It is stored
	// on this struct so we can safely know the length of the last line
	// in the file since not all files end in a newline.
	fileSize uint32
}

// TODO(xvandish): diffmatchpatch operates on []rune
// Can this func work on runes directly?
// gitCatBlob returns a string (that was initially a []byte)
func getNewlines(data string) newlines {
	var locs []uint32

	for i, c := range data {
		if c == '\n' {
			locs = append(locs, uint32(i))
		}
	}

	return newlines{
		locs:     locs,
		fileSize: uint32(len(data)),
	}
}

// atOffset returns the line number of the line containing the offset. If the offset lands on
// the newline ending line M, we return M.  The line is characterized
// by its linenumber (base-1, byte index of line start, byte index of
// line end). The line end is the index of a newline, or the filesize
// (if matching the last line of the file.)
func (nls newlines) atOffset(offset uint32) (lineNumber uint32) {
	idx := sort.Search(len(nls.locs), func(n int) bool {
		return nls.locs[n] >= offset
	})

	// we may want to not add +1
	// return idx + 1
	return uint32(idx)
}

// Actually, that's not the goal...
// Think about this more later
func balanceDiffs(sdLeft, sdRight *api.SplitDiffHalf) {

}

// oh no! we need to do this after the fact, otherwise we're going to end up with wonky line numbers..
// I think we should instead do this on the frontend, so that we don't need api calls to expand collapse content
// func collapseContext(firstDiff, lastDiff bool, lines *[]string) {
// 	// if firstDiff, remove top lines
// 	if firstDiff && len(*lines) > 5 {
// 		*lines = (*lines)[len(*lines)-5:]
// 		return
// 	}

// 	// if lastDiff, remove bottom lines
// 	if lastDiff && len(*lines) > 5 {
// 		*lines = (*lines)[:5]
// 		return
// 	}

// 	// otherwise, remove middle lines
// 	if len(*lines) <= 6 {
// 		return
// 	}

// 	// find the midpoint
// 	// 3lines -- hidden -- 3lines
// }

func generateSplitDiffForFile(relativePath string, repo config.RepoConfig, oldRev, newRev string) (splitDiff *api.SplitDiff) {
	cleanPath := path.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}

	commitHash := oldRev
	out, err := gitCommitHash(oldRev, repo.Path)
	if err == nil {
		commitHash = out[:strings.Index(out, "\n")]
	}
	obj := commitHash + ":" + cleanPath

	// for now, assume we're not running this on

	// fetch the fileContents at revA
	oldSrc, err := gitCatBlob(obj, repo.Path)
	if err != nil {
		log.Printf("whats going on\n")
		log.Fatalf(err.Error())
		// return nil, err
	}

	// fetch the fileContents at revB
	commitHash = newRev
	out, err = gitCommitHash(newRev, repo.Path)
	if err == nil {
		commitHash = out[:strings.Index(out, "\n")]
	}
	obj = commitHash + ":" + cleanPath
	newSrc, err := gitCatBlob(obj, repo.Path)
	if err != nil {
		log.Printf("whats going on 2\n")
		log.Fatalf(err.Error())
		// return nil, err
	}

	// log.Printf("hello\n")

	// oldSrc := `
	// func (r *singleStringReplacer) Replace(s string) string {
	// var buf []byte
	// i, matched := 0, false
	// for {
	// 	match := r.finder.next(s[i:])
	// 	if match == -1 {
	// 		break
	// 	}
	// 	matched = true
	// 	buf = append(buf, s[i:i+match]...)
	// 	buf = append(buf, r.value...)
	// 	i += match + len(r.finder.pattern)
	// }
	// if !matched {
	// 	return s
	// }
	// buf = append(buf, s[i:]...)
	// return string(buf)
	// }`

	// newSrc := `
	// func (r *singleStringReplacer) Replace(s string) string {
	// var buf Builder
	// i, matched := 0, false
	// for {
	// 	match := r.finder.next(s[i:])
	// 	if match == -1 {
	// 		break
	// 	}
	// 	matched = true
	// 	buf.Grow(match + len(r.value))
	// 	buf.WriteString(s[i : i+match])
	// 	buf.WriteString(r.value)
	// 	i += match + len(r.finder.pattern)
	// }
	// if !matched {
	// 	return s
	// }
	// buf.WriteString(s[i:])
	// return buf.String()
	// }`

	// oldSrc := `
	// a := make([]string, n)`

	// newSrc := `
	// if n > len(s)+1 {
	// 	n = len(s) + 1
	// }
	// a := make([]string, n)`

	// contentA := `
	// `

	// contentB := `
	// func test(x, y string) {
	// 	if text[0] == '.' || isSpeakerNote(text) {
	// 	for ok && !lesserHeading(isHeading, text, prefix) {
	// }
	// `

	dmp := diffmatchpatch.New()

	// diffs := dmp.DiffCleanupSemanticLossless(dmp.DiffMain(oldSrc, newSrc, false))
	// oldSrcRunes, newSrcRunes, runesToLines := dmp.DiffLinesToRunes(oldSrc, newSrc)
	// diffs := dmp.DiffCharsToLines(dmp.DiffMainRunes(oldSrcRunes, newSrcRunes, false), runesToLines)

	diffs := dmp.DiffMain(oldSrc, newSrc, false)
	diffs = dmp.DiffCleanupSemantic(diffs)
	diffs = dmp.DiffCleanupEfficiency(diffs)

	fmt.Printf("there are %d diffs\n", len(diffs))
	fmt.Println(dmp.DiffPrettyText(diffs))

	// Things to keep in mind while generating a split diff
	// * The deletion pane is on left, insert pane is on right
	// 1. We need to be able to map diff text positions to positions in old & new (for line numbers)
	// 2. When showing deletions, we need to show a blank line in the insert pane
	// 3. When shoing inserts, we need to show a blank line in the deletion pane
	// 3. When showing
	// 4. Google cs, when clicking diff, automatically diffs the clicked rev against the prev
	// 5. Google cs blurs/blocks clicks on "diff" for the last revision
	// How do I map a source line in the code, to a line from the diff?
	// The reason that question comes up at all is that I need to match
	// the left and right panels of a split diff

	leftDiff := &api.SplitDiffHalf{LinesMap: make(map[uint32]uint32)}
	rightDiff := &api.SplitDiffHalf{LinesMap: make(map[uint32]uint32)}

	var oldSrcLno, newSrcLno uint32

	for _, diff := range diffs {
		fmt.Printf("diff_type=%s\n", diff.Type)
		dLines := strings.Split(diff.Text, string('\n')) // I think we're gaurenteed that this will lave len 1
		// fmt.Printf("numLines=%d\n", len(dLines))
		// fmt.Printf("diffText=%#v\n", diff.Text)
		// If there's no newline we can't say the diff spans any lines so we
		// subtract 1
		// dLineLen := len(dLines) - 1
		// dNewLine := dLineLen != 0

		// TODO: Any difftype can span n lines. And, any diffType can start on a previous line
		// So we'll probably need to split any diffTypes text, then append it to the proper place
		// Right now, everything is one giant row, which is where we started :doh
		switch diff.Type {
		// The problem now is that inserts and deletes aren't
		case diffmatchpatch.DiffDelete:
			// fmt.Printf("whole delete = %s. HasNewline=%t\n", diff.Text, strings.HasSuffix(diff.Text, "\n"))
			for idx, l := range dLines {
				if l == "" { // if newline, increment oldSrcLno
					// addBlankLine(leftDiff)
					oldSrcLno += 1
					// fmt.Printf("line is newline. incremented oldSrcLno to=%d\n", oldSrcLno)
					continue
				}

				// addDiff to oldSrcLno
				addDiff(leftDiff, l, diff.Type, oldSrcLno)

				// if this is the last element, don't add a newline
				if idx < len(dLines)-1 {
					oldSrcLno += 1
					// fmt.Printf("incremented oldSrcLno to=%d\n", oldSrcLno)
				}
			}
		case diffmatchpatch.DiffInsert:
			// add to RightLines

			// fmt.Printf("whole insert = %s. HasNewline=%t\n", diff.Text, strings.HasSuffix(diff.Text, "\n"))
			for idx, l := range dLines {
				if l == "" { // if newline, increment oldSrcLno
					// addBlankLine(leftDiff)
					newSrcLno += 1
					// fmt.Printf("line is newline. incremented newSrcLno to=%d\n", newSrcLno)
					continue
				}

				// fmt.Printf("adding %#v as insert.\n", l)
				addDiff(rightDiff, l, diff.Type, newSrcLno)

				if idx < len(dLines)-1 {
					newSrcLno += 1
					// fmt.Printf("incremented newSrcLno to=%d\n", newSrcLno)
				}
			}

		case diffmatchpatch.DiffEqual:
			// catch left up to right
			// fmt.Printf("original text is: %#v \n", diff.Text)

			// if we have a bunch of equal text, like many lines, reduce it to, say,
			// 5 context lines for now. The possibilities are:
			// context before any diff hunks. We want to trim context from top
			// context betweem hunks - trim from middle. Leave a few lines at top, a few at bottom, and collpase mid
			// context after everything else - trim from bottom

			// fmt.Printf("oldSrcLno=%d newSrcLno=%d\n", oldSrcLno, newSrcLno)
			// if oldSrcLno < newSrcLno && len(dLines) > 1 {
			// leftLine = 415
			// rightLine = 416
			// linesToCatch := newSrcLno - oldSrcLno
			// fmt.Printf("oldSrc needs to catch up by %d lines\n", linesToCatch)
			// for linesToCatch > 0 {
			// 	addBlankLine(leftDiff)
			// 	linesToCatch -= 1
			// }
			// }

			// if newSrcLno < oldSrcLno && len(dLines) > 1 {
			// 	linesToCatch := oldSrcLno - newSrcLno
			// 	for linesToCatch > 0 {
			// 		addBlankLine(rightDiff)
			// 		linesToCatch -= 1
			// 	}
			// }
			// for len(leftDiff.Lines) < len(rightDiff.Lines) {
			// 	addBlankLine(leftDiff)
			// }
			// catch right up to left
			// for len(rightDiff.Lines) < len(leftDiff.Lines) {
			// 	addBlankLine(rightDiff)
			// }
			for idx, l := range dLines {
				// fmt.Printf("adding %#v as equal. line_len=%d\n", l, len([]rune(l)))
				// fmt.Printf("adding: `%s` as equal. i=%d numLines=%d withNewline=%t\n", l, i, numLines, i != numLines)

				if l == "" { // if newline char
					addRealBlankLine(leftDiff, oldSrcLno)
					addRealBlankLine(rightDiff, oldSrcLno)
					// addBlankLine(leftDiff)
					// addBlankLine(rightDiff)
					oldSrcLno += 1
					newSrcLno += 1
					continue
				}

				addDiff(leftDiff, l, diff.Type, oldSrcLno)
				addDiff(rightDiff, l, diff.Type, newSrcLno)

				// don't increment the lastLine
				if idx < len(dLines)-1 {
					oldSrcLno += 1
					newSrcLno += 1
				}

			}
		default:
			log.Fatalf("unknown diff type encountered: %v\n", diff.Type)
		}
		fmt.Printf("\n")
	}

	fmt.Printf("finished generating diff\n")
	fmt.Printf("leftDiff: %+v\n", leftDiff)
	fmt.Printf("rightDiff: %+v\n", rightDiff)
	return &api.SplitDiff{
		LeftDiff:  leftDiff,
		RightDiff: rightDiff,
	}
	// var buff1 bytes.Buffer
	// var buff2 bytes.Buffer

}

func resolveLeftAndRightDiffs() {
	// given unbalanced left and right arrays, make them the same
	// length by inserting null lines in either the left or right arrays
	//
}

// TODO(xvandish): Would be cool to eventually diff arbitratry files across repos.
// Could be useful for comparing a file that initiated in a different repo

var refFormat = "%(HEAD)%00%(authordate:human)%00%(refname:short)"
var sortFormat = "authordate"

//panic if s is not a slice
func ReverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

func listAllBranches(repo config.RepoConfig) ([]api.GitBranch, error) {
	// git for-each-ref --format='%(HEAD) %(refname:short)' refs/heads
	cmd := exec.Command("git", "-C", repo.Path, "for-each-ref", "--format="+refFormat, "--sort="+sortFormat, "refs/heads")

	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, err
	}

	err = cmd.Start()

	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	branches := make([]api.GitBranch, 0)
	headIdx := -1
	idx := 0
	for scanner.Scan() {
		words := strings.Split(scanner.Text(), "\x00")
		isHead := words[0] == "*"
		branches = append(branches, api.GitBranch{Name: words[2], IsHead: isHead, LastActivityDate: words[1]})
		if isHead {
			headIdx = idx
		}
		idx += 1
	}

	// now, somehow, move teh headIdx from where it is to the end of the list
	if headIdx != len(branches)-1 {
		// need to modify branches here
		tmp := branches[headIdx]
		branches = append(branches[:headIdx], branches[headIdx+1:]...)
		branches = append(branches, tmp)
	}

	// git sorts for date by us, but in descending order
	// we want ascending, and for now rather than parsing the date ourselves,
	// we're just going to reverse the slice
	ReverseSlice(branches)

	// now, finally, add the HEAD/default branch to the top

	return branches, nil
}

func listAllTags(repo config.RepoConfig) ([]api.GitTag, error) {
	// git for-each-ref --format='%(HEAD) %(refname:short)' refs/tags
	cmd := exec.Command("git", "-C", repo.Path, "for-each-ref", "--format="+refFormat, "--sort="+sortFormat, "refs/tags")

	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, err
	}

	err = cmd.Start()

	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	tags := make([]api.GitTag, 0)
	for scanner.Scan() {
		words := strings.Split(scanner.Text(), "\x00")
		tags = append(tags, api.GitTag{Name: words[2], IsHead: words[0] == "*", LastActivityDate: words[1]})
	}

	ReverseSlice(tags)

	return tags, nil
}

type GitDiffLineType int

const (
	ContextLine GitDiffLineType = iota + 1
	AddLine
	DeleteLine
	HunkLine
)

type GitDiffLine struct {
	Text string
	Type GitDiffLineType
	// Line number in the original diff patch (before expanding it), or null if
	// it was added as part of a diff expansion action.
	OriginalLineNumber int
	OldLineNumber      int
	NewLineNumber      int
	NoTrailingNewline  bool
}

func (gl GitDiffLine) isIncudableLine() bool {
	return gl.Type == AddLine || gl.Type == DeleteLine
}

/** The content of the line, i.e., without the line type marker. */
func (gl GitDiffLine) content() string {
	return gl.Text[1:]
}

type GitDiffHunkHeader struct {
	OldStartLine int // The line in the old (or original) file where this diff hunk starts.
	OldLineCount int // The number of lines in the old (or original) file that this diff hunk covers
	NewStartLine int // The line in the new file where this diff hunk starts.
	NewLineCount int // The number of lines in the new file that this diff hunk covers.
}

func (h GitDiffHunkHeader) toString() string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStartLine, h.OldLineCount, h.NewStartLine, h.NewLineCount)
}

type DiffHunkExpansionType int

const (
	/** The hunk header cannot be expanded at all. */
	None DiffHunkExpansionType = iota + 1

	/**
	* The hunk header can be expanded up exclusively. Only the first hunk can be
	* expanded up exclusively.
	 */
	Up

	/**
	* The hunk header can be expanded down exclusively. Only the last hunk (if
	* it's the dummy hunk with only one line) can be expanded down exclusively.
	 */
	Down

	/** The hunk header can be expanded both up and down. */
	Both

	/**
	* The hunk header represents a short gap that, when expanded, will
	* result in merging this hunk and the hunk above.
	 */
	Short
)

type GitDiffHunk struct {
	// The details from the diff hunk header about the line start and patch length
	Header GitDiffHunkHeader
	// The contents - context and changes - of the diff section.
	Lines []GitDiffLine
	// The diff hunk's start position in the overall file diff.
	UnifiedDiffStart int
	// The diff hunk's end position in the overall file diff.
	UnifiedDiffEnd int
	ExpansionType  DiffHunkExpansionType
}

type GitDiffHeader struct {
	IsBinary bool
}

// Diff is also a GitDiff, but I don't want to modify it right now
// Same as IRawDiff in GithubDesktop
type GitDiff struct {
	/**
	 * The plain text contents of the diff header. This contains
	 * everything from the start of the diff up until the first
	 * hunk header starts. Note that this does not include a trailing
	 * newline.
	 */
	Header string
	/**
	 * The plain text contents of the diff. This contains everything
	 * after the diff header until the last character in the diff.
	 *
	 * Note that this does not include a trailing newline nor does
	 * it include diff 'no newline at end of file' comments. For
	 * no-newline information, consult the DiffLine noTrailingNewLine
	 * property.
	 */
	Contents string

	/**
	 * Each hunk in the diff with information about start, and end
	 * positions, lines and line statuses.
	 */
	Hunks []*GitDiffHunk

	/**
	* Whether or not the unified diff indicates that the contents
	* could not be diffed due to one of the versions being binary.
	 */
	IsBinary bool

	/** The largest line number in the diff */
	MaxLineNumber int

	/** Whether or not the diff has invisible bidi characters */
	HasHiddenBidiChars bool
}

/**
* Parse the diff header, meaning everything from the
* start of the diff output to the end of the line beginning
* with +++
*
* Example diff header:
*
*   diff --git a/app/src/lib/diff-parser.ts b/app/src/lib/diff-parser.ts
*   index e1d4871..3bd3ee0 100644
*   --- a/app/src/lib/diff-parser.ts
*   +++ b/app/src/lib/diff-parser.ts
*
* Returns an object with information extracted from the diff
* header (currently whether it's a binary patch) or null if
* the end of the diff was reached before the +++ line could be
* found (which is a valid state).
 */
func parseGitDiffHeader(input *bufio.Scanner) (*GitDiffHeader, error) {
	// TODO: not sure this really needs to do anything...
	for input.Scan() {
		line := input.Bytes()
		if bytes.HasPrefix(line, []byte("Binary files ")) && bytes.HasSuffix(line, []byte("differ")) {
			return &GitDiffHeader{IsBinary: true}, nil
		}

		if bytes.HasPrefix(line, []byte("+++")) {
			return &GitDiffHeader{IsBinary: false}, nil
		}
	}

	if err := input.Err(); err != nil {
		return nil, err
	}

	// if we never found the +++, it's not an error
	// (diff of empty file)
	return nil, nil
}

// https://en.wikipedia.org/wiki/Diff_utility
//
// @@ -l,s +l,s @@ optional section heading
//
// The hunk range information contains two hunk ranges. The range for the hunk of the original
// file is preceded by a minus symbol, and the range for the new file is preceded by a plus
// symbol. Each hunk range is of the format l,s where l is the starting line number and s is
// the number of lines the change hunk applies to for each respective file.
//
// In many versions of GNU diff, each range can omit the comma and trailing value s,
// in which case s defaults to 1
var diffHeaderRe = regexp.MustCompile("^@@ -(\\d+),?(\\d*) \\+(\\d+),?(\\d*) @@")

func numberFromGroup(input []byte, df int) int {
	var s int64
	// TODO: not right. Returning -27 instead of 7
	s, n := binary.Varint(input)
	if n != len(input) {
		return df
	}
	return int(s)
}

/**
 * Parses a hunk header or throws an error if the given line isn't
 * a well-formed hunk header.
 *
 * We currently only extract the line number information and
 * ignore any hunk headings.
 *
 * Example hunk header (text within ``):
 *
 * `@@ -84,10 +82,8 @@ export function parseRawDiff(lines: ReadonlyArray<string>): Diff {`
 *
 * Where everything after the last @@ is what's known as the hunk, or section, heading
 */
func parseGitDiffHunkHeader(headerLine []byte) (*GitDiffHunkHeader, error) {
	h := diffHeaderRe.FindSubmatch(headerLine)

	if h == nil {
		return nil, errors.New(fmt.Sprintf("Invalid patch string: %s\n", string(headerLine)))
	}

	fmt.Printf("h[1]=%s h[2]=%s h[3]=%s h[4]=%s \n", string(h[1]), string(h[2]), string(h[3]), string(h[4]))
	// If endLines are missing default to 1, see diffHeaderRe docs
	oldStartLine := numberFromGroup(h[1], 0)
	oldLineCount := numberFromGroup(h[2], 1)
	newStartLine := numberFromGroup(h[3], 0)
	newLineCount := numberFromGroup(h[4], 1)

	return &GitDiffHunkHeader{
		OldStartLine: oldStartLine,
		OldLineCount: oldLineCount,
		NewStartLine: newStartLine,
		NewLineCount: newLineCount,
	}, nil
}

func parseGitDiffHunk(input *bufio.Scanner) *GitDiffHunk {
	input.Scan()
	headerLine := input.Bytes()
	header, err := parseGitDiffHunkHeader(headerLine)

	if err != nil {
		fmt.Printf("err=%v\n", err)
	}

	fmt.Printf("gitDiffHunkHeader: %+v\n", header)

	lines := make([]GitDiffLine, 0)
	lines = append(lines, GitDiffLine{
		Text:               string(headerLine),
		Type:               HunkLine,
		OriginalLineNumber: 1,
		OldLineNumber:      0,
		NewLineNumber:      0,
		NoTrailingNewline:  false,
	})

	hunk := &GitDiffHunk{
		Lines:  lines,
		Header: *header,
	}
	// now,
	return hunk
}

/**
* Parse a well-formed unified diff into hunks and lines.
*
* @param text A unified diff produced by git diff, git log --patch
*             or any other git plumbing command that produces unified
*             diffs.
 */
// we're already doing this.. maybe I should just keep doing it my way
// we can improve my way, but I really don't like the way that others are doing it
// this function should work for 1..n diffs, that way we can use it for diffs of a single
// file or for an entire commit
func parseGitUnifiedDiff(input *bufio.Scanner) *GitDiff {
	fmt.Printf("hello from parseGitUnifiedDiff\n")

	diff := &GitDiff{}

	// parse the header
	header, err := parseGitDiffHeader(input)

	fmt.Printf("header: %v err:%v\n", header, err)
	if err != nil {
		return nil
	}

	if header.IsBinary {
		fmt.Printf("binary not handled rn\n")
		return nil
	}

	diff.IsBinary = header.IsBinary

	// then, parse all hunks until none left
	hunks := make([]*GitDiffHunk, 0)
	for {
		hunk := parseGitDiffHunk(input)
		fmt.Printf("hunk=%+v\n", hunk)
		hunks = append(hunks, hunk)
		break
	}

	diff.Hunks = hunks

	fmt.Printf("diff: %+v\n", diff)

	return nil
}

// this version of the function is going to just parse the output of git diff and attempt
// to put it in a data structure that makes sense as a split half
// it MAY:
//	1. use the patience algorithm instead of myers
// it PROBABLY WONT:
//  1. Attempt to add context that can be collapsed
func generateSplitDiffForFileV2(relativePath string, repo config.RepoConfig, oldRev string, newRev string, hideWhitespace bool) (*api.SplitDiff, error) {

	// TODO: we can rebuild this function so that we do a diff against
	// first parent when necessary, instead of having newRev be calculated
	// by someone before us
	args := []string{
		"-C",
		repo.Path,
		"diff",
		oldRev,
		newRev,
	}

	if hideWhitespace {
		args = append(args, "-w")
	}

	args = append(args, "-z", "--no-color", "--", relativePath)

	// git -C somePath diff oldHash newHash -z --no-color -- pathToFile
	cmd := exec.Command("git", args...)

	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)
	// scanner.Split(ScanGitShowEntry) // read null byte delimited

	parseGitUnifiedDiff(scanner)

	return nil, nil
}

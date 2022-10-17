package server

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	FilePath        string
	DirectoryTree   *api.TreeNode
}

type sourceFileContent struct {
	Content   string
	LineCount int
	Language  string
	Filename  string
	BlameData *BlameResult
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
	out, err := exec.Command("git", "-C", repoPath, "cat-file", "-t", obj).Output()
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
		return nil, err
	}
	// Null terminate our thing
	start = time.Now()
	out = append(out, byte(rune(0)))
	err = os.WriteFile("./tmp-log", out, 0644)
	if err != nil {
		return nil, err
	}

	matches := gitLogRegex.FindAllSubmatch(out, -1)

	simpleGitLog := SimpleGitLog{}
	simpleGitLog.Commits = make([]*Commit, len(matches))

	for i, match := range matches {
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

func buildFileData(relativePath string, repo config.RepoConfig, commit string) (*fileViewerContext, error) {
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
	if err != nil {
		return nil, err
	}
	if objectType == "tree" {
		treeEntries, err := gitListDir(obj, repo.Path)
		if err != nil {
			return nil, err
		}

		dirEntries := make([]directoryListEntry, len(treeEntries))
		var readmePath, readmeLang string
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
			readmePath = obj + parts[0]
			readmeLang = parts[2]
		}

		var readmeContent *sourceFileContent
		if readmePath != "" {
			if content, err := gitCatBlob(readmePath, repo.Path); err == nil {
				readmeContent = &sourceFileContent{
					Content:   content,
					LineCount: strings.Count(content, "\n"),
					Language:  extToLangMap["."+readmeLang],
				}
			}
		}

		sort.Sort(DirListingSort(dirEntries))
		dirContent = &directoryContent{
			Entries:       dirEntries,
			ReadmeContent: readmeContent,
		}
	} else if objectType == "blob" {
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
			Filename:  filename,
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

	return &fileViewerContext{
		PathSegments:    segments,
		Repo:            repo,
		Commit:          commit,
		CommitHash:      commitHash[:16],
		ShortCommitHash: commitHash[:8],
		DirContent:      dirContent,
		FileContent:     fileContent,
		ExternalDomain:  externalDomain,
		Permalink:       permalink,
		Headlink:        headlink,
		FilePath:        relativePath,
	}, nil
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

	out, err := cmd.CombinedOutput()

	if err != nil {
		log.Fatal(err)
	}

	lines := strings.Split(string(out), "\x00")
	rootDir := &api.TreeNode{Name: "root"}
	currDir := rootDir
	prevDepth := 0

	for i, line := range lines {
		fmt.Printf("line=%s\n", line)
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
		fmt.Printf("pathDepth=%d\n", pathDepth)

		// 1777b4d56ea1471f155fa21fbf8d2969dcc3ce9e     600       cmd/server/main.go
		// 60c6f7580d7e6651739c86865e3c012a04650e4d       -       creds (prevDepth == 2)
		for prevDepth > pathDepth {
			currDir = currDir.ParentDir
			prevDepth -= 1
		}

		fmt.Printf("appending %s to %s children\n", treeEntry.Name, currDir.Name)
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
			fmt.Printf("nesting to dir with name=%s\n", treeEntry.Name)
			treeEntry.ParentDir = currDir
			currDir = treeEntry
			prevDepth += 1
		}
	}

	fmt.Printf("%+v\n", rootDir)
	return rootDir
}

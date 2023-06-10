package fileviewer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
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

type FileViewerContext struct {
	PathSegments    []breadCrumbEntry
	Repo            config.RepoConfig
	RepoRev         string // the commit/rev the repo is being viewed at (branch, commit, tag, etc)
	HeadRev         string // the rev that HEAD points to. May be the same as RepoRev
	Commit          string
	CommitHash      string
	ShortCommitHash string
	DirContent      *directoryContent
	FileContent     *SourceFileContent
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
	Branches      []GitBranch
	Tags          []GitTag
	RepoConfig    config.RepoConfig

	// the url that maps from /delve to /experimental
	// while experimental points to the new fileviewer.
	// Still TBD whether we will override /delve or switch
	// to a different prefix
	MigrationUrl string
}

type SourceFileContent struct {
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
	ReadmeContent *SourceFileContent
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
func GitRevParseAbbrev(rev string, repoPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", rev).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GitGetLastRevToTouchPath(relativePath, repoPath, repoRev string) (string, error) {
	// clean
	cleanPath := path.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}
	out, err := exec.Command("git", "-C", repoPath, "rev-list", "-1", repoRev, "--", relativePath).Output()
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

func getFileUrl(repo, pathFromRoot, name, commitHash string, isDir bool, useViewUrl bool) string {
	var fileUrl string
	fullPath := filepath.Join(pathFromRoot, path.Clean(name))
	if useViewUrl {
		fileUrl = viewUrl(repo, fullPath, isDir)
		if isDir {
			fileUrl += "/"
		}
	} else {
		fileUrl = "/" + repo + "/+/" + commitHash + ":" + fullPath
	}
	return fileUrl
}

func migrationUrl(repo, path, rev string) string {
	return "/experimental/" + repo + "/+/" + rev + ":" + path
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
	repoFileRegex := regexp.MustCompile(fmt.Sprintf("((?i)readme)\\.?(%s)", repoRegexAlt))

	return repoFileRegex
}

func buildDirectoryListEntry(treeEntry gitTreeEntry, pathFromRoot, repoName, repoPath, commitHash string, useViewUrl bool) directoryListEntry {
	var fileUrl string
	var symlinkTarget string
	if treeEntry.Mode == "120000" {
		resolvedPath, err := gitCatBlob(treeEntry.ObjectId, repoPath)
		if err == nil {
			symlinkTarget = resolvedPath
		}
	} else {
		fileUrl = getFileUrl(repoName, pathFromRoot, treeEntry.ObjectName, commitHash, treeEntry.ObjectType == "tree", useViewUrl)
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

const (
	partsPerCommitBasic         = 10 // number of \x00-separated fields per commit
	partsPerCommitWithFileNames = 11 // number of \x00-separated fields per commit with names of modified files also returned

	// don't include refs (faster, should be used if refs are not needed)
	// outputs (with null sep between each field) commitHash authorName authorEmail authorDate committerName commiterEmail commiterDate raw body(unwrapped subject and body) ParentHashes
	logFormatWithoutRefs = "--format=format:%H%x00%aN%x00%aE%x00%at%x00%cN%x00%cE%x00%ct%x00%s%x00%b%x00%P%x00"
)

// The named capture groups are just for human readability
var gitLogRegex = regexp.MustCompile("(?ms)" + `commit\s(?P<commitHash>\w*)\s<(?P<shortHash>\w*)>\nauthor\s<(?P<authorName>[^>]*)>\s<(?P<authorEmail>[^>]*)>\nsubject\s(?P<commitSubject>[^\n]*)\ndate\s(?P<commitDate>[^\n]*)\nbody\s(?P<commitBody>[\s\S]*?)\x00`)

type GitBranch struct {
	Name             string
	IsHead           bool
	LastActivityDate string
}

type GitTag struct {
	Name             string
	IsHead           bool
	LastActivityDate string
}

// a commit signature. Either author or commiter
type Signature struct {
	Name  string
	Email string
	Date  time.Time
}

type CommitId string

func (c CommitId) Short() string {
	if len(c) >= 7 {
		return string(c)[:7]
	}
	return string(c)
}

type GitCommit struct {
	ID        CommitId
	Author    Signature
	Committer *Signature // pointer since its sometimes nil
	Subject   string
	Body      string
	Parents   []CommitId
	Files     []string // not sure if going to use, when log is run with --name-only, lists all files changed by commit
}

type GitLog struct {
	Commits       []*GitCommit
	MaybeLastPage bool
}

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
			Path: getFileUrl(repo.Name, parentPath, name, "", true, false),
		}
	}

	return segments
}

func parseTimeFromLogPart(part []byte) (time.Time, error) {
	t, err := strconv.ParseInt(string(part), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(t, 0).UTC(), nil
}

// reads the next commit from rawLog, and advances rawLog by all the data read
func parseNextCommitFromLog(rawLog []byte, partsPerCommit int) (commit *GitCommit, rest []byte, err error) {
	parts := bytes.SplitN(rawLog, []byte{'\x00'}, partsPerCommit+1)
	if len(parts) < partsPerCommit {
		return nil, nil, errors.New(fmt.Sprintf("invalid commit log entry: %q", parts))
	}

	// log outputs are newline separated, so all but the 1st commit ID part
	// has an erroneous leading newline
	parts[0] = bytes.TrimPrefix(parts[0], []byte{'\n'})
	commitId := CommitId(parts[0])

	authorTime, err := parseTimeFromLogPart(parts[3])
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("parsing git commit author time: %s", err))
	}
	committerTime, err := parseTimeFromLogPart(parts[6])
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("parsing git commit committer time: %s", err))
	}

	var parentCommits []CommitId
	if parentPart := parts[9]; len(parentPart) > 0 {
		parentIds := bytes.Split(parentPart, []byte{' '})
		parentCommits = make([]CommitId, len(parentIds))
		for i, id := range parentIds {
			parentCommits[i] = CommitId(id)
		}
	}

	// TODO: if the commit has file names parse them
	// write the pareCommitFileNames
	fileNames, nextCommit := parseCommitFileNames(partsPerCommit, parts)

	commit = &GitCommit{
		ID:        commitId,
		Author:    Signature{Name: string(parts[1]), Email: string(parts[2]), Date: authorTime},
		Committer: &Signature{Name: string(parts[4]), Email: string(parts[5]), Date: committerTime},
		Subject:   string(parts[7]),
		Body:      string(parts[8]),
		Parents:   parentCommits,
		Files:     fileNames,
	}

	// if there is more data to process, advance rawLog for the next read
	if len(parts) == partsPerCommit+1 {
		rest = parts[partsPerCommit]
		if string(nextCommit) != "" {
			// if filenames are included, the nextcommit was in the chunk read by parseCommitFileNames.
			// so re-add it
			rest = append(append(nextCommit, '\x00'), rest...)
		}
	}

	return commit, rest, nil
}

func parseCommitFileNames(partsPerCommit int, parts [][]byte) ([]string, []byte) {
	var fileNames []string
	var nextCommit []byte
	if partsPerCommit == partsPerCommitWithFileNames {
		parts[10] = bytes.TrimPrefix(parts[10], []byte{'\n'})
		fileNamesRaw := parts[10]
		fileNameParts := bytes.Split(fileNamesRaw, []byte{'\n'})
		for i, name := range fileNameParts {
			// The last item contains the files modified, some empty space, and the commit ID for the next commit. Drop
			// the empty space and the next commit ID (which will be processed in the next iteration).
			if string(name) == "" || i == len(fileNameParts)-1 {
				continue
			}
			fileNames = append(fileNames, string(name))
		}
		nextCommit = fileNameParts[len(fileNameParts)-1]
	}
	return fileNames, nextCommit
}

func parseCommitLogOutput(rawLog []byte, nameOnly bool) ([]*GitCommit, error) {
	partsPerCommit := partsPerCommitBasic
	if nameOnly {
		partsPerCommit = partsPerCommitWithFileNames
	}

	commits := make([]*GitCommit, 0)
	for len(rawLog) > 0 {
		var commit *GitCommit
		var err error
		commit, rawLog, err = parseNextCommitFromLog(rawLog, partsPerCommit)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

type CommitOptions struct {
	Range string // commit range (revspec, "A..HEAD")

	N     uint // limit the number of commits to `n` (0 is no limit)
	SkipN uint // skip `n` commits at beginning. Used for pagination reqs

	// MessageQuery string // include only commits whose commit message contains this substring

	// Author string // include only commits whose author matches this
	// After  string // include only commits after this date
	// Before string // include only commits before this date

	// Reverse   bool // Whether or not commits should be given in reverse order (optional)
	// DateOrder bool // Whether or not commits should be sorted by date (optional)

	Path   string // only commits modifying the given path are selected
	Follow bool   // follow the history of the path beyond renames (single path only)

	// When true we opt out of attempting to fetch missing revisions
	NoEnsureRevision bool

	// When true return the names of the files changed in the commit
	// This is a frustrating name, --name-only doesn't exclude the rest of the things
	// you asked for from being included
	NameOnly bool
}

func ensureSafeSpecArg(spec string) error {
	if strings.HasPrefix(spec, "-") {
		return errors.New(fmt.Sprintf("invalid git revision spec %s (begins with '-')", spec))
	}
	return nil
}

func (opts CommitOptions) genLogArgs(initialArgs []string) (args []string, err error) {
	if err := ensureSafeSpecArg(opts.Range); err != nil {
		return nil, err
	}

	args = initialArgs

	// we currently always set N to 1000 on the server
	if opts.N != 0 {
		args = append(args, "-n", strconv.FormatUint(uint64(opts.N), 10))
	}

	if opts.SkipN != 0 {
		args = append(args, "--skip", strconv.FormatUint(uint64(opts.SkipN), 10))
	}

	// TODO: the rest of the filtering that we don't do
	if opts.Range != "" {
		args = append(args, opts.Range)
	}
	// Such a dumb name
	if opts.NameOnly {
		args = append(args, "--name-only")
	}
	if opts.Follow {
		args = append(args, "--follow")
	}
	if opts.Path != "" {
		args = append(args, "--", opts.Path)
	}

	return args, nil
}

func BuildGitLog(logArgs CommitOptions, repoPath string) (*GitLog, error) {
	args, err := logArgs.genLogArgs([]string{"-C", repoPath, "log", logFormatWithoutRefs})
	if err != nil {
		return nil, err
	}

	start := time.Now()
	cmd := exec.Command("git", args...)
	fmt.Printf("Commits cmd=%s\n", cmd.String())

	out, err := cmd.Output()
	fmt.Printf("took %s to get git log\n", time.Since(start))
	if err != nil {
		fmt.Printf("err=%s\n", err.Error())
		return nil, err
	}

	start = time.Now()
	commits, err := parseCommitLogOutput(out, logArgs.NameOnly)
	if err != nil {
		return nil, err
	}

	return &GitLog{
		Commits:       commits,
		MaybeLastPage: len(commits) < int(logArgs.N),
	}, nil
}

// We should add a bound for this - make it max at 3 seconds (use project-vi as reference)
func BuildSimpleGitLogData(relativePath string, firstParent string, repo config.RepoConfig) (*SimpleGitLog, error) {
	cleanPath := path.Clean(relativePath)
	start := time.Now()
	cmd := exec.Command("git", "-C", repo.Path, "log", "-n", "1000", "-z", "--no-abbrev", "--pretty="+customGitLogFormat, firstParent, "--", cleanPath)
	fmt.Printf("BuildSimpleGitLogData cmd=%s", cmd.String())

	out, err := cmd.Output()
	fmt.Printf("took %s to get git log\n", time.Since(start))
	if err != nil {
		fmt.Printf("err=%s\n", err.Error())
		return nil, err
	}

	// Null terminate our thing
	start = time.Now()
	// out = append(out, byte(rune(0)))
	// err = os.WriteFile("./tmp-log", out, 0644)
	// if err != nil {
	// 	fmt.Printf("err=%s\n", err.Error())
	// 	return nil, err
	// }

	matches := gitLogRegex.FindAllSubmatch(out, -1)

	simpleGitLog := SimpleGitLog{}
	simpleGitLog.Commits = make([]*Commit, len(matches))
	// fmt.Printf("git log out=%s\n", out)
	// fmt.Printf("git log matches=%+v\n", matches)

	for i, match := range matches {
		if len(match) != 8 {
			fmt.Printf("GIT_LOG_ERROR: match len < 8: %+v\n", match)
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
	if len(simpleGitLog.Commits) > 0 {
		simpleGitLog.NextParent = simpleGitLog.Commits[len(simpleGitLog.Commits)-1].Hash
	}
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
func GitShowCommit(repo config.RepoConfig, commit string) (*GitShow, error) {
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

func GitBlameBlob(relativePath string, repo config.RepoConfig, commit string) (*BlameResult, error) {
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

// Used to support zoekt "preview file" actions.
// Since we don't want to maintain repoConfig for zoekt
// repos, we tell this function explicitly where the repo
// is and what its name is
// Only supports files for now.
func BuildFileDataForZoektFilePreview(relativePath, repoPath, repoName, commit string) (*FileViewerContext, error) {
	commitHash := commit
	out, err := gitCommitHash(commit, repoPath)
	if err == nil {
		commitHash = out[:strings.Index(out, "\n")]
	}
	cleanPath := path.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}
	obj := commitHash + ":" + cleanPath

	var fileContent *SourceFileContent
	var dirContent *directoryContent

	objectType, err := gitObjectType(obj, repoPath)

	if err != nil {
		fmt.Printf("error getting object type: %v\n", err)
		return nil, err
	}
	if objectType == "tree" {
		fmt.Printf("objectType is tree\n")
		treeEntries, err := gitListDir(obj, repoPath)
		if err != nil {
			fmt.Printf("err=%v\n", err)
			return nil, err
		}

		dirEntries := make([]directoryListEntry, len(treeEntries))
		var readmePath, readmeLang, readmeName string
		for i, treeEntry := range treeEntries {
			dirEntries[i] = buildDirectoryListEntry(treeEntry, cleanPath, repoName, repoPath, commitHash, false)

			// special case, for README or readme without an extension
			if strings.ToLower(dirEntries[i].Name) == "readme" {
				readmeName = dirEntries[i].Name
				readmePath = obj + dirEntries[i].Name
				readmeLang = "md"
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

		var readmeContent *SourceFileContent
		if readmePath != "" {
			if content, err := gitCatBlob(readmePath, repoPath); err == nil {
				readmeContent = &SourceFileContent{
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
		content, err := gitCatBlob(obj, repoPath)
		if err != nil {
			return nil, err
		}
		filename := filepath.Base(cleanPath)
		language := filenameToLangMap[filename]
		if language == "" {
			language = extToLangMap[filepath.Ext(cleanPath)]
		}
		fileContent = &SourceFileContent{
			Content: content,
			// LineCount: strings.Count(string(content), "\n"),
			LineCount: 0,
			Language:  language,
			FileName:  filename,
			FilePath:  relativePath,
		}
	}

	return &FileViewerContext{
		FileContent: fileContent,
		DirContent:  dirContent,
	}, nil
}

func BuildFileData(relativePath string, repo config.RepoConfig, commit string) (*FileViewerContext, error) {
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

	var fileContent *SourceFileContent
	var dirContent *directoryContent

	objectType, err := gitObjectType(obj, repo.Path)

	// if there is an error here, most likely this file does not exist at obj
	// we still want the fileviewer to load, and we want to display a message like
	// "The file does not exist at the commit"
	if err != nil {
		fmt.Printf("error getting object type: %v\n", err)
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
			dirEntries[i] = buildDirectoryListEntry(treeEntry, cleanPath, repo.Name, repo.Path, commitHash, true)
			// Git supports case sensitive files, so README.md & readme.md in the same tree is possible
			// so in this case we just grab the first matching file
			if readmePath != "" {
				continue
			}

			// special case, for README or readme without an extension
			if strings.ToLower(dirEntries[i].Name) == "readme" {
				readmeName = dirEntries[i].Name
				readmePath = obj + dirEntries[i].Name
				readmeLang = "md"
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

		var readmeContent *SourceFileContent
		if readmePath != "" {
			fmt.Printf("readmePath != empty\n")
			if content, err := gitCatBlob(readmePath, repo.Path); err == nil {
				readmeContent = &SourceFileContent{
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
		fileContent = &SourceFileContent{
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
			Path: getFileUrl(repo.Name, parentPath, name, "", true, false),
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

	normalizedName, normalizedPath := getFileNameAndPathFromContent(fileContent, dirContent)
	return &FileViewerContext{
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
		MigrationUrl:    migrationUrl(repo.Name, cleanPath, commit),
	}, nil
}

/*
 * Gets the name from either FileContent or DirectoryContent.ReadmeContent, depending
 * on which is not nil
 */
func getFileNameAndPathFromContent(fc *SourceFileContent, dc *directoryContent) (string, string) {
	if fc != nil {
		return fc.FileName, fc.FilePath
	} else if dc.ReadmeContent != nil {
		return dc.ReadmeContent.FileName, dc.ReadmeContent.FilePath
	}

	return "", ""
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

func buildDirectoryTree(out []byte) (*api.TreeNode, error) {
	lines := strings.Split(string(out), "\x00")
	rootDir := &api.TreeNode{Name: "root"}
	currDir := rootDir
	prevDepth := 0
	var err error

	for i, line := range lines {
		// fmt.Printf("line=%s\n", line)
		if i == len(lines)-1 {
			// last entry is empty
			continue
		}
		tabPos := strings.IndexByte(line, '\t')
		if tabPos == -1 {
			return nil, errors.New(fmt.Sprintf("invalid `git ls-tree` output: %q", out))
		}

		info := strings.SplitN(line[:tabPos], " ", 4)
		name := line[tabPos+1:]

		if len(info) != 4 {
			return nil, errors.New(fmt.Sprintf("invalid `git ls-tree` output: %q", out))
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
				return nil, errors.New(fmt.Sprintf("invalid `git ls-tree` size output: %q (error: %s)", sizeStr, err))
			}
		}

		modeVal, err := strconv.ParseInt(info[0], 8, 32)
		if err != nil {
			return nil, err
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

	return rootDir, nil
}

func GetLsTreeOutput(relativePath string, repo, commit string) ([]byte, error) {
	defer timeTrack(time.Now(), "getLSTree")
	cmd := exec.Command("git", "-C", repo, "ls-tree",
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
		return nil, err
	}

	return out, err

}

// At a given commit, build the directory tree
// The frontend will have to be responsible for traversing it and finding/opening the current
func BuildDirectoryTree(relativePath string, repo, commit string) (*api.TreeNode, error) {
	// cleanPath := path.Clean(relativePath)
	// to start out, we always compute the tree for the root.
	defer timeTrack(time.Now(), "buildDirectoryTree")
	out, err := GetLsTreeOutput(relativePath, repo, commit)
	if err != nil {
		return nil, err
	}

	return buildDirectoryTree(out)
}

// TODO(xvandish): Would be cool to eventually diff arbitratry files across repos.
// Could be useful for comparing a file that initiated in a different repo

var refFormat = "%(HEAD)%00%(authordate:human)%00%(refname:short)"
var sortFormat = "authordate"

// panic if s is not a slice
func ReverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

func ListAllBranches(repo config.RepoConfig) ([]GitBranch, error) {
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

	branches := make([]GitBranch, 0)
	headIdx := -1
	idx := 0
	for scanner.Scan() {
		words := strings.Split(scanner.Text(), "\x00")
		isHead := words[0] == "*"
		branches = append(branches, GitBranch{Name: words[2], IsHead: isHead, LastActivityDate: words[1]})
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

func parseGitListTagsOutput(input *bufio.Scanner) []GitTag {
	tags := make([]GitTag, 0)
	for input.Scan() {
		words := bytes.SplitN(input.Bytes(), []byte("\x00"), 3)

		tags = append(tags, GitTag{Name: string(words[2]), IsHead: bytes.Equal(words[0], []byte("*")), LastActivityDate: string(words[1])})
	}

	ReverseSlice(tags)
	return tags
}

func ListAllTags(repo config.RepoConfig) ([]GitTag, error) {
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

	tags := parseGitListTagsOutput(scanner)
	return tags, nil
}

// TODO: rename these to make it clear these belong to the hunks
type GitDiffLineType int

const (
	ContextLine GitDiffLineType = iota + 1
	AddLine
	DeleteLine
	HunkLine
	NoTrailingNewlineLine // used to indicate that the previous line has no trailing newline
	UnknownLine
	ModifiedLine
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
	// TODO: not right. Returning -27 instead of 7
	// while byte -> string -> int may seem inneficient, it takes
	// <100ns normally
	defer timeTrack(time.Now(), "numberFromGroup")
	intVal, err := strconv.Atoi(string(input))
	if err != nil {
		intVal = df
	}
	return intVal
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

	fmt.Printf("oldStartLine=%d oldLineCount=%d newStartLine=%d newLineCount=%d\n", oldStartLine, oldLineCount, newStartLine, newLineCount)

	return &GitDiffHunkHeader{
		OldStartLine: oldStartLine,
		OldLineCount: oldLineCount,
		NewStartLine: newStartLine,
		NewLineCount: newLineCount,
	}, nil
}

// const DiffPrefixAdd = '+' as const
// const DiffPrefixDelete = '-' as const
// const DiffPrefixContext = ' ' as const
// const DiffPrefixNoNewline = '\\' as const

// type DiffLinePrefix =
//   | typeof DiffPrefixAdd
//   | typeof DiffPrefixDelete
//   | typeof DiffPrefixContext
//   | typeof DiffPrefixNoNewline
// const DiffLinePrefixChars: Set<DiffLinePrefix> = new Set([
//   DiffPrefixAdd,
//   DiffPrefixDelete,
//   DiffPrefixContext,
//   DiffPrefixNoNewline,
// ])

// linePrefixes is the set of all characters a valid line in a diff
// hunk can start with. '\' can appear in diffs when no newline is
// present at the end of a file.
// See: 'http://www.gnu.org/software/diffutils/manual/diffutils.html#Incomplete-Lines'
var linePrefixes = []byte{' ', '-', '+', '\\'}

// linePrefix returns true if 'c' is in 'linePrefixes'.
func isValidDiffLinePrefix(c byte) bool {
	for _, p := range linePrefixes {
		if p == c {
			return true
		}
	}
	return false
}

// type DiffLinePrefix []byte

var (
	DiffPrefixAdd       = []byte("+")
	DiffPrefixDelete    = []byte("-")
	DiffPrefixContext   = []byte(" ")
	DiffPrefixNoNewline = []byte("\\")
	DiffPrefixUnknown   = []byte("NANA")
)

func getDiffLineType(line []byte) GitDiffLineType {
	if bytes.HasPrefix(line, DiffPrefixAdd) {
		return AddLine
	}
	if bytes.HasPrefix(line, DiffPrefixDelete) {
		return DeleteLine
	}
	if bytes.HasPrefix(line, DiffPrefixContext) {
		return ContextLine
	}
	if bytes.HasPrefix(line, DiffPrefixNoNewline) {
		return NoTrailingNewlineLine
	}

	return UnknownLine
}

// TODO: this only works to parse a single hunk.
// why? because the loop that scans hunk lines will bail out when it
// encounters a @@ line that signifies the start of the next hunk
// so we return, call parseGitDiffHunk again - but! we call input.scan()
// right away, which forwards us past the hunk header, which means that we
// fail out immediately.

// options - check if the current line is a header. If not, advance and re-check
// parseGitDiffHunk can return whether to advance or not on the next

type HunkScanner struct {
	input          *bufio.Scanner
	nextHunkHeader []byte
}

func parseGitDiffHunk(hs *HunkScanner) *GitDiffHunk {
	fmt.Printf("in parseGitDiffHunk\n")
	var headerLine []byte

	// if we encountered a hunk header the last time we were processing,
	// use it, then empty it out
	if hs.nextHunkHeader != nil {
		fmt.Printf("hs.nextHunkHeader != nil. hs.nextHunkHeader=%s\n", string(hs.nextHunkHeader))
		headerLine = hs.nextHunkHeader
		hs.nextHunkHeader = nil
		fmt.Printf("headerLineInner=%s\n", string(headerLine))
	} else {
		fmt.Printf("scanning\n")
		hs.input.Scan()
		headerLine = hs.input.Bytes()
	}

	fmt.Printf("headerLine=%s\n", string(headerLine))

	// if nothing left to process, exit
	if len(headerLine) == 0 {
		return nil
	}

	header, err := parseGitDiffHunkHeader(headerLine)

	fmt.Printf("header=%s\n", header.toString())

	if err != nil {
		fmt.Printf("err=%v\n", err)
		return nil
	}

	lines := make([]GitDiffLine, 0)
	lines = append(lines, GitDiffLine{
		Text:               string(headerLine),
		Type:               HunkLine,
		OriginalLineNumber: 1,
		OldLineNumber:      0,
		NewLineNumber:      0,
		NoTrailingNewline:  false,
	})

	fmt.Printf("gitDiffHunkLine: %s\n", lines[0].Text)

	hunk := &GitDiffHunk{
		Lines:  lines,
		Header: *header,
	}

	rollingDiffBeforeCounter := header.OldStartLine
	rollingDiffAfterCounter := header.NewStartLine

	// now, parse the
	for hs.input.Scan() {
		line := hs.input.Bytes()

		lineType := getDiffLineType(line)
		if lineType == UnknownLine {
			if diffHeaderRe.Match(line) {
				fmt.Printf("found the next hunk header, storing it. header=%s\n", string(line))
				hs.nextHunkHeader = line
			} else {
				fmt.Printf("line=%s has invalid prefix:%s\n", string(line), string(line[0]))
			}
			break
		}

		// A marker indicating that the last line in the original or the new file
		// is missing a trailing newline. In other words, the presence of this marker
		// means that the new and/or original file lacks a trailing newline.
		//
		// When we find it we have to look up the previous line and set the
		// noTrailingNewLine flag
		if lineType == NoTrailingNewlineLine {
			if len(line) < 12 {
				fmt.Printf("Expected no-newline-marker to be 12bytes long")
				break
			}
			// tell the previous line that there is no trailing newline
			lines[len(lines)-1].NoTrailingNewline = true
			continue
		}

		// TODO: add the freaking line numbers
		// DOH!!
		var diffLine *GitDiffLine
		if lineType == AddLine {
			diffLine = &GitDiffLine{
				Text:          string(line),
				Type:          AddLine,
				NewLineNumber: rollingDiffAfterCounter,
			}
			rollingDiffAfterCounter += 1
		} else if lineType == DeleteLine {
			diffLine = &GitDiffLine{
				Text:          string(line),
				Type:          DeleteLine,
				OldLineNumber: rollingDiffBeforeCounter,
			}
			rollingDiffBeforeCounter += 1
		} else if lineType == ContextLine {
			diffLine = &GitDiffLine{
				Text:          string(line),
				Type:          ContextLine,
				OldLineNumber: rollingDiffBeforeCounter,
				NewLineNumber: rollingDiffAfterCounter,
			}
			rollingDiffBeforeCounter += 1
			rollingDiffAfterCounter += 1
		}

		// append this new line to the hunk
		hunk.Lines = append(hunk.Lines, *diffLine)

	}

	if len(hunk.Lines) == 1 {
		fmt.Printf("error. malformed hunk\n")
	}

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
	// TODO: return the text content of the header
	header, err := parseGitDiffHeader(input)

	fmt.Printf("header: %v err:%v\n", header, err)
	if err != nil || header == nil {
		return nil
	}

	if header.IsBinary {
		fmt.Printf("binary not handled rn\n")
		return nil
	}

	// todo: Everytime we call input.Scan(), append to a buffer
	// so we have the entirety of the diff text in a single buffer
	// we can attatch to the diff. If we're never going to use it, no
	// point going to the trouble though
	diff.IsBinary = header.IsBinary // always false but eh

	// then, parse all hunks until none left
	hunkScanner := &HunkScanner{
		input: input,
	}
	hunks := make([]*GitDiffHunk, 0)
	for {
		hunk := parseGitDiffHunk(hunkScanner)
		if hunk == nil {
			break
		}
		// fmt.Printf("hunk=%+v\n", hunk)
		hunks = append(hunks, hunk)
	}

	diff.Hunks = hunks

	// fmt.Printf("diff: %+v\n", diff)

	// for debugging, loop through every hunk and line and print line numbers
	for i, hunk := range diff.Hunks {
		fmt.Printf("hunk=%d has %d lines\n", i, len(hunk.Lines))
		// 	for _, line := range hunk.Lines {
		// fmt.Printf("newLineNumber=%d oldLineNumber=%d originalLineNumber=%d\n", line.NewLineNumber, line.OriginalLineNumber, line.OriginalLineNumber)
		// }
	}

	return diff
}

// this version of the function is going to just parse the output of git diff and attempt
// to put it in a data structure that makes sense as a split half
// it MAY:
//  1. use the patience algorithm instead of myers
//
// it PROBABLY WONT:
//  1. Attempt to add context that can be collapsed
func GetDiffBetweenTwoCommits(relativePath string, repo config.RepoConfig, oldRev string, newRev string, hideWhitespace bool) (*GitDiff, error) {

	// TODO: decide whether its worth it to bounce early if oldRev == newRev or to let diff check

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

	fmt.Printf("diff-command=%s\n", cmd.String())

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)
	// scanner.Split(ScanGitShowEntry) // read null byte delimited

	diff := parseGitUnifiedDiff(scanner)

	return diff, nil
}

// the following is all presentation code - used to generate the rows for a split
// diff. IDiffRow isDiffRow is hacky way around Go's lack on union types
// It's also, before I forget, mostly ripped from GitHub Desktops code

type IDiffRow interface {
	isDiffRow()
}

// argh, lets get this working and then see if its worth it
type IDiffRowData struct {
	/**
	 * The actual contents of the diff line.
	 */
	Content string

	/**
	 * The line number on the source file.
	 */
	LineNumber int

	/**
	 * The line number on the original diff (without expansion).
	 * This is used for discarding lines and for partial committing lines.
	 */
	DiffLineNumber int

	/**
	 * Flag to display that this diff line lacks a new line.
	 * This is used to display when a newline is
	 * added or removed to the last line of a file.
	 */
	NoNewLineIndicator bool

	/**
	 * Whether the diff line has been selected for partial committing.
	 */
	// IsSelected: boolean

	/**
	 * Array of tokens to do syntax highlighting on the diff line.
	 */
	// readonly tokens: ReadonlyArray<ILineTokens>
}

type IDiffRowAdded struct {
	Type GitDiffLineType
	Data IDiffRowData
	/**
	 * The start line of the hunk where this line belongs in the diff.
	 *
	 * In this context, a hunk is not exactly equivalent to a diff hunk, but
	 * instead marks a group of consecutive added/deleted lines (see hoveredHunk
	 * comment in the `<SideBySide />` component).
	 */
	HunkStartLine int
	Row           IDiffRow
}

func (IDiffRowAdded) isDiffRow() {}

type IDiffRowDeleted struct {
	Type GitDiffLineType
	Data IDiffRowData

	/**
	 * The start line of the hunk where this line belongs in the diff.
	 *
	 * In this context, a hunk is not exactly equivalent to a diff hunk, but
	 * instead marks a group of consecutive added/deleted lines (see hoveredHunk
	 * comment in the `<SideBySide />` component).
	 */
	HunkStartLine int
	Row           IDiffRow
}

func (IDiffRowDeleted) isDiffRow() {}

type IDiffRowModified struct {
	Type       GitDiffLineType
	BeforeData IDiffRowData
	AfterData  IDiffRowData

	/**
	 * The start line of the hunk where this line belongs in the diff.
	 *
	 * In this context, a hunk is not exactly equivalent to a diff hunk, but
	 * instead marks a group of consecutive added/deleted lines (see hoveredHunk
	 * comment in the `<SideBySide />` component).
	 */
	HunkStartLine int
	Row           IDiffRow
}

func (IDiffRowModified) isDiffRow() {}

type IDiffRowContext struct {
	Type GitDiffLineType
	Data IDiffRowData
	/**
	 * The actual contents of the contextual line.
	 */
	Content string

	/**
	 * The line number of this row in the previous state source file.
	 */
	BeforeLineNumber int

	/**
	 * The line number of this row in the next state source file.
	 */
	AfterLineNumber int

	/**
	 * Tokens to use to syntax highlight the contents of the before version of the line.
	 */
	// readonly beforeTokens: ReadonlyArray<ILineTokens>

	/**
	 * Tokens to use to syntax highlight the contents of the after version of the line.
	 */
	// readonly afterTokens: ReadonlyArray<ILineTokens>
	Row IDiffRow
}

func (IDiffRowContext) isDiffRow() {}

/**
 * IDiffRowContext represents a row that contains the header
 * of a diff hunk.
 */
type IDiffRowHunk struct {
	Type GitDiffLineType
	/**
	 * The actual contents of the line.
	 */
	Content string

	/** How the hunk can be expanded. */
	ExpansionType DiffHunkExpansionType

	/** Index of the hunk in the diff. */
	HunkIndex int

	Row IDiffRow
}

func (IDiffRowHunk) isDiffRow() {}

// type DiffRow = IDiffRowAdded | IDiffRowHunk

func (gd *GitDiff) GetDiffRowsSplit() []IDiffRow {
	// iterate through each hunk
	rows := make([]IDiffRow, 0)

	for i, hunk := range gd.Hunks {
		rows = append(rows, getDiffRowsFromHunk(hunk, i)...)
	}

	return rows
}

// TODO: I'd like to find a way to get ALL context rows for a file and ship
// them to the browser. I really, really dislike expanding up/down slowly like
// GitHub allows.
func getDiffRowsFromHunk(hunk *GitDiffHunk, hunkIndex int) []IDiffRow {
	rows := make([]IDiffRow, 0)
	/**
	 * Array containing multiple consecutive added/deleted lines. This
	 * is used to be able to merge them into modified rows.
	 */
	// let modifiedLines = new Array<ModifiedLine>()
	modifiedLines := make([]GitDiffLine, 0)

	for _, line := range hunk.Lines {
		//     const diffLineNumber = hunk.unifiedDiffStart + num
		if line.Type == AddLine || line.Type == DeleteLine {
			modifiedLines = append(modifiedLines, line)
			continue
		}

		// If the current line is not added/deleted and we have any added/deleted
		// line stored, we need to process them.
		if len(modifiedLines) > 0 {
			rows = append(rows, getModifiedDiffRows(modifiedLines)...)
			modifiedLines = nil // clear out the slice
		}

		if line.Type == HunkLine {
			rows = append(rows, IDiffRowHunk{
				Type:    HunkLine,
				Content: line.Text,
				// TODO: ExpansionType:
				HunkIndex: hunkIndex,
			})
			continue
		}

		if line.Type == ContextLine {
			rows = append(rows, IDiffRowContext{
				Type:             ContextLine,
				Content:          line.content(),
				BeforeLineNumber: line.OldLineNumber,
				AfterLineNumber:  line.NewLineNumber,
			})
			continue
		}

		// TODO: assert here if we ever have a different type of row, which
		// should be impossible

		// if (modifiedLines.length > 0) {
		// for (const row of getModifiedRows(modifiedLines, showSideBySideDiff)) {
		// 	rows.push(row)
		// }
		// modifiedLines = []
		// }

	}

	// Do one more pass to process the remaining list of modified lines.
	// This may happen, for example, if a diff contains only deleted/added lines,
	// so the prior for-loop only ever adds to modifiedLines
	if len(modifiedLines) > 0 {
		rows = append(rows, getModifiedDiffRows(modifiedLines)...)
	}

	return rows
}

// so, what we're really getting here are the rows that will eventually be rendered
// AND, in the case that two rows are "balanced" for rendering, that is, there is context either before/after them, and there is a matching delete for an insert, they are grouped together

// credit for the IDiffRow idea - https://eli.thegreenplace.net/2018/go-and-algebraic-data-types/
// and the go library here -https://cs.opensource.google/go/go/+/master:src/cmd/compile/internal/ir/stmt.go;l=41;bpv=0;bpt=0
func getModifiedDiffRows(addedOrDeletedLines []GitDiffLine) []IDiffRow {
	rows := make([]IDiffRow, 0)
	if len(addedOrDeletedLines) == 0 {
		return rows
	}

	// TODO: hunkStartline
	addedLines := make([]GitDiffLine, 0)
	deletedLines := make([]GitDiffLine, 0)

	// split out into added or deleted lines
	// TODO: this is needless re-processing, can probably modify getDiffRows to send in
	// split arrays
	for _, line := range addedOrDeletedLines {
		if line.Type == AddLine {
			addedLines = append(addedLines, line)
		} else if line.Type == DeleteLine {
			deletedLines = append(deletedLines, line)
		}
	}

	// eventually, get intraline diff if necessary

	modifiedRowIdx := 0

	for modifiedRowIdx < len(addedLines) && modifiedRowIdx < len(deletedLines) {
		addedLine := addedLines[modifiedRowIdx]
		deletedLine := deletedLines[modifiedRowIdx]

		fmt.Printf("deletedLineNum=%d newLineNume=%d\n", deletedLine.OriginalLineNumber, addedLine.NewLineNumber)
		rows = append(rows, IDiffRowModified{
			Type: ModifiedLine,
			BeforeData: IDiffRowData{
				Content:            deletedLine.content(),
				DiffLineNumber:     deletedLine.OriginalLineNumber,
				NoNewLineIndicator: deletedLine.NoTrailingNewline,
				LineNumber:         deletedLine.OldLineNumber,
			},
			AfterData: IDiffRowData{
				Content:            addedLine.content(),
				DiffLineNumber:     addedLine.OriginalLineNumber,
				NoNewLineIndicator: addedLine.NoTrailingNewline,
				LineNumber:         addedLine.NewLineNumber,
			},
			// TODO: HunkStartLine
		})
		modifiedRowIdx++
	}

	// process remaining delete lines
	for i := modifiedRowIdx; i < len(deletedLines); i++ {
		dl := deletedLines[i]
		rows = append(rows, IDiffRowDeleted{
			Type: DeleteLine,
			Data: IDiffRowData{
				Content:            dl.content(),
				DiffLineNumber:     dl.OriginalLineNumber,
				NoNewLineIndicator: dl.NoTrailingNewline,
				LineNumber:         dl.OldLineNumber,
			},
			// TODO: HunkStartLine
		})
	}

	// process remaining insert lines
	for i := modifiedRowIdx; i < len(addedLines); i++ {
		al := addedLines[i]
		rows = append(rows, IDiffRowAdded{
			Type: AddLine,
			Data: IDiffRowData{
				Content:            al.content(),
				DiffLineNumber:     al.OriginalLineNumber,
				NoNewLineIndicator: al.NoTrailingNewline,
				LineNumber:         al.NewLineNumber,
			},
			// TODO: HunkStartLine
		})
	}

	return rows
}

package server

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
	PathSegments   []breadCrumbEntry
	Repo           config.RepoConfig
	Commit         string
	DirContent     *directoryContent
	FileContent    *sourceFileContent
	ExternalDomain string
	Permalink      string
	Headlink       string
}

type sourceFileContent struct {
	Content   string
	LineCount int
	Language  string
	Filename  string
}

type directoryContent struct {
	Entries       []directoryListEntry
	ReadmeContent *sourceFileContent
}

type DirListingSort []directoryListEntry

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

func viewUrl(repo string, path string) string {
	return "/view/" + repo + "/" + path
}

func getFileUrl(repo string, pathFromRoot string, name string, isDir bool) string {
	fileUrl := viewUrl(repo, filepath.Join(pathFromRoot, path.Clean(name)))
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
var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ai%nbody %b"

// The named capture groups are just for human readability
var gitLogRegex = regexp.MustCompile("(?ms)" + `commit\s(?P<commitHash>\w*)\s<(?P<shortHash>\w*)>\nauthor\s<(?P<authorName>[^>]*)>\s<(?P<authorEmail>[^>]*)>\nsubject\s(?P<commitSubject>[^\n]*)\ndate\s(?P<commitDate>[^\n]*)\nbody\s(?P<commitBody>[\s\S]*?)\x00`)

// Later on when we add support for CommitCommiter we can abstract Author to it's own struct
type Commit struct {
	Hash            string
	ShortHash       string
	ParentHash      string
	ParentShortHash string
	AuthorName      string
	AuthorEmail     string
	Date            string
	Subject         string
	Body            string
}

// Add more as we need it
// Next parent needs to be fixed up so that we don't get the first commit of a paged
// response with the same commit as the last commit as the prev response: e.g.
// commit x
// commit y
// commit y
// commit z
type SimpleGitLog struct {
	Commits         []*Commit
	MaybeLastPage   bool
	IsPaginationReq bool
	NextParent      string // hash of the commit
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
	fmt.Printf("took %s to append rune\n", time.Since(start))
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
type Diff struct {
	Header      string
	HeaderLines []string
	ChunkLine   string // may not be necessary to have a special ref to it
	Lines       []string
	HunkNum     int
}

// src/whatever/whatever.c | 15 +++++++-----
type StatLine struct {
	Path         string // src/whatever/whatever.c
	LinesChanged string // 15
	GraphString  string // +++++------
	HunkNum      int    // used to link to say, #h0, which is the diff of this path
}

type DiffStat struct {
	StatLines   []*StatLine
	SummaryLine string // 4 files changed, 50 insertions(+), 6 deletions(-)
}

type GitShow struct {
	Commit   *Commit // basic commit info
	Diffs    []*Diff
	DiffStat *DiffStat
}

// var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ai%nbody %b"
var customShowFormat = "format:commit %H <%h>%nparent %P <%p>%nauthor <%an> <%ae>%nsubject %s%ndate %ai%nbody %b"
var gitShowRegex = regexp.MustCompile("(?ms)" + `commit\s(?P<commitHash>\w*)\s<(?P<shortHash>\w*)>\nparent\s(?P<parentHash>\w*)\s<(?P<shortParentHash>\w*)>\nauthor\s<(?P<authorName>[^>]*)>\s<(?P<authorEmail>[^>]*)>\nsubject\s(?P<commitSubject>[^\n]*)\ndate\s(?P<commitDate>[^\n]*)\nbody\s(?P<commitBody>[\s\S]*?)\n---\n(?P<diffStat>.*)\x00(?P<diffText>.*)`)

// used to parse src/whatever/whatever.c | 15 +++++++-----
var diffStatLineRegex = regexp.MustCompile("(.*)\\s\\|\\s(\\d*)\\s(.*)")

// Given a specific commitHash, get detailed info (--numstat or --shortstat)
func gitShowCommit(relativePath string, repo config.RepoConfig, commit string) (*GitShow, error) {

	// git show 74846d35b24b6efd61bb88a0a750b6bb257e6e78 --patch-with-stat -z > out.txt
	out, err := exec.Command("git", "-C", repo.Path, "show", commit, "--patch-with-stat", "--pretty="+customShowFormat, "-z").Output()

	if err != nil {
		return nil, err
	}

	match := gitShowRegex.FindSubmatch(out)

	gitShow := GitShow{}

	gitCommit := Commit{
		Hash:            string(match[1]),
		ShortHash:       string(match[2]),
		ParentHash:      string(match[3]),
		ParentShortHash: string(match[4]),
		AuthorName:      string(match[5]),
		AuthorEmail:     string(match[6]),
		Subject:         string(match[7]),
		Date:            string(match[8]),
		Body:            string(match[9]),
	}

	diffStat := DiffStat{}
	diffStatBytes := match[10]
	buf := bytes.NewBuffer(diffStatBytes)
	hunkNum := 0
	for {
		line, err := buf.ReadBytes('\n')

		if err != nil {
			break
		}

		match := diffStatLineRegex.FindSubmatch(line)

		if len(match) == 0 {
			diffStat.SummaryLine = string(line)
			break
		}

		statLine := StatLine{
			HunkNum:      hunkNum,
			Path:         string(match[1]),
			LinesChanged: string(match[2]),
			GraphString:  string(match[3]),
		}

		diffStat.StatLines = append(diffStat.StatLines, &statLine)
		hunkNum += 1
	}

	diffText := match[11]
	// We'll have to see how this behaves with long lines
	diffBuf := bytes.NewBuffer(diffText)
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
		} else if strings.HasPrefix(s, "@@") {
			currDif.ChunkLine = s
		}

		// If we haven't seen the @@ line yet, then add to header info
		if currDif.ChunkLine == "" {
			currDif.HeaderLines = append(currDif.HeaderLines, s)
		} else {
			currDif.Lines = append(currDif.Lines, s)
		}

	}

	gitShow.DiffStat = &diffStat
	gitShow.Commit = &gitCommit

	return &gitShow, nil
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
		PathSegments:   segments,
		Repo:           repo,
		Commit:         commit,
		DirContent:     dirContent,
		FileContent:    fileContent,
		ExternalDomain: externalDomain,
		Permalink:      permalink,
		Headlink:       headlink,
	}, nil
}

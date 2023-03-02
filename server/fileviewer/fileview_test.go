package fileviewer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadmeRegex(t *testing.T) {
	cases := []struct {
		in  string
		out []string
	}{
		{
			"README.md",
			[]string{"README.md", "README", "md"},
		},
		{
			"readme.md",
			[]string{"readme.md", "readme", "md"},
		},
		{
			"readme.rst",
			[]string{"readme.rst", "readme", "rst"},
		},
		{
			"readme.unknown",
			nil,
		},
		{
			"what.md",
			nil,
		},
	}

	for _, tc := range cases {
		matches := supportedReadmeRegex.FindStringSubmatch(tc.in)
		if !reflect.DeepEqual(tc.out, matches) {
			got, _ := json.MarshalIndent(matches, "", "  ")
			want, _ := json.MarshalIndent(tc.out, "", "  ")
			t.Errorf("error parsing %q: expected:\n%s\ngot:\n%s",
				tc.in, want, got)
		}
	}
}

const exampleDiff = `diff --git a/server/fileview.go b/server/fileview.go
index 58f6d94..2e43ee5 100644
--- a/server/fileview.go
+++ b/server/fileview.go
@@ -270,7 +270,7 @@ func buildDirectoryListEntry(treeEntry gitTreeEntry, pathFromRoot string, repo c
 * body ............
 * \x00 (null seperator from the -z option)
  */
-var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ai%nbody %b"
+var customGitLogFormat = "format:commit %H <%h>%nauthor <%an> <%ae>%nsubject %s%ndate %ah%nbody %b"
 
 // The named capture groups are just for human readability
`

func TestParseUnifiedGitDiff(t *testing.T) {

	scanner := bufio.NewScanner(strings.NewReader(exampleDiff))

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	diff := parseGitUnifiedDiff(scanner)
	if diff == nil {
		t.Errorf("diff is nil. should not be. diff=%+v\n", diff)
	}

	if diff.IsBinary {
		t.Errorf("diff marked as binary but should not be. Diff=%+v\n", diff)
	}
	if len(diff.Hunks) != 1 {
		t.Errorf("Expected 1 hunk but got %d\n", len(diff.Hunks))
	}

	onlyHunk := diff.Hunks[0]
	if len(onlyHunk.Lines) != 8 {
		t.Errorf("Expected 8 hunk lines but got: %d. hunk=%+v\n", len(onlyHunk.Lines), onlyHunk)
	}

	// we could test the content of each hunk line by line but... eh.

	// test that the first line of the hunk is a hunk line
	if onlyHunk.Lines[0].Type != HunkLine {
		t.Errorf("the first line should always be a hunk line. hunk=%+v\n", onlyHunk)
	}

	// there should be one deleted line and one added line

	// just for seeing what happens
	rows := diff.GetDiffRowsSplit()

	fmt.Printf("testing testing testing\n")
	fmt.Printf("rows=%+v\n", rows)
}

const exampleDiffWithMultipleHunks = `diff --git a/server/templates/templates.go b/server/templates/templates.go
index 4045de2..6033aab 100644
--- a/server/templates/templates.go
+++ b/server/templates/templates.go
@@ -21,6 +21,7 @@ import (
 	"github.com/sergi/go-diff/diffmatchpatch"
 
 	"github.com/livegrep/livegrep/server/api"
+	"github.com/livegrep/livegrep/server/fileviewer"
 )
 
 func linkTag(nonce template.HTMLAttr, rel string, s string, m map[string]string) template.HTML {
@@ -432,6 +433,8 @@ func getFuncs() map[string]interface{} {
 		"getSyntaxHighlightedContent":      getSyntaxHighlightedContent,
 		"renderDirectoryTree":              RenderDirectoryTree,
 		"renderSplitDiffHalf":              renderSplitDiffHalf,
+		"getDiffRowType":                   getDiffRowType,
+		"getClassFromRowType":              getClassFromRowType,
 	}
 }
 
@@ -475,3 +478,33 @@ func LoadAssetHashes(assetHashFile string, assetHashMap map[string]string) error
 
 	return nil
 }
+
+func getDiffRowType(row fileviewer.IDiffRow) string {
+	switch row.(type) {
+	case fileviewer.IDiffRowAdded:
+		return "added"
+	case fileviewer.IDiffRowDeleted:
+		return "deleted"
+	case fileviewer.IDiffRowContext:
+		return "context"
+	case fileviewer.IDiffRowModified:
+		return "modified"
+	case fileviewer.IDiffRowHunk:
+		return "hunk"
+	default:
+		fmt.Printf("encountered weird row. row T=%T val=%v\n type=%v", row)
+		return "blah"
+	}
+
+	return "blah"
+}
+
+func getClassFromRowType(rowType string) string {
+	switch rowType {
+	case "hunk":
+		return "hunk-row"
+	default:
+		return "row"
+	}
+	return ""
+}
`

func TestParseUnifiedGitDiffWithMultipleHunks(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(exampleDiffWithMultipleHunks))

	const maxCapacity = 100 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	diff := parseGitUnifiedDiff(scanner)
	if diff == nil {
		t.Errorf("diff is nil. should not be. diff=%+v\n", diff)
	}

	if diff.IsBinary {
		t.Errorf("diff marked as binary but should not be. Diff=%+v\n", diff)
	}
	if len(diff.Hunks) != 3 {
		t.Errorf("Expected 3 hunks but got %d\n", len(diff.Hunks))
	}

	// onlyHunk := diff.Hunks[0]
	// if len(onlyHunk.Lines) != 8 {
	// 	t.Errorf("Expected 8 hunk lines but got: %d. hunk=%+v\n", len(onlyHunk.Lines), onlyHunk)
	// }

	// // we could test the content of each hunk line by line but... eh.

	// // test that the first line of the hunk is a hunk line
	// if onlyHunk.Lines[0].Type != HunkLine {
	// 	t.Errorf("the first line should always be a hunk line. hunk=%+v\n", onlyHunk)
	// }

	// // there should be one deleted line and one added line

	// // just for seeing what happens
	// rows := diff.GetDiffRowsSplit()

	// fmt.Printf("testing testing testing\n")
	// fmt.Printf("rows=%+v\n", rows)

}

func TestParseUnifiedGitDiffEmptyDiff(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))

	diff := parseGitUnifiedDiff(scanner)

	if diff != nil {
		t.Errorf("diff is not nil. It should be! diff=%+v\n", diff)
	}
}

func TestNumberFromGroup(t *testing.T) {
	cases := []struct {
		in  []byte
		df  int
		out int
	}{
		{
			[]byte("1"),
			-1,
			1,
		},
		{
			[]byte("10"),
			-1,
			10,
		},
		{
			[]byte("100"),
			-1,
			100,
		},
		{
			[]byte("1000"),
			-1,
			1000,
		},
		{
			[]byte("15000"),
			-1,
			15000,
		},
		{
			[]byte("blah"),
			10,
			10,
		},
	}

	for _, tc := range cases {
		num := numberFromGroup(tc.in, tc.df)
		if num != tc.out {
			t.Errorf("expected=%d got=%d\n", tc.out, num)
		}
	}

}

func TestGenLogArgs(t *testing.T) {
	cases := []struct {
		in  *CommitOptions
		out string
		err bool
	}{
		{
			in:  &CommitOptions{N: 10, SkipN: 30, Range: "revspec"},
			out: "log -n 10 --skip 30 revspec",
			err: false,
		},
		{
			in:  &CommitOptions{Path: "/cmd/test/test.go"},
			out: "log -- /cmd/test/test.go",
			err: false,
		},
		{
			in:  &CommitOptions{N: 10, SkipN: 30, NameOnly: true, Follow: true, Path: "/cmd/test/test.go"},
			out: "log -n 10 --skip 30 --name-only --follow -- /cmd/test/test.go",
			err: false,
		},
	}
	initArgs := []string{"log"}
	for _, tc := range cases {
		args, err := tc.in.genLogArgs(initArgs)

		if err != nil && !tc.err {
			t.Errorf("genLogArgs errored but should not have. err=%+v\n", err)
		}

		strArgs := strings.Join(args, " ")

		if strArgs != tc.out {
			t.Errorf("genLogArgs: expected=%s got=%s\n", tc.out, strArgs)
		}
	}
}

const te = `Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677217274\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677217274\x00Handle a completely empty diff and failed header parsing\x00
717a8f7d543bcc427482636f40c995df27b6fd1d\x002e8e042956123c09092fc1656d425188e98107aa\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677185323\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677185323\x00Fix diff line numbers, and process multiple hunks when available\x00\x00a9ba1f919b7d6c27868914de587c2e13f11992d9\x00
a9ba1f919b7d6c27868914de587c2e13f11992d9\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677131189\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677131398\x00Mostly working (besides line numbers) split diffs in fileviewer!\x00\x0017a38aa9fd09854cb4de33d124fa9ce6ba60acd9\x00
425a4d2fda390295bb6a04dca218ae7ac4623655\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677119046\x00Rodrigo Silva Mendoza\x00xvandish@users.noreply.github.com\x001677119046\x00Move fileview to fileviewer and into its own package\x00\x00e0c76350d7a15b4a3ddd60fb6661bbc1c961c6af\x00
`

func getTimeForCommit(in string) time.Time {
	t, _ := parseTimeFromLogPart([]byte(in))
	return t
}

func compareSignatures(testCase string, id CommitId, want, got Signature, sigType string, t *testing.T) {
	if want.Name != got.Name {
		t.Errorf("[%s]-[%s] Commit Signature %s Name does not match. Want=%s got=%s\n", testCase, id, sigType, want.Name, got.Name)
	}
	if want.Email != got.Email {
		t.Errorf("[%s]-[%s] Commit Signature %s Email does not match. Want=%s got=%s\n", testCase, id, sigType, want.Email, got.Email)
	}
	if want.Date != got.Date {
		t.Errorf("[%s]-[%s] Commit Signature %s Date does not match. Want=%s got=%s\n", testCase, id, sigType, want.Date, got.Date)
	}
}
func compareTwoCommits(testCase string, want, got *GitCommit, t *testing.T) {
	// while reflect is much faster, its miserable to find differences, so we do it manually
	if want.ID != got.ID {
		t.Errorf("[%s] Commit IDs don't match: Want=%s got=%s\n", testCase, want.ID, got.ID)
	}

	compareSignatures(testCase, want.ID, want.Author, got.Author, "author", t)
	compareSignatures(testCase, want.ID, *want.Committer, *got.Committer, "commiter", t)

	if want.Subject != got.Subject {
		t.Errorf("[%s]-[%s] Commit Subject's don't match: Want=%s got=%s\n", testCase, want.ID, want.Subject, got.Subject)
	}

	if want.Body != got.Body {
		t.Errorf("[%s]-[%s] Commit Body's don't match: Want=%s got=%s\n", testCase, want.ID, want.Body, got.Body)
	}

	for i, file := range want.Files {
		if strings.Compare(file, got.Files[i]) != 0 {
			t.Errorf("[%s]-[%s] files[%d] don't match. Want=%s got=%s\n", testCase, want.ID, i, file, got.Files[i])
		}
	}
}

// While the best way to do this would be to use a real life git repo to test, this will suffice
func TestParseCommitLogOutput(t *testing.T) {
	// the following log files were generated with
	// git -C ../repos-for-livegrep/xvandish/livegrep log fileviewer-improvements --format=format:%H%x00%aN%x00%aE%x00%at%x00%cN%x00%cE%x00%ct%x00%s%x00%b%x00%P%x00 -- server/fileviewer/fileview.go
	// with the --name-only flag when indicated
	cases := []struct {
		in_file  string
		nameOnly bool
		out      []*GitCommit
		err      bool
	}{
		{
			in_file:  "./simple_log.txt",
			nameOnly: false,
			out: []*GitCommit{
				&GitCommit{
					ID:        "604e7f313f5a070bbaac2ccb2c23344c669ae1fb",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677217274")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677217274")},
					Subject:   "Handle a completely empty diff and failed header parsing",
					Body:      "",
					Parents:   []CommitId{CommitId("717a8f7d543bcc427482636f40c995df27b6fd1d")},
				},
				&GitCommit{
					ID:        "2e8e042956123c09092fc1656d425188e98107aa",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677185323")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677185323")},
					Subject:   "Fix diff line numbers, and process multiple hunks when available",
					Body:      "",
					Parents:   []CommitId{CommitId("a9ba1f919b7d6c27868914de587c2e13f11992d9")},
				},
				&GitCommit{
					ID:        "a9ba1f919b7d6c27868914de587c2e13f11992d9",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677131189")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677131398")},
					Subject:   "Mostly working (besides line numbers) split diffs in fileviewer!",
					Body:      "",
					Parents:   []CommitId{CommitId("17a38aa9fd09854cb4de33d124fa9ce6ba60acd9")},
				},
				&GitCommit{
					ID:        "425a4d2fda390295bb6a04dca218ae7ac4623655",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677119046")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677119046")},
					Subject:   "Move fileview to fileviewer and into its own package",
					Body:      "",
					Parents:   []CommitId{CommitId("e0c76350d7a15b4a3ddd60fb6661bbc1c961c6af")},
				},
			},
		},
		{
			in_file:  "./simple_log_name_only.txt",
			nameOnly: true,
			out: []*GitCommit{
				&GitCommit{
					ID:        "604e7f313f5a070bbaac2ccb2c23344c669ae1fb",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677217274")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677217274")},
					Subject:   "Handle a completely empty diff and failed header parsing",
					Body:      "",
					Parents:   []CommitId{CommitId("717a8f7d543bcc427482636f40c995df27b6fd1d")},
					Files:     []string{"server/fileviewer/fileview.go"},
				},
				&GitCommit{
					ID:        "2e8e042956123c09092fc1656d425188e98107aa",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677185323")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677185323")},
					Subject:   "Fix diff line numbers, and process multiple hunks when available",
					Body:      "",
					Parents:   []CommitId{CommitId("a9ba1f919b7d6c27868914de587c2e13f11992d9")},
					Files:     []string{"server/fileviewer/fileview.go"},
				},
				&GitCommit{
					ID:        "a9ba1f919b7d6c27868914de587c2e13f11992d9",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677131189")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677131398")},
					Subject:   "Mostly working (besides line numbers) split diffs in fileviewer!",
					Body:      "",
					Parents:   []CommitId{CommitId("17a38aa9fd09854cb4de33d124fa9ce6ba60acd9")},
					Files:     []string{"server/fileviewer/fileview.go"},
				},
				&GitCommit{
					ID:        "425a4d2fda390295bb6a04dca218ae7ac4623655",
					Author:    Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677119046")},
					Committer: &Signature{Name: "Rodrigo Silva Mendoza", Email: "xvandish@users.noreply.github.com", Date: getTimeForCommit("1677119046")},
					Subject:   "Move fileview to fileviewer and into its own package",
					Body:      "",
					Parents:   []CommitId{CommitId("e0c76350d7a15b4a3ddd60fb6661bbc1c961c6af")},
					Files:     []string{"server/fileviewer/fileview.go"},
				},
			},
			err: false,
		},
	}

	for _, tc := range cases {
		bytes, err := os.ReadFile(tc.in_file)

		if err != nil {
			t.Errorf("err reading file: %s\n", err)
		}

		commits, err := parseCommitLogOutput(bytes, tc.nameOnly)

		if err != nil && !tc.err {
			t.Errorf("did not expect error but got: %s\n", err)
		}

		// somehow, deep compare the commits, start with length
		if len(commits) != len(tc.out) {
			t.Errorf("expected %d commits but got %d\n", len(tc.out), len(commits))
		}

		for i, commit := range commits {
			compareTwoCommits(tc.in_file, tc.out[i], commit, t)
		}
	}
}

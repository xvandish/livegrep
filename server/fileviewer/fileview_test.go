package fileviewer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
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

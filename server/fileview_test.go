package server

import (
	"bufio"
	"encoding/json"
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

	parseGitUnifiedDiff(scanner)
}

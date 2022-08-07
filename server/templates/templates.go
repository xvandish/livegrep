package templates

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/livegrep/livegrep/server/api"
)

func linkTag(nonce template.HTMLAttr, rel string, s string, m map[string]string) template.HTML {
	hash := m[strings.TrimPrefix(s, "/")]
	href := s + "?v=" + hash
	hashBytes, _ := hex.DecodeString(hash)
	integrity := "sha256-" + base64.StdEncoding.EncodeToString(hashBytes)
	return template.HTML(fmt.Sprintf(
		`<link%s rel="%s" href="%s" integrity="%s" />`,
		nonce, rel, href, integrity,
	))
}

func scriptTag(nonce template.HTMLAttr, s string, m map[string]string) template.HTML {
	hash := m[strings.TrimPrefix(s, "/")]
	href := s + "?v=" + hash
	hashBytes, _ := hex.DecodeString(hash)
	integrity := "sha256-" + base64.StdEncoding.EncodeToString(hashBytes)
	return template.HTML(fmt.Sprintf(
		`<script%s src="%s" integrity="%s"></script>`,
		nonce, href, integrity,
	))
}

type lineParts struct {
	Prefix      string
	Highlighted string
	Suffix      string
}

func splitCodeLineIntoParts(line string, bounds []int) lineParts {
	start := bounds[0]
	end := bounds[1]

	p := lineParts{
		Prefix:      line[0:start],
		Highlighted: line[start:end],
		Suffix:      line[end:],
	}

	return p
}

func renderCodeLine(line string, bounds []api.Bounds) template.HTML {
	// There may be multiple bounds
	// var parts []lineParts

	// for _, bound := range bounds {
	// 	start := bound.Left
	// 	end := bound.Right

	// 	parts = append(parts, lineParts{
	// 		Prefix:      line[0:start],
	// 		Highlighted: line[start:end],
	// 		Suffix:      line[end:],
	// 	})
	// }

	fmt.Printf("got bounds of: %+v\n", bounds)

	// let's just build the string for the template engine
	// at some point we may have bounds of different lengths, once we get regex searches to also show
	// multiple bounds, so we can't optimize that.

	// process each bound at a time
	// keep track of the currentIdx into the string
	// at each bound.Left, if it's greater than currentIdx, we have a prefix
	// at each bound.Right, set currentIdx to bound.Right. If there are no more bounds left, then we have a suffix
	currIdx := 0
	lineOut := ""
	lastBound := len(bounds) - 1

	for boundIdx, bound := range bounds {
		if bound.Left > currIdx {
			lineOut += fmt.Sprintf("<span>%s</span>", line[currIdx:bound.Left])
		}
		currIdx = bound.Right

		lineOut += fmt.Sprintf("<span class='highlighted'>%s</span>", line[bound.Left:bound.Right])

		if boundIdx == lastBound && currIdx <= len(line) {
			lineOut += fmt.Sprintf("<span>%s</span>", line[currIdx:len(line)])
		}
	}

	return template.HTML(lineOut)
}

// used to cap slice iteration
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// returns [:min(n|len(T))]
func getFirstNFiles(s []*api.FileResult, n int) []*api.FileResult {
	c := min(n, len(s))
	return s[:c]
}

func shouldInsertBlankLine(currIdx int, lines []*api.ResultLine) bool {
	prevIdx := currIdx - 1
	if prevIdx < 0 {
		return false
	}

	return lines[currIdx].LineNumber-lines[prevIdx].LineNumber != 1
}

func getLineNumberLinkClass(bounds []api.Bounds) string {
	if len(bounds) > 0 {
		return "num-link match"
	}
	return "num-link"
}

func getFuncs() map[string]interface{} {
	return map[string]interface{}{
		"loop":                   func(n int) []struct{} { return make([]struct{}, n) },
		"toLineNum":              func(n int) int { return n + 1 },
		"linkTag":                linkTag,
		"scriptTag":              scriptTag,
		"splitCodeLineIntoParts": splitCodeLineIntoParts,
		"min":                    min,
		"getFirstNFiles":         getFirstNFiles,
		"shouldInsertBlankLine":  shouldInsertBlankLine,
		"getLineNumberLinkClass": getLineNumberLinkClass,
		"renderCodeLine":         renderCodeLine,
	}
}

func LoadTemplates(base string, templates map[string]*template.Template) error {
	pattern := base + "/templates/common/*.html"
	common := template.New("").Funcs(getFuncs())
	common = template.Must(common.ParseGlob(pattern))

	pattern = base + "/templates/*.html"
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, path := range paths {
		t := template.Must(common.Clone())
		t = template.Must(t.ParseFiles(path))
		templates[filepath.Base(path)] = t
	}
	return nil
}

func LoadAssetHashes(assetHashFile string, assetHashMap map[string]string) error {
	file, err := os.Open(assetHashFile)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for k := range assetHashMap {
		delete(assetHashMap, k)
	}

	for scanner.Scan() {
		pieces := strings.SplitN(scanner.Text(), "  ", 2)
		hash := pieces[0]
		asset := pieces[1]
		(assetHashMap)[asset] = hash
	}

	return nil
}

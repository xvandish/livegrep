package templates

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/chroma"
	htmlf "github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
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

type CodePart struct {
	Line  string
	Match bool
}

func renderCodeLine(line string, bounds [][2]int) []CodePart {
	// process each bound at a time
	// keep track of the currentIdx into the string
	// at each bound.Left, if it's greater than currentIdx, we have a prefix
	// at each bound.Right, set currentIdx to bound.Right. If there are no more bounds left, then we have a suffix
	currIdx := 0
	lastBound := len(bounds) - 1

	var codeParts []CodePart

	for boundIdx, bound := range bounds {
		leftBound := bound[0]
		rightBound := bound[1]

		if bound[0] > currIdx {
			codeParts = append(codeParts, CodePart{
				Line:  line[currIdx:leftBound],
				Match: false,
			})
		}
		currIdx = rightBound

		codeParts = append(codeParts, CodePart{
			Line:  line[leftBound:rightBound],
			Match: true,
		})

		if boundIdx == lastBound && currIdx <= len(line) {
			codeParts = append(codeParts, CodePart{
				Line:  line[currIdx:len(line)],
				Match: false,
			})
		}
	}
	return codeParts
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

func getLineNumberLinkClass(bounds [][2]int) string {
	if len(bounds) > 0 {
		return "num-link match"
	}
	return "num-link"
}

func convertContentBlobToArrayOfLines(content string) []template.HTML {
	init := strings.Split(content, "\n")
	asHtml := make([]template.HTML, len(init))
	for idx, s := range init {
		asHtml[idx] = template.HTML(s)
	}

	return asHtml
}

type SyntaxHighlightedContent struct {
	Content []template.HTML
	Styles  template.CSS
}

// TODO(xvandish): Convert this function so that our build process can use it
// and inject it into our stylesheets. Right now, manual process.
func styleToCSS(style *chroma.Style) map[chroma.TokenType]string {
	classes := map[chroma.TokenType]string{}

	bg := style.Get(chroma.Background)

	for t := range chroma.StandardTypes {
		entry := style.Get(t)

		if t != chroma.Background {
			entry = entry.Sub(bg)
		}

		styleEntryCSS := htmlf.StyleEntryToCSS(entry)
		if styleEntryCSS != `` && classes[t] != `` {
			styleEntryCSS += `;`
		}
		classes[t] = styleEntryCSS + classes[t]
	}

	return classes
}

func getChromaClass(t chroma.TokenType) string {
	for t != 0 {
		if cls, ok := chroma.StandardTypes[t]; ok {
			if cls != "" {
				return cls
			}
			return ""
		}
		t = t.Parent()
	}
	if cls := chroma.StandardTypes[t]; cls != "" {
		return cls
	}
	return ""
}

func styleAttr(styles map[chroma.TokenType]string, tt chroma.TokenType) string {
	cls := getChromaClass(tt)
	if cls == "" {
		return ""
	}
	return fmt.Sprintf(` class="%s"`, cls)
}

func writeCSS(w io.Writer, style *chroma.Style) error {
	css := styleToCSS(style)

	tts := []int{}
	for tt := range css {
		tts = append(tts, int(tt))
	}
	sort.Ints(tts)
	for _, ti := range tts {
		tt := chroma.TokenType(ti)
		class := getChromaClass(tt)
		if class == "" {
			continue
		}
		styles := css[tt]
		if styles == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "/* %s */ .%schroma .%s { %s }\n", tt, "", class, styles); err != nil {
			return err
		}
	}
	return nil
}

func timeTrack(start time.Time, name string) {
	fmt.Printf("%s took %s\n", name, time.Since(start))
}

func getSyntaxHighlightedContent(content, language, filename string) SyntaxHighlightedContent {
	defer timeTrack(time.Now(), "getSyntaxHighlightedContent")
	fmt.Printf("language is %s\n", language)
	l := lexers.Get(language)
	css := styleToCSS(styles.Xcode)

	if l == nil {
		fmt.Printf("unable to get lexer with language=%s. Trying via filename=%s\n", language, filename)
		l = lexers.Match(filename)
		if l == nil {
			fmt.Printf("failed to get lexer with filename. Not using a lexer and just splitting content.\n")
			return SyntaxHighlightedContent{
				Content: convertContentBlobToArrayOfLines(content),
				Styles:  "",
			}
		}
	}

	// Use the coalescing lexer to coalesce runs of idential token types into a single token
	l = chroma.Coalesce(l)

	it, err := l.Tokenise(nil, content)

	if err != nil {
		fmt.Printf("error tokenizing=%+v\n", err)
		return SyntaxHighlightedContent{
			Content: convertContentBlobToArrayOfLines(content),
			Styles:  "",
		}
	}

	tokens := it.Tokens()

	lines := chroma.SplitTokensIntoLines(tokens)
	outLines := make([]template.HTML, len(lines))

	for idx, tokens := range lines {
		// we want to convert the line into its html equivalent
		// each line can have n tokens
		// each token is a span with (potentially) styling

		var b strings.Builder
		for _, token := range tokens {
			html := html.EscapeString(token.String())
			attr := styleAttr(css, token.Type)
			b.WriteString(fmt.Sprintf("<span%s>%s</span>", attr, html))
		}

		outLines[idx] = template.HTML(b.String())
	}

	return SyntaxHighlightedContent{
		Content: outLines,
		// Styles:  template.CSS(sbuilder.String()),
		Styles: "",
	}
}

func getFuncs() map[string]interface{} {
	return map[string]interface{}{
		"loop":                             func(n int) []struct{} { return make([]struct{}, n) },
		"toLineNum":                        func(n int) int { return n + 1 },
		"linkTag":                          linkTag,
		"scriptTag":                        scriptTag,
		"splitCodeLineIntoParts":           splitCodeLineIntoParts,
		"min":                              min,
		"getFirstNFiles":                   getFirstNFiles,
		"shouldInsertBlankLine":            shouldInsertBlankLine,
		"getLineNumberLinkClass":           getLineNumberLinkClass,
		"renderCodeLine":                   renderCodeLine,
		"convertContentBlobToArrayOfLines": convertContentBlobToArrayOfLines,
		"getSyntaxHighlightedContent":      getSyntaxHighlightedContent,
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

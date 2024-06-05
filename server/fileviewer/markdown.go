package fileviewer

import (
	"bytes"

	chromahtml "github.com/alecthomas/chroma/formatters/html"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
			),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
	goldmark.WithRendererOptions(
		html.WithHardWraps(),
		html.WithXHTML(),
	),
)

func RenderMarkdown(source string) (bytes.Buffer, error) {
	var buf bytes.Buffer
	if _, err := buf.WriteString(`<div class="markdown-content">`); err != nil {
		return buf, err
	}

	err := md.Convert([]byte(source), &buf)
	if err != nil {
		return buf, err
	}
	if _, err := buf.WriteString("</div>"); err != nil {
		return buf, err
	}
	return buf, err
}

# gazelle:ignore
load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "fileview.go",
        "markdown.go"
    ],
    importpath = "github.com/livegrep/livegrep/server/fileviewer",
    visibility = ["//visibility:public"],
    deps = [
        "//server/api:go_default_library",
        "//server/config:go_default_library",
        "@com_github_sergi_go_diff//diffmatchpatch:go_default_library",
        "@com_github_yuin_goldmark//:goldmark",
        "@com_github_yuin_goldmark//ast",
        "@com_github_yuin_goldmark//extension",
        "@com_github_yuin_goldmark//parser",
        "@com_github_yuin_goldmark//renderer/html",
        "@com_github_yuin_goldmark//text",
        "@com_github_yuin_goldmark//util",
        "@com_github_yuin_goldmark_highlighting_v2//:goldmark-highlighting",
        "@com_github_alecthomas_chroma_v2//formatters/html:go_default_library",
    ]
)

go_test(
    name = "go_default_test",
    srcs = [
        "fileview_test.go",
    ],
    data = [
        "simple_log.txt",
        "simple_log_name_only.txt",
    ],
    embed = [":go_default_library"],
)

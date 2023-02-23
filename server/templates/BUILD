load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["templates.go"],
    importpath = "github.com/livegrep/livegrep/server/templates",
    visibility = ["//visibility:public"],
    deps = [
        "//server/api:go_default_library",
        "//server/fileviewer:go_default_library",
        "@com_github_alecthomas_chroma//:go_default_library",
        "@com_github_alecthomas_chroma//formatters/html:go_default_library",
        "@com_github_alecthomas_chroma//lexers:go_default_library",
        "@com_github_alecthomas_chroma//styles:go_default_library",
        "@com_github_sergi_go_diff//diffmatchpatch:go_default_library",
    ],
)

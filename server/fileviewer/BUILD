# gazelle:ignore
load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "fileview.go"
    ],
    importpath = "github.com/livegrep/livegrep/server/fileviewer",
    visibility = ["//visibility:public"],
    deps = [
        "//server/api:go_default_library",
        "//server/config:go_default_library",
        "@com_github_sergi_go_diff//diffmatchpatch:go_default_library",
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
        "long_log.txt",
        "really_long_log.txt",
    ],
    embed = [":go_default_library"],
)

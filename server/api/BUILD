load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["types.go"],
    importpath = "github.com/livegrep/livegrep/server/api",
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_sergi_go_diff//diffmatchpatch:go_default_library",
    ]
)

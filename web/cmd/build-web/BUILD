load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    importpath = "github.com/livegrep/livegrep/web/cmd/build-web",
    visibility = ["//visibility:private"],
    deps = [
        "@com_github_evanw_esbuild//pkg/api:go_default_library",
    ],
)

go_binary(
    name = "build_web",
    embed = [":go_default_library"],
    visibility = ["//visibility:public"],
)

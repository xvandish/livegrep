load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "flags.go",
        "main.go",
    ],
    importpath = "github.com/livegrep/livegrep/cmd/livegrep-metrics-exporter",
    visibility = ["//visibility:private"],
    deps = [
        "@in_gopkg_alexcesaro_statsd_v2//:go_default_library",
    ],
)

go_binary(
    name = "livegrep-metrics-exporter",
    embed = [":go_default_library"],
    visibility = ["//visibility:public"],
)

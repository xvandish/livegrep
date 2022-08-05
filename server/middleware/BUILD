load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "google_iap.go",
        "reverse_proxy.go",
    ],
    importpath = "github.com/livegrep/livegrep/server/middleware",
    visibility = ["//visibility:public"],
    deps = [
        "//server/config:go_default_library",
        "//server/log:go_default_library",
        "@org_golang_google_api//idtoken:go_default_library",
        "@org_golang_x_net//context:go_default_library",
    ],
)

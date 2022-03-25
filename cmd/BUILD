load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

pkg_tar(
    name = "go_tools",
    srcs = [
        "//cmd/{}:{}".format(cmd, cmd)
        for cmd in [
            "lg",
            "livegrep",
            "livegrep-fetch-reindex",
            "livegrep-github-reindex",
            "livegrep-reload",
            "livegrep-metrics-exporter",
        ]
    ],
    package_dir = "/bin",
    visibility = ["//visibility:public"],
)

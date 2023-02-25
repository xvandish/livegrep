package(default_visibility = ["//visibility:public"])

load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")

filegroup(
    name = "templates",
    srcs = glob(["templates/**/*"]),
)

filegroup(
    name = "htdocs",
    srcs = glob(["htdocs/**"]),
)

genrule(
    name = "asset_hashes",
    srcs = [
        "//web:htdocs",
        "//web:build_js_and_css",
    ],
    outs = ["hashes.txt"],
    cmd = "sha256sum $(SRCS) | sed \"s~ $(BINDIR)/~ ~\" | sed \"s~ web/htdocs/~ ~\"> $@",
)

genrule(
    name = "build_js_and_css",
    srcs = glob([
        "src/**",
        "htdocs/assets/css/**",
    ]),
    outs = [
        "htdocs/assets/js/bundle_new.js",
        "htdocs/assets/js/fileview_v2.min.js",
        "htdocs/assets/css/codesearch.min.css",
    ],
    cmd = "$(location //web/cmd/build-web:build_web) -test=$(RULEDIR)",
    tools = ["//web/cmd/build-web:build_web"],
)

pkg_tar(
    name = "assets",
    srcs = [
        ":asset_hashes",
        ":build_js_and_css",
        ":htdocs",
        ":templates",
    ],
    package_dir = "web/",
    strip_prefix = ".",
)

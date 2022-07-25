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
        "//web:test_genrule_two",
    ],
    outs = ["hashes.txt"],
    cmd = "sha256sum $(SRCS) | sed \"s~ $(BINDIR)/~ ~\" | sed \"s~ web/htdocs/~ ~\"> $@",
)

genrule(
    name = "test_genrule",
    outs = ["what.txt"],
    cmd = "$(location //cmd/lg_minify:lg_minify) > $@",
    tools = ["//cmd/lg_minify:lg_minify"],
)

genrule(
    name = "test_genrule_two",
    outs = ["htdocs/assets/js/bundle_new.js", "htdocs/assets/css/codesearch.min.css"],
    srcs = glob([
        "src/**",
        "htdocs/assets/css/**",
    ]),
    cmd = "$(location //web/cmd/build-web:build_web) -test=$(RULEDIR)",
    tools = ["//web/cmd/build-web:build_web"],
)

pkg_tar(
    name = "assets",
    srcs = [
        ":asset_hashes",
        ":htdocs",
        ":templates",
        ":test_genrule_two",
    ],
    package_dir = "web/",
    strip_prefix = ".",
)

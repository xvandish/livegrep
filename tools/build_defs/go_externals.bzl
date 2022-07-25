load(
    "@bazel_gazelle//:deps.bzl",
    "go_repository",
)

def _normalize_repo_name(repo):
    return repo.replace("/", "_").replace("-", "_").replace(".", "_")

def _github(repo, commit):
    name = "com_github_" + _normalize_repo_name(repo)
    importpath = "github.com/" + repo
    return struct(
        name = name,
        commit = commit,
        importpath = importpath,
    )

def _golang_x(pkg, commit):
    name = "org_golang_x_" + pkg
    importpath = "golang.org/x/" + pkg
    return struct(
        name = name,
        commit = commit,
        importpath = importpath,
    )

def _gopkg(repo, commit):
    name = "in_gopkg_" + _normalize_repo_name(repo)
    importpath = "gopkg.in/" + repo
    return struct(
        name = name,
        commit = commit,
        importpath = importpath,
    )

_externals = [
    _golang_x("net", "e204ce36a2ba698f7e949cbd2b13458cf51a8042"),
    _golang_x("text", "d1c84af989ab0f62cd853b5ae33b1b4db4f1e88b"),
    _golang_x("oauth2", "d3ed0bb246c8d3c75b63937d9a5eecff9c74d7fe"),
    _golang_x("crypto", "5e0467b6c7cee3ce8969a8b584d9e6ab01d074f7"),
    struct(
        name = "org_golang_google_appengine",
        commit = "170382fa85b10b94728989dfcf6cc818b335c952",
        importpath = "google.golang.org/appengine/",
        remote = "https://github.com/golang/appengine",
        vcs = "git",
    ),
    _github("google/go-github", "e0066688b631702f66e0435ee1633f9d0091e4b9"),
    _github("nelhage/go.cli", "2aeb96ef8025f3646befae8353b90f95e9e79bdc"),
    _github("bmizerany/pat", "c068ca2f0aacee5ac3681d68e4d0a003b7d1fd2c"),
    _github("google/go-querystring", "53e6ce116135b80d037921a7fdd5138cf32d7a8a"),
    _github("facebookgo/muster", "fd3d7953fd52354a74b9f6b3d70d0c9650c4ec2a"),
    _github("facebookgo/limitgroup", "6abd8d71ec01451d7f1929eacaa263bbe2935d05"),
    _github("facebookgo/clock", "600d898af40aa09a7a93ecb9265d87b0504b6f03"),
    _github("evanw/esbuild", "8c6c39a05b7904bb49b072938146098f4a27f3b8"),
    _gopkg("alexcesaro/statsd.v2", "7fea3f0d2fab1ad973e641e51dba45443a311a90"),
    _gopkg("check.v1", "20d25e2804050c1cd24a7eea1e7a6447dd0e74ec"),
    struct(
        name = "org_golang_google_grpc",
        commit = "f74f0337644653eba7923908a4d7f79a4f3a267b",
        importpath = "google.golang.org/grpc",
    ),

    struct(
        name = "org_golang_google_api",
        importpath = "google.golang.org/api",
        commit = "32bf29c2e17105d5f285adac4531846c57847f11", # v0.50.0
    ),
    struct(
        name = "com_google_cloud_go",
        importpath = "cloud.google.com/go",
        commit = "2a43d6d30d7041eb6ed0b305c81dc32c8c42ebc1", # v0.87.0
    ),
    struct(
        name = "io_opencensus_go",
        importpath = "go.opencensus.io",
        commit = "49838f207d61097fc0ebb8aeef306913388376ca", #v0.23.0
    ),
    struct(
        name = "com_github_golang_groupcache",
        importpath = "github.com/golang/groupcache",
        commit = "41bb18bfe9da5321badc438f91158cd790a33aa3",
    ),
]

def go_externals():
    go_repository(
        name = "org_golang_x_sys",
        importpath = "golang.org/x/sys",
        sum = "h1:ntjMns5wyP/fN65tdBD4g8J5w8n015+iIIs9rtjXkY0=",
        version = "v0.0.0-20220412211240-33da011f77ad",
    )
    go_repository(
        name = "com_google_cloud_go_compute",
        importpath = "cloud.google.com/go/compute",
        sum = "h1:rSUBvAyVwNJ5uQCKNJFMwPtTvJkfN38b6Pvb9zZoqJ8=",
        version = "v0.1.0",
    )

    for ext in _externals:
        if hasattr(ext, "vcs"):
            go_repository(
                name = ext.name,
                commit = ext.commit,
                importpath = ext.importpath,
                remote = ext.remote,
                vcs = ext.vcs,
            )
        else:
            go_repository(
                name = ext.name,
                commit = ext.commit,
                importpath = ext.importpath,
            )

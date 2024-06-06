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
        sum = "h1:aoLIYaA1fX3ywihqpBk2APQKOo20nXsp1GEZQbx5Jk4=",
        version = "v1.10.0",
    )
    go_repository(
        name = "com_google_cloud_go_compute_metadata",
        importpath = "cloud.google.com/go/compute/metadata",
        sum = "h1:/sxEbyrm6cw+XOUw1YxBHlatV71z4vpnmO7z2IZ0h3I=",
        version = "v0.1.1",
    )
    go_repository(
        name = "com_github_alecthomas_chroma",
        importpath = "github.com/alecthomas/chroma",
        sum = "h1:7XDcGkCQopCNKjZHfYrNLraA+M7e0fMiJ/Mfikbfjek=",
        version = "v0.10.0",
    )
    go_repository(
        name = "com_github_alecthomas_chroma_v2",
        build_file_proto_mode = "disable_global",
        importpath = "github.com/alecthomas/chroma/v2",
        sum = "h1:Wh8qLEgMMsN7mgyG8/qIpegky2Hvzr4By6gEF7cmWgw=",
        version = "v2.12.0",
    )
    go_repository(
        name = "com_github_davecgh_go_spew",
        importpath = "github.com/davecgh/go-spew",
        sum = "h1:vj9j/u1bqnvCEfJOwUhtlOARqs3+rkHYY13jYWTU97c=",
        version = "v1.1.1",
    )

    go_repository(
        name = "com_github_dlclark_regexp2",
        importpath = "github.com/dlclark/regexp2",
        sum = "h1:F1rxgk7p4uKjwIQxBs9oAXe5CqrXlCduYEJvrF4u93E=",
        version = "v1.4.0",
    )

    go_repository(
        name = "com_github_pmezard_go_difflib",
        importpath = "github.com/pmezard/go-difflib",
        sum = "h1:4DBwDE0NGyQoBHbLQYPwSUPoCMWR5BEzIk/f1lZbAQM=",
        version = "v1.0.0",
    )

    go_repository(
        name = "com_github_stretchr_objx",
        importpath = "github.com/stretchr/objx",
        sum = "h1:4G4v2dO3VZwixGIRoQ5Lfboy6nUhCyYzaqnIAPPhYs4=",
        version = "v0.1.0",
    )

    go_repository(
        name = "com_github_stretchr_testify",
        importpath = "github.com/stretchr/testify",
        sum = "h1:nwc3DEeHmmLAfoZucVR881uASk0Mfjw8xYJ99tb5CcY=",
        version = "v1.7.0",
    )

    go_repository(
        name = "com_github_kr_pretty",
        importpath = "github.com/kr/pretty",
        sum = "h1:L/CwN0zerZDmRFUapSPitk6f+Q3+0za1rQkzVuMiMFI=",
        version = "v0.1.0",
    )

    go_repository(
        name = "com_github_kr_pty",
        importpath = "github.com/kr/pty",
        sum = "h1:VkoXIwSboBpnk99O/KFauAEILuNHv5DVFKZMBN/gUgw=",
        version = "v1.1.1",
    )

    go_repository(
        name = "com_github_kr_text",
        importpath = "github.com/kr/text",
        sum = "h1:45sCR5RtlFHMR4UwH9sdQ5TC8v0qDQCHnXt+kaKSTVE=",
        version = "v0.1.0",
    )

    go_repository(
        name = "in_gopkg_check_v1",
        importpath = "gopkg.in/check.v1",
        sum = "h1:YR8cESwS4TdDjEe65xsg0ogRM/Nc3DYOhEAlW+xobZo=",
        version = "v1.0.0-20190902080502-41f04d3bba15",
    )

    go_repository(
        name = "in_gopkg_yaml_v2",
        importpath = "gopkg.in/yaml.v2",
        sum = "h1:/eiJrUcujPVeJ3xlSWaiNi3uSVmDGBK1pDHUHAnao1I=",
        version = "v2.2.4",
    )

    go_repository(
        name = "in_gopkg_yaml_v3",
        importpath = "gopkg.in/yaml.v3",
        sum = "h1:dUUwHk2QECo/6vqA44rthZ8ie2QXMNeKRTHCNY2nXvo=",
        version = "v3.0.0-20200313102051-9f266ea9e77c",
    )

    # We use 1.1.0 because 1.2.0 has unresolved issues and I don't
    # feel like maintaining a fork for now.
    # https://github.com/sergi/go-diff/issues/123
    go_repository(
        name = "com_github_sergi_go_diff",
        importpath = "github.com/sergi/go-diff",
        sum = "h1:we8PVUC3FE2uYfodKH/nBHMSetSfHDR6scGdBi+erh0=",
        version = "v1.1.0",
    )

    go_repository(
        name = "com_github_yuin_goldmark",
        build_file_proto_mode = "disable_global",
        importpath = "github.com/yuin/goldmark",
        sum = "h1:3bajkSilaCbjdKVsKdZjZCLBNPL9pYzrCakKaf4U49U=",
        version = "v1.7.1",
    )
    go_repository(
        name = "com_github_yuin_goldmark_emoji",
        build_file_proto_mode = "disable_global",
        importpath = "github.com/yuin/goldmark-emoji",
        sum = "h1:ctuWEyzGBwiucEqxzwe0SOYDXPAucOrE9NQC18Wa1os=",
        version = "v1.0.1",
    )
    go_repository(
        name = "com_github_yuin_goldmark_highlighting_v2",
        build_file_proto_mode = "disable_global",
        importpath = "github.com/yuin/goldmark-highlighting/v2",
        sum = "h1:Py16JEzkSdKAtEFJjiaYLYBOWGXc1r/xHj/Q/5lA37k=",
        version = "v2.0.0-20220924101305-151362477c87",
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

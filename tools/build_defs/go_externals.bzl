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
    # _github("tdewolff/minify/v2", "f066c279bb780e9748f38259fe2b5c170028ce56"),
    # _github("tdewolff/parse/v2", "4c5fc37e223fe27c33dfe2e71651a3bd9f500e54"),
    # struct(
    #     name = "com_github_tdewolff_minify_v2",
    #     commit = "f066c279bb780e9748f38259fe2b5c170028ce56",
    #     importpath = "github.com/tdewolff/minify",
    #     remote = "https://github.com/tdewolff/minify",
    #     vcs = "git",
    # ),
    # struct(
    #     name = "com_github_tdewolff_parse_v2",
    #     commit = "4c5fc37e223fe27c33dfe2e71651a3bd9f500e54",
    #     importpath = "github.com/tdewolff/parse",
    #     remote = "https://github.com/tdewolff/parse",
    #     vcs = "git",
    # ),
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

    go_repository(
        name = "com_github_tdewolff_minify_v2",
        importpath = "github.com/tdewolff/minify/v2",
        sum = "h1:ZyvMKeciyR3vzJrK/oHyBcSmpttQ/V+ah7qOqTZclaU=",
        version = "v2.12.0",
    )

    go_repository(
        name = "com_github_tdewolff_parse_v2",
        importpath = "github.com/tdewolff/parse/v2",
        sum = "h1:RIfy1erADkO90ynJWvty8VIkqqKYRzf2iLp8ObG174I=",
        version = "v2.6.1",
    )

    go_repository(
        name = "com_github_tdewolff_test",
        importpath = "github.com/tdewolff/test",
        sum = "h1:8Vs0142DmPFW/bQeHRP3MV19m1gvndjUb1sn8yy74LM=",
        version = "v1.0.7",
    )

    go_repository(
        name = "com_github_cheekybits_is",
        importpath = "github.com/cheekybits/is",
        sum = "h1:SKI1/fuSdodxmNNyVBR8d7X/HuLnRpvvFO0AgyQk764=",
        version = "v0.0.0-20150225183255-68e9c0620927",
    )

    go_repository(
        name = "com_github_djherbis_atime",
        importpath = "github.com/djherbis/atime",
        sum = "h1:rgwVbP/5by8BvvjBNrbh64Qz33idKT3pSnMSJsxhi0g=",
        version = "v1.1.0",
    )

    go_repository(
        name = "com_github_dustin_go_humanize",
        importpath = "github.com/dustin/go-humanize",
        sum = "h1:VSnTsYCnlFHaM2/igO1h6X3HA71jcobQuxemgkq4zYo=",
        version = "v1.0.0",
    )

    go_repository(
        name = "com_github_fsnotify_fsnotify",
        importpath = "github.com/fsnotify/fsnotify",
        sum = "h1:jRbGcIw6P2Meqdwuo0H1p6JVLbL5DHKAKlYndzMwVZI=",
        version = "v1.5.4",
    )

    go_repository(
        name = "com_github_matryer_try",
        importpath = "github.com/matryer/try",
        sum = "h1:JAEbJn3j/FrhdWA9jW8B5ajsLIjeuEHLi8xE4fk997o=",
        version = "v0.0.0-20161228173917-9ac251b645a2",
    )

    go_repository(
        name = "com_github_spf13_pflag",
        importpath = "github.com/spf13/pflag",
        sum = "h1:iy+VFUOCP1a+8yFto/drg2CJ5u0yRoB7fZw3DKv/JXA=",
        version = "v1.0.5",
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

#include <gflags/gflags.h>
#include <stdio.h>
#include <dirent.h>

#include <string>

using std::string

// file's to search, for now just pacakge.json
string file_pat = "package.json";
string line_pat;
string base_path;

int main(int argc, char **argv) {

    if ((err = git_libgit2_init()) < 0)
        die("git_libgit2_init: %s", giterr_last()->message);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_BLOB, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_OFS_DELTA, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_REF_DELTA, 10*1024);



    const char* PATH = ".";

    DIR *dir = opendir(PATH);

    struct dirent *entry = readdir(dir);

    while (entry != NULL) {
        if (entry->d_type == DT_DIR) {

        }
    }

    return 0;

    // given a directory, a search pattern, and a file pattern
    // search every bare git repo in directory (including submobules)
    // for:
    //   1. files that match file pattern
    //   2. string matches of the search pattern
    // for matches:
    //   print out the line number and the file (without context, for now)

}

// search a git repo (and submodules) for all files that match file_pat (package.json)
// then, search all those files for line_pat
void search_git_repo(const char *repopath, const string& line_pat, const string& file_pat) {
    git_repository *curr_repo = NULL;

    int err = git_repository_open(&curr_repo, repopath);
    if (err < 0) {
        print_last_git_err_and_exit(err);
    }
    
    smart_object<git_commit> commit;
    smart_object<git_tree> tree;

    if (0 != git_revparse_single(commit, curr_repo, ("HEAD" + "^0").c_str())) {
        fprintf(stderr, "%s: ref HEAD not found, skipping (empty repo?)\n", name.c_str());
        return;
    }
    git_commit_tree(tree, commit);

    char oidstr[GIT_OID_HEXSZ+1];
    string version = FLAGS_revparse ?
        strdup(git_oid_tostr(oidstr, sizeof(oidstr), git_commit_id(commit))) : "HEAD";

    walk_tree("", FLAGS_order_root, repopath, walk_submodules, submodule_prefix, idx_tree, tree, curr_repo, 0);
}

bool is_package_json(string& file_name) {
    const string rev = "nosj.egakcap";
    int i = 0;

    if (file_name.length() != rev.length()) {
        return false;
    }

    for (string::reverse_iterator rit=file_name.rbegin(); rit!=file_name.rend(); ++rit) {
        if (*rit != rev[i])
            return false;
        if (i + 1 == rev.size())
            break;
        i += 1;
    }
    return true;
}

void walk_tree(git_tree *tree, git_repository *curr_repo, int depth) {

    int num_entries = git_tree_entrycount(tree);

    for (int i = 0; i < num_entries; ++i) {
        const git_tree_entry *ent = git_tree_entry_byindex(tree, i);
        git_object *obj;
        git_tree_entry_to_object(&obj, curr_repo, ent);

        const string path = pfx + git_tree_entry_name(ent);
        if (git_tree_entry_type(ent) == GIT_OBJ_TREE) {

        } else if (git_tree_entry_type(ent) == GIT_OBJ_BLOB && is_package_json(path)) {
            // read the blob that's stored in obj
            // use RE2 to search it for line_pat
            // options:
            // 1. Search line by line (thought shall not)
            // 2. Search the blob, if a match found, then look line by
            //    line......
            // I think I'm partial to #1 for the following reasons:
            //   * our search space is severely reduced already (only a few
            //   files per repo)
            //   * to print out line numbers, we need to loop through the file
            //   line by line anyways...
            // OR - use RE2 to search the whole blob, and given a search result,
            // count the number of newlines up to the search position...
            // lets do that ^ first
        } else if (git_tree_entry_type(ent) == GIT_OBJ_COMMIT) {
            // submodule, call walk_tree recursively
        }
    }
}


void search_blob(const string& path, StringPiece contents) {
 
    size_t len = contents.size();
    const char *p = contents.data();
    const char *end = p + len;
    const char *f;
    StringPiece line;

}


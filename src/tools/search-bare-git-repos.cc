#include <gflags/gflags.h>
#include <stdio.h>
#include <dirent.h>
#include "src/smart_git.h"
#include "src/codesearch.h"

#include "src/lib/metrics.h"
#include "src/lib/debug.h"

#include <string>

using std::string;

// file's to search, for now just pacakge.json
string file_pat = "package.json";
string line_pat;
string base_path;
std::atomic<int> next_repo_to_process_idx_(0);
std::vector<string> repos_to_walk_;

int get_next_repo_idx() {
    if (next_repo_to_process_idx_.load() == repos_to_walk_.size()) {
        return -1;
    }
    return next_repo_to_process_idx_.fetch_add(1, std::memory_order_relaxed);
}

int get_num_threads_to_use(int num_repos) {
    unsigned long const min_per_thread = 16; 
    unsigned long const max_threads = (num_repos + min_per_thread - 1) / min_per_thread;
    unsigned long const hardware_threads = std::thread::hardware_concurrency();
    unsigned long num_threads = std::min(hardware_threads, max_threads);

    if (num_threads == 1) {
        // 1 thread is pointless. It takes more time to spin up than indexing
        // would take.
        num_threads = 0;
    }

    fprintf(stderr, "length=%d min_per_thread=%lu max_threads=%lu hardware_threads=%lu num_threads=%lu\n", 
            num_repos, min_per_thread, max_threads, hardware_threads, num_threads);

    return num_threads;
}

struct repo_to_walk {
    const string path;
};

void walk_repos(int estimatedReposPerThread) {
    int idx_to_process = get_next_repo_idx();

    while (idx_to_process >= 0) {
        const string &repo_path = repos_to_walk_[idx_to_process];
        fprintf(stdout, "going to call search_git_repo on repo=%s\n", repo_path.c_str());
        idx_to_process = get_next_repo_idx();
    }
}

DEFINE_string(path, "", "Path containing bare git repos");

int main(int argc, char **argv) {
    gflags::SetUsageMessage("Usage: " + string(argv[0]) + " <options> REFS");
    gflags::ParseCommandLineFlags(&argc, &argv, true);

    int err;
    if ((err = git_libgit2_init()) < 0)
        die("git_libgit2_init: %s", giterr_last()->message);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_BLOB, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_OFS_DELTA, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_REF_DELTA, 10*1024);



    const char* PATH = FLAGS_path.c_str();

    fprintf(stdout, "path is: %s\n", PATH);

    DIR *dir = opendir(PATH);

    if (dir == NULL) {
        fprintf(stderr, "Error, directory passed is invalid. Use the --path flag to pass the dir\n");
        exit(1);
    }

    struct dirent *entry = readdir(dir);

    std::vector<string> repos_to_walk;

    while ((entry = readdir(dir)) != NULL) {
        if (entry->d_type != DT_DIR) {
            continue;
        }

        if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0)
            continue;

        fprintf(stdout, "found directory at path=%s\n", entry->d_name); 
        repos_to_walk.push_back(FLAGS_path + string(entry->d_name));
    }

    repos_to_walk_ = repos_to_walk;

    fprintf(stdout, "there are %lu git repos to walk under path: %s\n", repos_to_walk.size(), PATH);
    /* exit(0); */

    int num_threads = get_num_threads_to_use(repos_to_walk.size());

    if (num_threads == 0) {
        walk_repos(repos_to_walk_.size());
        return 0;
    }

    std::vector<std::thread> threads;
    threads.reserve(num_threads);
    int estimatedReposPerThread = repos_to_walk.size() / num_threads;
    for (int i = 0; i < num_threads; i++) {
        // figure out the chunk of work to do
        threads.emplace_back(&walk_repos, estimatedReposPerThread); 
    }
    // now, spin up x threads. Each thread will take a subset of the
    // repos_to_walk, and work in its piece. No work sharing for now. 

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
/* void search_git_repo(const char *repopath, const string& line_pat, const string& file_pat) { */
/*     git_repository *curr_repo = NULL; */

/*     int err = git_repository_open(&curr_repo, repopath); */
/*     if (err < 0) { */
/*         print_last_git_err_and_exit(err); */
/*     } */
    
/*     smart_object<git_commit> commit; */
/*     smart_object<git_tree> tree; */

/*     if (0 != git_revparse_single(commit, curr_repo, ("HEAD" + "^0").c_str())) { */
/*         fprintf(stderr, "%s: ref HEAD not found, skipping (empty repo?)\n", name.c_str()); */
/*         return; */
/*     } */
/*     git_commit_tree(tree, commit); */

/*     char oidstr[GIT_OID_HEXSZ+1]; */
/*     string version = FLAGS_revparse ? */
/*         strdup(git_oid_tostr(oidstr, sizeof(oidstr), git_commit_id(commit))) : "HEAD"; */

/*     walk_tree("", FLAGS_order_root, repopath, walk_submodules, submodule_prefix, idx_tree, tree, curr_repo, 0); */
/* } */

/* bool is_package_json(string& file_name) { */
/*     const string rev = "nosj.egakcap"; */
/*     int i = 0; */

/*     if (file_name.length() != rev.length()) { */
/*         return false; */
/*     } */

/*     for (string::reverse_iterator rit=file_name.rbegin(); rit!=file_name.rend(); ++rit) { */
/*         if (*rit != rev[i]) */
/*             return false; */
/*         if (i + 1 == rev.size()) */
/*             break; */
/*         i += 1; */
/*     } */
/*     return true; */
/* } */

/* void walk_tree(git_tree *tree, git_repository *curr_repo, int depth) { */

/*     int num_entries = git_tree_entrycount(tree); */

/*     for (int i = 0; i < num_entries; ++i) { */
/*         const git_tree_entry *ent = git_tree_entry_byindex(tree, i); */
/*         git_object *obj; */
/*         git_tree_entry_to_object(&obj, curr_repo, ent); */

/*         const string path = pfx + git_tree_entry_name(ent); */
/*         if (git_tree_entry_type(ent) == GIT_OBJ_TREE) { */

/*         } else if (git_tree_entry_type(ent) == GIT_OBJ_BLOB && is_package_json(path)) { */
/*             // read the blob that's stored in obj */
/*             // use RE2 to search it for line_pat */
/*             // options: */
/*             // 1. Search line by line (thought shall not) */
/*             // 2. Search the blob, if a match found, then look line by */
/*             //    line...... */
/*             // I think I'm partial to #1 for the following reasons: */
/*             //   * our search space is severely reduced already (only a few */
/*             //   files per repo) */
/*             //   * to print out line numbers, we need to loop through the file */
/*             //   line by line anyways... */
/*             // OR - use RE2 to search the whole blob, and given a search result, */
/*             // count the number of newlines up to the search position... */
/*             // lets do that ^ first */
/*         } else if (git_tree_entry_type(ent) == GIT_OBJ_COMMIT) { */
/*             // submodule, call walk_tree recursively */
/*         } */
/*     } */
/* } */


/* void search_blob(const string& path, StringPiece contents) { */
 
/*     size_t len = contents.size(); */
/*     const char *p = contents.data(); */
/*     const char *end = p + len; */
/*     const char *f; */
/*     StringPiece line; */

/* } */


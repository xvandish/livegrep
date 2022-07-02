#include <gflags/gflags.h>
#include <sstream>

#include "src/lib/metrics.h"
#include "src/lib/debug.h"
#include "src/lib/threadsafe_progress_indicator.h"

#include "src/codesearch.h"
#include "src/git_indexer.h"
#include "src/smart_git.h"
#include "src/score.h"

#include "src/proto/config.pb.h"

using namespace std;
using namespace std::chrono;

DEFINE_string(order_root, "", "Walk top-level directories in this order.");
DEFINE_bool(revparse, false, "Display parsed revisions, rather than as-provided");

git_indexer::git_indexer(code_searcher *cs,
                         const google::protobuf::RepeatedPtrField<RepoSpec>& repositories)
    : cs_(cs), repositories_to_index_(repositories), repositories_to_index_length_(repositories.size()) {
    int err;
    if ((err = git_libgit2_init()) < 0)
        die("git_libgit2_init: %s", giterr_last()->message);

    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_BLOB, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_OFS_DELTA, 10*1024);
    git_libgit2_opts(GIT_OPT_SET_CACHE_OBJECT_LIMIT, GIT_OBJ_REF_DELTA, 10*1024);
}

git_indexer::~git_indexer() {
    fprintf(stderr, "Closing open git repos\n");
    for (auto it = open_git_repos_.begin(); it != open_git_repos_.end(); ++it) {
        git_repository_free(*(it));
    }
    git_libgit2_shutdown();
}


// Used to get the next index of a repo a thread should focus on
int git_indexer::get_next_repo_idx() {
    if (next_repo_to_process_idx_ == repositories_to_index_length_) {
        return -1;
    }
    return next_repo_to_process_idx_++;
}

void git_indexer::print_last_git_err_and_exit(int err) {
    const git_error *e = giterr_last();
    printf("Error %d/%d: %s\n", err, e->klass, e->message);
    exit(1);
}

void git_indexer::process_repos(int estimatedReposToProcess, threadsafe_progress_indicator *tpi) {
    int idx_to_process = get_next_repo_idx();
    std::vector<git_repository *> open_git_repos_local;
    std::vector<std::unique_ptr<pre_indexed_file>> files_to_index_local;

    open_git_repos_local.reserve(estimatedReposToProcess);
    files_to_index_local.reserve(estimatedReposToProcess * 100);

    while (idx_to_process >= 0) {
        /* fprintf(stderr, "going to process: %d\n", idx_to_process); */
        /* auto start = high_resolution_clock::now(); */
        const auto &repo = repositories_to_index_[idx_to_process];
        const char *repopath = repo.path().c_str();

        /* fprintf(stderr, "walking repo: %s\n", repopath); */
        git_repository *curr_repo = NULL;

        // Is it safe to assume these are bare repos (or the mirror clones)
        // that we create?. If so, we can use git_repository_open_bare
        int err = git_repository_open_bare(&curr_repo, repopath);
        if (err < 0) {
            print_last_git_err_and_exit(err);
        }

        open_git_repos_local.push_back(curr_repo);

        for (auto &rev : repo.revisions()) {
            /* fprintf(stderr, "walking %s at %s \n", repopath, rev.c_str()); */
            walk(curr_repo, rev, repo.path(), repo.name(), repo.metadata(), repo.walk_submodules(), "", files_to_index_local);
            /* fprintf(stderr, "done walking %s at %s\n", repopath, rev.c_str()); */
            tpi->tick();
        }
        /* auto stop = high_resolution_clock::now(); */
        /* auto duration = duration_cast<seconds>(stop - start); */
        /* cout << "took: " << duration.count() << " seconds to process_repos" << endl; */

        idx_to_process = get_next_repo_idx();
    }

    std::lock_guard<std::mutex> guard(files_mutex_);
    files_to_index_.insert(files_to_index_.end(), std::make_move_iterator(files_to_index_local.begin()), std::make_move_iterator(files_to_index_local.end()));
    open_git_repos_.insert(open_git_repos_.end(), open_git_repos_local.begin(), open_git_repos_local.end());
}


void git_indexer::begin_indexing() {

    // min_per_thread will require tweaking. For example, even with only
    // 2 repos, would it not be worth it to spin up two threads (if available)?
    // Or would the overhead of the thread creation far outweigh the single-core
    // performance for just a few repos.
    
    unsigned long const length = repositories_to_index_.size();
    unsigned long const min_per_thread = 16; 
    unsigned long const max_threads = 
        (length + min_per_thread - 1)/min_per_thread;
    unsigned long const hardware_threads = std::thread::hardware_concurrency();
    unsigned long const num_threads = std::min(hardware_threads!=0?hardware_threads:2, max_threads);

    fprintf(stderr, "length=%lu min_per_thread=%lu max_threads=%lu hardware_threads=%lu num_threads=%lu\n", length, min_per_thread, max_threads, hardware_threads, num_threads);

    threadsafe_progress_indicator tpi(length, "Walking repos...", "Done");

    if (length < 2 * min_per_thread) {
        fprintf(stderr, "Not going to create any new threads.\n");
        process_repos(length, &tpi);
        index_files();
        return;
    }

    int estimatedReposPerThread = length / num_threads;
    threads_.reserve(num_threads - 1);
    auto start = high_resolution_clock::now();
    for (long i = 0; i < num_threads; ++i) {
        threads_.emplace_back(&git_indexer::process_repos, this, estimatedReposPerThread, &tpi);
    }

    for (long i = 0; i < num_threads; ++i) {
        threads_[i].join();
    }

    auto stop = high_resolution_clock::now();
    auto duration = duration_cast<milliseconds>(stop - start);
    cout << "took: " << duration.count() << " milliseconds to process_repos" << endl;
    /* exit(0); */

    index_files();
}

bool compareFiles(const std::unique_ptr<pre_indexed_file>& a, const std::unique_ptr<pre_indexed_file>& b) {
    return a->score > b->score;
}

// sorts `files_to_index_` based on score. This way, the lowest scoring files
// get indexed last, and so show up in sorts results last. We use a stable sort
// so that most of a repos files are indexed together which has 2 benefits:
//  1. If a term is matched in a lot of repos files, that repos files will end
//     up grouped together in search results
//  2. We can avoid many calls to git_repository_open/git_repository_free. At
//     least until the end, where many repos "low" scoring files are found.
// walks `files_to_index_`, looks up the repo & blob combination for each file
// and then calls `cs->index_file` to actually index the file.
void git_indexer::index_files() {
    fprintf(stderr, "sorting files_to_index_... [%lu]\n", files_to_index_.size());
    std::stable_sort(files_to_index_.begin(), files_to_index_.end(), compareFiles);
    fprintf(stderr, "  done\n");

    /* fprintf(stderr, "walking files_to_index_ ...\n"); */
    threadsafe_progress_indicator tpi(files_to_index_.size(), "Indexing files_to_index_...", "Done");
    for (auto it = files_to_index_.begin(); it != files_to_index_.end(); ++it) {
        auto file = it->get();

        git_blob *blob;
        int err = git_blob_lookup(&blob, file->repo, file->oid);

        if (err < 0) {
            print_last_git_err_and_exit(err);
        }
        
        const char *data = static_cast<const char*>(git_blob_rawcontent(blob));
        cs_->index_file(file->tree, file->path, StringPiece(data, git_blob_rawsize(blob)));

        git_blob_free(blob);
        tpi.tick();
    }
}

void git_indexer::walk(git_repository *curr_repo,
        const string& ref, 
        const string& repopath, 
        const string& name,
        Metadata metadata,
        bool walk_submodules,
        const string& submodule_prefix,
        std::vector<std::unique_ptr<pre_indexed_file>>& results) {
    smart_object<git_commit> commit;
    smart_object<git_tree> tree;
    if (0 != git_revparse_single(commit, curr_repo, (ref + "^0").c_str())) {
        fprintf(stderr, "%s: ref %s not found, skipping (empty repo?)\n", name.c_str(), ref.c_str());
        return;
    }
    git_commit_tree(tree, commit);
    /* fprintf(stderr, "opened commit_tree for %s\n", repopath.c_str()); */

    char oidstr[GIT_OID_HEXSZ+1];
    string version = FLAGS_revparse ?
        strdup(git_oid_tostr(oidstr, sizeof(oidstr), git_commit_id(commit))) : ref;

    const indexed_tree *idx_tree = cs_->open_tree(name, metadata, version);
    walk_tree("", FLAGS_order_root, repopath, walk_submodules, submodule_prefix, idx_tree, tree, curr_repo, results);
}


void git_indexer::walk_tree(const string& pfx,
                            const string& order,
                            const string& repopath,
                            bool walk_submodules,
                            const string& submodule_prefix,
                            const indexed_tree *idx_tree,
                            git_tree *tree,
                            git_repository *curr_repo,
                            std::vector<std::unique_ptr<pre_indexed_file>>& results) {
    /* fprintf(stderr, "preparing to walk_tree for %s with prefix: %s \n", repopath.c_str(), pfx.c_str()); */
    map<string, const git_tree_entry *> root;
    vector<const git_tree_entry *> ordered;
    int entries = git_tree_entrycount(tree);
    /* fprintf(stderr, "repo: %s has %d entries\n", repopath.c_str(), entries); */
    for (int i = 0; i < entries; ++i) {
        const git_tree_entry *ent = git_tree_entry_byindex(tree, i);
        root[git_tree_entry_name(ent)] = ent;
    }

    istringstream stream(order);
    string dir;
    while(stream >> dir) {
        map<string, const git_tree_entry *>::iterator it = root.find(dir);
        if (it == root.end())
            continue;
        ordered.push_back(it->second);
        root.erase(it);
    }
    for (map<string, const git_tree_entry *>::iterator it = root.begin();
         it != root.end(); ++it)
        ordered.push_back(it->second);
    for (vector<const git_tree_entry *>::iterator it = ordered.begin();
         it != ordered.end(); ++it) {
        
        smart_object<git_object> obj;
        git_tree_entry_to_object(obj, curr_repo, *it);
        string path = pfx + git_tree_entry_name(*it);

        /* fprintf(stderr, "walking obj with path: %s/%s\n", repopath.c_str(), path.c_str()); */

        if (git_tree_entry_type(*it) == GIT_OBJ_TREE) {
            walk_tree(path + "/", "", repopath, walk_submodules, submodule_prefix, idx_tree, obj, curr_repo, results);
        } else if (git_tree_entry_type(*it) == GIT_OBJ_BLOB) {

            const string full_path = submodule_prefix + path;

            auto file = std::make_unique<pre_indexed_file>();

            file->tree = idx_tree;
            file->repopath = repopath;
            file->path = path;
            file->score = score_file(full_path);
            file->repo = curr_repo;
            file->oid = (git_oid *)malloc(sizeof(git_oid));
            git_oid_cpy(file->oid, git_blob_id(obj));

            results.push_back(std::move(file));
        } else if (git_tree_entry_type(*it) == GIT_OBJ_COMMIT) {
            // Submodule
            if (!walk_submodules) {
                continue;
            }
            git_submodule* submod = nullptr;
            if (0 != git_submodule_lookup(&submod, curr_repo, path.c_str())) {
                fprintf(stderr, "Unable to get submodule entry for %s, skipping\n", path.c_str());
                continue;
            }

            const char* sub_name = git_submodule_name(submod);
            string sub_repopath = repopath + "/" + path;
            string new_submodule_prefix = submodule_prefix + path + "/";
            Metadata meta;

            const git_oid* rev = git_tree_entry_id(*it);
            char revstr[GIT_OID_HEXSZ + 1];
            git_oid_tostr(revstr, GIT_OID_HEXSZ + 1, rev);

            // Open the submodule repo
            git_repository *sub_repo;

            int err = git_repository_open_bare(&sub_repo, sub_repopath.c_str());

            if (err < 0) {
                fprintf(stderr, "Unable to open subrepo: %s\n", sub_repopath.c_str());
                print_last_git_err_and_exit(err);
            }

            walk(sub_repo, string(revstr), sub_repopath, string(sub_name), meta, walk_submodules, new_submodule_prefix, results);

            // TODO: See if this is efficient enough. We're depending on
            // libgi2's shutdown to close these submodule repos, while we
            // manually close those that we append to open_git_repos_.
            /* git_repository_free(sub_repo); */
        }
    }
}

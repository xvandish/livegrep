/********************************************************************
 * livegrep -- git_indexer.h
 * Copyright (c) 2011-2013 Nelson Elhage
 *
 * This program is free software. You may use, redistribute, and/or
 * modify it under the terms listed in the COPYING file.
 ********************************************************************/
#ifndef CODESEARCH_GIT_INDEXER_H
#define CODESEARCH_GIT_INDEXER_H

#include <string>
#include "src/proto/config.pb.h"
#include "src/smart_git.h"
#include "src/lib/thread_queue.h"

class code_searcher;
class git_repository;
class git_tree;
struct indexed_tree;

// We walk all repos and create a vector of pre_indexed_file(s)
// We then sort that vector by score (and repo name)
// In that way, low scoring files are indexed at the back of a chunk,
// or are contained mostly in the last chunks, so higher ranked files
// appear sooner in search results, since we search by chunk.
struct pre_indexed_file {
    const indexed_tree *tree;
    std::string  repopath;
    std::string  path;
    int score;
    git_repository *repo;
    git_oid *oid;
};

// used to walk a repos folders/trees in parallel using threads
struct tree_to_walk {
    std::string prefix;
    std::string repopath;
    bool walk_submodules;
    string submodule_prefix;
    const indexed_tree *idx_tree;
    git_tree *tree;
    git_repository *repo;
};

class git_indexer {
public:
    git_indexer(code_searcher *cs,
                const google::protobuf::RepeatedPtrField<RepoSpec>& repositories);
    ~git_indexer();
    void index_repos();
protected:
    void process_trees();
    void walk(git_repository *curr_repo,
            const std::string& ref,
            const std::string& repopath,
            const std::string& name,
            Metadata metadata,
            bool walk_submodules,
            const std::string& submodule_prefix);
    void walk_tree(const std::string& pfx,
                   const std::string& order,
                   const std::string& repopath,
                   bool walk_submodules,
                   const std::string& submodule_prefix,
                   const indexed_tree *idx_tree,
                   git_tree *tree,
                   git_repository *curr_repo,
                   int depth);
    void index_files();
    void print_last_git_err_and_exit(int err);
    int get_num_threads_to_use();

    code_searcher *cs_;
    const google::protobuf::RepeatedPtrField<RepoSpec>& repositories_to_index_;
    const int repositories_to_index_length_;
    bool is_singlethreaded_;
    std::vector<std::thread> threads_;
    thread_queue<tree_to_walk*> trees_to_walk_;

    std::mutex files_mutex_;
    std::vector<pre_indexed_file*> files_to_index_;
    std::vector<git_repository *> open_git_repos_;
};

#endif

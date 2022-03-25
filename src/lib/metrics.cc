#include "metrics.h"
#include <string.h>

#include <stdlib.h>
#include <string>
#include <map>
#include <mutex>

namespace {
    std::mutex metrics_mtx;
    std::map<std::string, metric*> *metrics;
};


metric::metric(const std::string &name) {
    std::unique_lock<std::mutex> locked(metrics_mtx);
    if (metrics == 0)
        metrics = new std::map<std::string, metric*>;
    (*metrics)[name] = this;
}


void metric::dump_all() {
    fprintf(stderr, "== begin metrics ==\n");
    for (auto it = metrics->begin(); it != metrics->end(); ++it) {
        fprintf(stderr, "%s %ld\n", it->first.c_str(), it->second->val_.load());
    }
    fprintf(stderr, "== end metrics ==\n");
}

void metric::dump_all_to_file(const std::string &filePath, int elapsedSec, int elapsedUSec) {
    FILE * pFile; 
    pFile = fopen(filePath.c_str(), "w");
    if (pFile == NULL) {
        std::string errMsg = "Cannot open " + filePath;
        perror(errMsg.c_str());
        exit(1);
    }

    fprintf(pFile, "repository indexed in %d.%06ds\n", elapsedSec, elapsedUSec);
    fprintf(pFile, "== begin metrics ==\n");
    for (auto it = metrics->begin(); it != metrics->end(); ++it) {
        fprintf(pFile, "%s %ld\n", it->first.c_str(), it->second->val_.load());
    }
    fprintf(pFile, "== end metrics ==\n");
}

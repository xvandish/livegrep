#include <stdio.h>
#include <string.h>
#include <iostream>
#include <string>
#include <chrono>

#include "re2/re2.h"
// #include "re2/walker-inl.h"

#include "src/literal_search.h"
#include "src/codesearch.h"
#include "utf8.h"

using re2::RE2;
using re2::Regexp;
using re2::StringPiece;
using namespace re2;
using namespace std;
using namespace std::chrono;

#define d 10

// implementation of rabinKarp as found in CLRS. Rather than returning after the first match,
// we keep going until we've consumed haystack as much as possible.
// I've renamed some variables for clarity, and left those that make comparing this algo
// with CLRS easy as-is.
std::vector<match_bound> LiteralSearcher::rabinKarp(StringPiece haystack, StringPiece needle, int q) {
    int m = needle.length();
    int n = haystack.length();
    int needleHash = 0;
    int haystackHash = 0; // rolling hash
    int h = 1; // highest order thing

    // compute highest order digit position
    for (int i = 0; i < m - 1; i++)
        h = (h * d) % q;

    // compute hash value for haystack and needle
    for (int i = 0; i < m; i++) {
        needleHash = (d * needleHash + needle[i]) % q;
        haystackHash = (d * haystackHash + haystack[i]) % q;
    }

    vector<match_bound> bounds;
    // now, search for matches
    for (int i = 0; i <= n - m; i++) {
        if (needleHash == haystackHash && memcmp(haystack.data()+i, needle.data(), m) == 0) {
            match_bound mb;
            mb.matchleft = i;
            mb.matchright = i + utf8::distance(needle.data(), needle.data() + m);
            bounds.push_back(mb);
        }

        if (i < n - m) {
            haystackHash = (d * (haystackHash - haystack[i] * h) + haystack[i + m]) %q;
            if (haystackHash < 0) {
                haystackHash = (haystackHash + q);
            }
        }
    }

    return bounds;
}

std::vector<match_bound> LiteralSearcher::getMatchBounds(StringPiece haystack, StringPiece needle) {
    // eventually, split out to other fast string search methods
    // for now though, just use rk
    return rabinKarp(haystack, needle, 31);
}



// int main(int argc, char *argv[])
// {
//     LiteralSearcher ls;
//     fprintf(stderr, "hello\n");

//     StringPiece searchLine = StringPiece("Reads CSV files in gs://int.nyt.com/data/covid-19/hhs-scrapes/ and creates timeseries CSV files for hospitalizations, hospital admissions, and tests. Those CSV files are written out by gcf-hhs-version-files");

//     StringPiece needle = StringPiece("e");


//     // int equal = memcmp(searchLine.data(), needle.data(), needle.length());

//     // fprintf(stderr, "moved is: %s\n", searchLine.data()+5);

//     // fprintf(stderr, "parts equal = %d. Via inline functions =%d\n", equal == 0, searchLine.starts_with(needle));

//     int q = 31;
//     fprintf(stderr, "size of thing is: %lu\n", searchLine.length());

//     auto start = high_resolution_clock::now();
//     auto bounds = ls.getMatchBounds(searchLine, needle);
//     auto stop = high_resolution_clock::now();
//     auto duration = duration_cast<microseconds>(stop - start);
//     fprintf(stderr, "took %lld microseconds for StringPiece RK\n", duration.count());

//     for (int i = 0; i < bounds.size(); i++) {
//         auto bound = bounds[i];
//         fprintf(stderr, "matchleft=%d matchright=%d\n", bound.matchleft, bound.matchright);
//     }

//     // We'd like to find both instances of "test" above, and return their bounds

//     // there are various rules, depending on the needle and the haystack
//     // needle rules
//     // needle <=2 chars = simple memchar
//     // needle <= 18hars = ayo-carstick
//     // haystack <= chars =  rabin-karp

//     // I'm not sure

//     return 0;
// }

// given a literal string
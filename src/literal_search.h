/********************************************************************
 * livegrep -- literal_search.h
 * Copyright (c) 2022 Rodrigo Silva Mendoza
 *
 * This program is free software. You may use, redistribute, and/or
 * modify it under the terms listed in the COPYING file.
 ********************************************************************/

#include "re2/re2.h"
#include "re2/walker-inl.h"

#include "src/codesearch.h"


using re2::StringPiece;
using namespace re2;

// this class is used to perform rapid substring searches for text that can be searched literally
class LiteralSearcher {
public:
    // the main entry into this file. getMatchBounds will decide which string function to call
    std::vector<match_bound> getMatchBounds(StringPiece haystack, StringPiece needle);

private:
    std::vector<match_bound> rabinKarp(StringPiece haystack, StringPiece needle, int q);
    std::vector<match_bound> oneByte(StringPiece haystack, StringPiece needle);
};

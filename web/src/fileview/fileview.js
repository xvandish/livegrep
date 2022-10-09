var KeyCodes = {
  ESCAPE: 27,
  ENTER: 13,
  DOT: 190,
  SLASH_OR_QUESTION_MARK: 191,
};

function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}

function getOffset(element) {
  if (!element.getClientRects().length) {
    return { top: 0, left: 0 };
  }

  var rect = element.getBoundingClientRect();
  var win = element.ownerDocument.defaultView;
  return {
    top: rect.top + win.pageYOffset,
    left: rect.left + win.pageXOffset,
  };
}

// Here we need some JS that we can probably abstract out
function scrollToRange(range, elementContainer) {
  // - If we have a single line, scroll the viewport so that the element is
  // at 1/3 of the viewport.
  // - If we have a range, try and center the range in the viewport
  // - If the range is to high to fit in the viewport, fallback to the single
  //   element scenario for the first line
  var viewport = window;
  var viewportHeight = viewport.innerHeight;

  var scrollOffset = Math.floor(viewportHeight / 3.0);

  var firstLineElement = root.querySelector("#L" + range.start);
  if (!firstLineElement) {
    // We were given a scroll offset to a line number that doesn't exist in the page, bail
    return;
  }
  if (range.start != range.end) {
    // We have a range, try and center the entire range. If it's to high
    // for the viewport, fallback to revealing the first element.
    var lastLineElement = elementContainer.querySelector("#L" + range.end);
    var rangeHeight =
      getOffset(lastLineElement).top +
      lastLineElement.clientHeight -
      getOffset(firstLineElement).top;
    if (rangeHeight <= viewportHeight) {
      // Range fits in viewport, center it
      scrollOffset = 0.5 * (viewportHeight - rangeHeight);
    } else {
      scrollOffset = firstLineElement.clientHeight / 2; // Stick to (almost) the top of the viewport
    }
  }

  // viewport.scrollTop(firstLineElement.offset().top - scrollOffset);
  viewport.scrollTo({ top: getOffset(firstLineElement).top - scrollOffset });
}

function setHash(hash) {
  if (history.replaceState) {
    history.replaceState(null, null, hash);
  } else {
    location.hash = hash;
  }
}

function parseHashForLineRange(hashString) {
  var parseMatch = hashString.match(/#L(\d+)(?:-L?(\d+))?/);

  if (parseMatch && parseMatch.length === 3) {
    // We have a match on the regex expression
    var startLine = parseInt(parseMatch[1], 10);
    var endLine = parseInt(parseMatch[2], 10);
    if (isNaN(endLine) || endLine < startLine) {
      endLine = startLine;
    }
    return {
      start: startLine,
      end: endLine,
    };
  }

  return null;
}

function addHighlightClassesForRange(range, root) {
  for (var lineNumber = range.start; lineNumber <= range.end; lineNumber++) {
    root.querySelector("#LC" + lineNumber).classList.add("highlighted");
  }
}

function expandRangeToElement(element) {
  var range = parseHashForLineRange(document.location.hash);
  if (range) {
    var elementLine = parseInt(element.getAttribute("id").replace("L", ""), 10);
    if (elementLine < range.start) {
      range.end = range.start;
      range.start = elementLine;
    } else {
      range.end = elementLine;
    }
    setHash("#L" + range.start + "-" + "L" + range.end);
  }
}

function doSearch(query, newTab, scopeToRepo) {
  var url = new URL("/search", window.location.href);

  if (query !== null && query !== "") {
    url.searchParams.set("q", query);
  }

  if (scopeToRepo) {
    var repoTerm = "repo:" + fullRepoName;
    url.searchParams.set("q", repoTerm + " " + query);
  }

  if (newTab === true) {
    window.open(url);
  } else {
    window.location.href = url.toString();
    if (query && query !== "") {
      url =
        "/search?q=" +
        encodeURIComponent(query) +
        "&repo=" +
        encodeURIComponent(fullRepoName);
    } else {
      url = "/search";
    }
  }
}

function handleHashChange(scrollElementIntoView) {
  if (scrollElementIntoView === undefined) {
    scrollElementIntoView = true; // default if nothing was provided
  }

  // Clear current highlights
  lineNumberContainer.querySelectorAll(".highlighted").forEach(function (elem) {
    elem.classList.remove("highlighted");
  });

  // Highlight the current range from the hash, if any
  var range = parseHashForLineRange(document.location.hash);
  if (range) {
    addHighlightClassesForRange(range, lineNumberContainer);
    if (scrollElementIntoView) {
      scrollToRange(range, root);
    }
  }

  // Update the external-browse link
  document
    .getElementById("external-link")
    .setAttribute("href", getExternalLink(range));
  updateFragments(range, document.querySelectorAll("#permalink, #backToHead"));
}

function getLineNumber(range) {
  if (range == null) {
    // Default to first line if no lines are selected.
    return 1;
  } else if (range.start == range.end) {
    return range.start;
  } else {
    // We blindly assume that the external viewer supports linking to a
    // range of lines. Github doesn't support this, but highlights the
    // first line given, which is close enough.
    return range.start + "-L" + range.end;
  }
}

function getExternalLink(range) {
  var lno = getLineNumber(range);

  var repoName = fullRepoName;
  var transformedFilePath = filePath;

  var url = urlPattern;

  // If url not found, warn user and fail gracefully
  if (!url) {
    // deal with both undefined and empty string
    console.error(
      "The index file you provided does not provide repositories[x].metadata.url_pattern. External links to file sources will not work. See the README for more information on file viewing."
    );
    return;
  }

  // if we are at the root of the repo
  // TODO(xvandish): respect permalinks (specific commits of dirs)
  if (transformedFilePath == "") {
    if (githubUrl != "") { 
      return githubUrl;
    }

    // otherwise, take the url pattern, then split off everthing after '{name}/'
    // e.g "https://github.com/{name}/blob/{version}/{path}#L{lno}" -> "https://github.com/{name}/"
    var idx = url.indexOf("{name}/");  
    var cutStr = url.substring(0, idx+7); // idx + len of search term
    url = cutStr.replace("{name}", repoName);
    return url;
  }

  // If {path} already has a slash in front of it, trim extra leading
  // slashes from `pathInRepo` to avoid a double-slash in the URL.
  if (url.indexOf("/{path}") !== -1) {
    transformedFilePath = transformedFilePath.replace(/^\/+/, "");
  }

  // XXX code copied
  url = url.replace("{lno}", lno);
  url = url.replace("{version}", commit);
  url = url.replace("{name}", repoName);
  url = url.replace("{path}", transformedFilePath);
  return url;
}

function updateFragments(range, anchors) {
  anchors.forEach(function (anchor) {
    var href = anchor.getAttribute("href").split("#")[0];
    if (range !== null) {
      href += "#L" + getLineNumber(range);
    }
    anchor.setAttribute("href", href);
  });
}

function processKeyEvent(event) {
  var selectedText = getSelectedText();
  if (event.which === KeyCodes.ENTER) {
    // Perform a new search with the selected text, if any
    if (selectedText) {
      doSearch(selectedText, true, false);
    }
  } else if (event.which === KeyCodes.SLASH_OR_QUESTION_MARK) {
    event.preventDefault();
    if (event.shiftKey) {
      showHelp();
    } else {
      hideHelp();
      doSearch(selectedText, false, false);
    }
  } else if (event.which === KeyCodes.DOT) {
    event.preventDefault();
    doSearch(selectedText, false, true);
  } else if (event.which === KeyCodes.ESCAPE) {
    // Avoid swallowing the important escape key event unless we're sure we want to
    if (!helpScreen.classList.contains("hidden")) {
      event.preventDefault();
      hideHelp();
    }
  } else if (String.fromCharCode(event.which) == "V") {
    // Visually highlight the external link to indicate what happened
    var externalLink = document.getElementById("external-link");
    externalLink.focus();
    window.location.href = externalLink.getAttribute("href");
  } else if (String.fromCharCode(event.which) == "Y") {
    var permalinkLink = document.getElementById("permalink");
    if (permalinkLink) {
      permalinkLink.focus();
      // get the current url
      var curr = window.location.href;
      var searchingFor = "/blob/"+commit+"/";
      var replacingWith = "/blob/"+commitHash+"/";
      curr = curr.replace(searchingFor, replacingWith);

      window.location.href = curr;
    }
  } else if (
    String.fromCharCode(event.which) == "N" ||
    String.fromCharCode(event.which) == "P"
  ) {
    var goBackwards = String.fromCharCode(event.which) === "P";
    var selectedText = getSelectedText();
    if (selectedText) {
      window.find(selectedText, false /* case sensitive */, goBackwards);
    }
  } else if (String.fromCharCode(event.which) == 'H') {
    var logLink = document.getElementById("commit-history");
    logLink.focus();
    window.location = logLink.getAttribute("href");
  }
  return true;
}

function initializeActionButtons() {
  // Map out action name to function call, and automate the details of actually hooking
  // up the event handling.
  var ACTION_MAP = {
    searchGlobally: function () {
      return doSearch(null, false, false);
    },
    searchInCurrRepo: function () {
      return doSearch(null, false, true);
    },
    help: showHelp,
  };

  for (var actionName in ACTION_MAP) {
    document
      .querySelector("a[data-action-name=" + actionName + "]")
      .addEventListener(
        "click",
        // We can't use the action mapped handler directly here since the iterator (`actioName`)
        // will keep changing in the closure of the inline function.
        // Generating a click handler on the fly removes the dependency on closure which
        // makes this work as one would expect. #justjsthings.
        (function (handler) {
          return function (event) {
            event.preventDefault();
            event.stopImmediatePropagation(); // Prevent immediately closing modals etc.
            handler.call(this, event);
          };
        })(ACTION_MAP[actionName])
      );
  }
}

var showSelectionReminder = function () {
  document.getElementsByClassName("without-selection")[0].style.display =
    "none";
  document.getElementsByClassName("with-selection")[0].style.display = "block";
};

var hideSelectionReminder = function () {
  document.getElementsByClassName("without-selection")[0].style.display =
    "block";
  document.getElementsByClassName("with-selection")[0].style.display = "none";
};

function initializePage(initData) {
  console.log({ initData });
  urlPattern = initData.repo_info.metadata["url_pattern"];
  githubUrl = initData.repo_info.metadata["github"];
  fullRepoName = initData.repo_info.name;
  filePath = initData.file_path;
  commit = initData.commit;
  commitHash = initData.commit_hash;

  root = document.getElementsByClassName("file-content")[0];
  lineNumberContainer = document.querySelector(".file-content");
  helpScreen = document.getElementsByClassName("help-screen")[0];

  // The native browser handling of hashes in the location is to scroll
  // to the element that has a name matching the id. We want to prevent
  // this since we want to take control over scrolling ourselves, and the
  // most reliable way to do this is to hide the elements until the page
  // has loaded. We also need defer our own scroll handling since we can't
  // access the geometry of the DOM elements until they are visible.

  if (lineNumberContainer) {
    // Initial range detection for when the page is loaded
    handleHashChange(true);
    // Allow shift clicking links to expand the highlight range
    // rather than adding an event handler to all links, we just
    // add a handler to the container, then check if the target
    // is a link
    lineNumberContainer.addEventListener("click", function (event) {
      if (!event.target.classList.contains("lno")) {
        return;
      }
      event.preventDefault();
      if (event.shiftKey) {
        expandRangeToElement(event.target);
      } else {
        setHash("#" + event.target.getAttribute("id"));
      }
      handleHashChange(false);
    });
    window.addEventListener("hashchange", function (event) {
      event.preventDefault();
      handleHashChange(true);
    });
  } else {
    document
      .getElementById("external-link")
      .setAttribute("href", getExternalLink(null));
  }

  initializeActionButtons();

  window.document.addEventListener("keydown", function (e) {
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    processKeyEvent(e);
  });

  window.document.addEventListener("selectionchange", function () {
    var selectedText = getSelectedText();
    if (selectedText) {
      showSelectionReminder();
    } else {
      hideSelectionReminder();
    }
  });

  window.document.addEventListener("click", function (event) {
    var helpScreenCard = document.querySelector(".help-screen-card");
    if (
      !helpScreen.classList.contains("hidden") &&
      !helpScreenCard.contains(event.target)
    ) {
      // check against card, not overlay
      hideHelp();
    }
  });
}

function showHelp() {
  helpScreen.classList.remove("hidden");
}

function hideHelp() {
  helpScreen.classList.add("hidden");
}

var initData;
var urlPattern;
var githubUrl;
var commit;
var commitHash;
var fullRepoName;
var filePath;

var lineNumberContainer;
var root;
var helpScreen;

module.exports = {
  init: initializePage,
};

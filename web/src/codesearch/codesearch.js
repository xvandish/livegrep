function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}

var searchBox;
var errorsBox;
var resultsContainer;
var helpArea;
var caseSelect;
var regexToggle;

var liveDot;
var liveText;

var searchOptions = {
  q: "",
  regex: false,
  context: true, // we don't have an option for disabling context. No one uses it
  case: "auto",
};

function livePoll() {
  var url = "/api/v1/bkstatus/";
  setInterval(function () {
    // Don't make network requests while tab is in background
    if (document.hidden) return;
    fetch(url)
      .then(function (r) {
        return r.text();
      })
      .then(function (text) {
        // closure land
        // if (text === lastText) return;

        var split = text.split(",");
        var status = split[0];
        if (status === "0") {
          // TODO: If the indexTime here is different than what we have in
          // state, then add a "reload" button

          liveDot.dataset.status = "up";
          liveText.innerText = "Connected. Index age: " + split[1];
        } else {
          var timeDown = split[1];

          // https://pkg.go.dev/google.golang.org/grpc@v1.45.0/codes#Code
          if (status === "14") {
            liveDot.dataset.status = "reloading";
            liveText.innerText = "Index reloading.. (" + timeDown + ")";
            return;
          }
          // Some other unhandled grpc issue
          liveDot.dataset.status = "down";
          liveText.innerText = "Disconnected. (" + timeDown + ")";
        }
      })
      .catch(function (error) {
        console.log(error);
        liveDot.dataset.status = "down";
        liveText.innerText = "Disconnected";
        liveSubInfo.innerText = "No connection to webserver";
      });
  }, 1000);
}

var currUrl;
var lastUrlUpdate;
var two_seconds = 2000;
// We could maybe rethink this to be a function that only updates
// the searchapram for the changed option. Ok for now tho
function updateSearchParamState() {
  currUrl = new URL(window.location);

  var sp = currUrl.searchParams;

  sp.set("q", encodeURIComponent(searchOptions.q));
  sp.set("regex", searchOptions.regex);
  sp.set("fold_case", searchOptions.case);

  // If the user is typing quickly, just keep replacing the
  // current URL.  But after they've paused, enroll the URL they
  // paused at into their browser history.
  var now = Date.now();
  if (now - lastUrlUpdate > two_seconds) {
    window.history.pushState({}, "", currUrl);
  } else {
    history.replaceState(null, "", currUrl);
  }
  lastUrlUpdate = now;

  doSearch();
}

// Take the present search options, perform a search
// then update the
var urlFetching;
function doSearch() {
  if (searchOptions.q === "") {
    helpArea.style.display = "initial";
    resultsContainer.innerHTML = "";
    errorsBox.style.display = "none";
    return;
  }
  var time1 = performance.now();
  var time2;
  var urlToFetch =
    "/api/v2/getRenderedSearchResults/?q=" +
    searchOptions.q +
    "&fold_case=" +
    searchOptions.case +
    "&regex=" +
    searchOptions.regex +
    "&context=" +
    searchOptions.context;
  urlFetching = urlToFetch;
  fetch(urlToFetch)
    .then(function (r) {
      time2 = performance.now();
      if (!r.ok) {
        return Promise.reject(r.text());
      } else {
        return r.text();
      }
    })
    .then(function (text) {
      if (urlToFetch != urlFetching) {
        // don't display if the results are for an old query. This is hacky,
        // but works just fine. TODO: use an abortController and fancier
        // ids to keep track of in-flight searches
        return;
      }
      helpArea.style.display = "none";
      resultsContainer.innerHTML = text;
      errorsBox.style.display = "none";

      var timeTaken = ((time2 - time1) / 1000).toFixed(4);
      document.getElementById("searchtime").innerText = timeTaken + "s";
      document.getElementById("searchtimebox").style.display = "initial";
    })
    .catch(function (err) {
      err.then(function (errText) {
        // display the help area, clear previous results, and show the new error
        helpArea.style.display = "initial";
        resultsContainer.innerHTML = "";
        errorsBox.querySelector("#errortext").innerText = errText;
        errorsBox.style.display = "initial";
      });
    });
}

function updateQuery(inputEvnt) {
  searchOptions.q = inputEvnt.target.value;
  updateSearchParamState();
}

function toggleControlButton(button) {
  var currValue = button.getAttribute("data-selected") === "true";
  button.setAttribute("data-selected", !currValue);
  searchOptions[button.getAttribute("name")] = !currValue;
  updateSearchParamState();
}

function hasParams() {
  var url = new URL(document.location);
  var sp = url.searchParams;

  return sp.has("q") || sp.has("fold_case") || sp.has("regex");
}

// Set the textInput value and all selection controls
// TODO: validate the given options
var validControlOptions = {
  regex: [true, false],
  context: [true, false],
  case: ["auto", "false", "true"],
};
function initStateFromQueryParams() {
  var currURL = new URL(document.location);
  var sp = currURL.searchParams;

  var currentQ = decodeURIComponent(sp.get("q") || "");
  var caseVal = sp.get("fold_case") || "auto";
  var regexVal = sp.get("regex") || false;

  searchBox.value = currentQ;
  caseSelect.value = caseVal;
  regexToggle.dataset.selected = regexVal;

  searchOptions = {
    q: currentQ,
    regex: regexVal,
    context: sp.get("context") || true,
    case: caseVal,
  };

  doSearch();
}

// the regex toggle
function initControlsFromLocalPrefs() {
  var currControls = localStorage.getItem("controls-state") || "{}";
  try {
    currControls = JSON.parse(currControls);
  } catch (err) {
    console.error("error parsing localStorage controls state. Resetting it.");
  }

  var regexVal = currControls["regex"];
  var caseVal = currControls["case"];

  regexToggle.dataset.selected = regexVal == "true";

  // validation in case someone tries to mess with localStorage
  if (!validControlOptions.case.includes(caseVal)) {
    caseVal = "auto";
  }
  caseSelect.value = caseVal;

  searchOptions = {
    q: "",
    regex: regexVal,
    context: true,
    case: caseVal,
  };
}

function storePrefs() {
  var newPrefs = {
    regex: regexToggle.dataset.selected,
    case: caseSelect.value,
  };

  localStorage.setItem("controls-state", JSON.stringify(newPrefs));
}

function renderSearchHistory() {
  var currHistory = localStorage.getItem("search-history") || "[]";
  try {
    currHistory = JSON.parse(currHistory);
  } catch (err) {
    console.error("error parsing localStorage search history. Resetting it.");
    currHistory = [];
  }

  var c = document.querySelector(
    "#helparea #recent-searches .searches-container"
  );
  // empty the container. Note we can't replaceChildren(historyElems)
  // because the current UglifyJs plugin used by webpack doesn't support the
  // .../spread operator and replaceChildren expects a comma delimited list
  // of nodes
  c.replaceChildren();

  for (var i = 0; i < currHistory.length; i++) {
    var searchText = currHistory[i];
    var elem = document.createElement("button");
    elem.innerText = searchText;
    elem.title = "Do search for: " + searchText;
    elem.classList.add("search-item");
    elem.addEventListener("click", function (e) {
      searchBox.value = e.target.innerText;
      searchBox.dispatchEvent(new Event("input"));
    });
    c.appendChild(elem);
  }
}

function addSearchQueryToHistory(e) {
  if (e.target.value.trim() == "") {
    return;
  }

  var currHistory = localStorage.getItem("search-history") || "[]";
  try {
    currHistory = JSON.parse(currHistory);
  } catch (err) {
    console.error("error parsing localStorage search history. Resetting it.");
    currHistory = [];
  }

  var dedupedHistory = currHistory.filter(function (hElem) {
    return hElem !== e.target.value;
  });

  dedupedHistory.unshift(e.target.value); // Add the new item to the front
  dedupedHistory = dedupedHistory.slice(0, 5); // Only keep the last 5 entries

  localStorage.setItem("search-history", JSON.stringify(dedupedHistory));
  renderSearchHistory();
}

function toggleMoreFileMatches(e) {
  document
    .querySelector(".path-results .extra-results")
    .classList.toggle("hidden");
  var textContainer = e.currentTarget.querySelector("#toggle-btn-text");
  textContainer.innerText =
    textContainer.innerText === "Show all" ? "Show less" : "Show all";
  e.currentTarget.querySelector("img").classList.toggle("open");
}

function handleFileExtBtnClick(e) {
  var q = searchBox.value;
  var ext = e.target.innerText;
  if (regexToggle.dataset.selected == "true") {
    q = "path:\\" + ext + "$ " + q;
  } else {
    q = "path:" + ext + " " + q;
  }
  searchBox.value = q;
  searchBox.dispatchEvent(new Event("input"));
}

function init() {
  "use strict";

  searchBox = document.getElementById("searchbox");
  resultsContainer = document.querySelector("#resultarea > #results");
  helpArea = document.getElementById("helparea");
  caseSelect = document.getElementById("case-sensitivity-toggle");
  regexToggle = document.querySelector("button[id=toggle-regex]");
  errorsBox = document.getElementById("regex-error");
  liveDot = document.getElementById("live-status-dot");
  liveText = document.getElementById("live-status-text");

  caseSelect.addEventListener("change", function (e) {
    var newVal = e.target.value;
    searchOptions["case"] = newVal;
    updateSearchParamState();
    storePrefs();
  });
  regexToggle.addEventListener("click", function () {
    toggleControlButton(this);
    storePrefs();
  });
  searchBox.addEventListener("input", updateQuery);

  // add search events to recent searches
  searchBox.addEventListener("blur", addSearchQueryToHistory);

  document.addEventListener("click", function (e) {
    var btn = e.target.closest("button");
    if (btn && btn.id == "showMoreFilematchesBtn") {
      toggleMoreFileMatches(e);
    } else if (btn && btn.classList.contains("file-extension")) {
      handleFileExtBtnClick(e);
    }
  });

  // listen for the '/' key to trigger search input focus
  // or, if text is selected, trigger a search for it
  document.addEventListener("keyup", function (event) {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey)
      return;
    if (event.key !== "/" || searchBox === document.activeElement) return;

    // if there is some selected text, then start a new search for it
    var selectedText = getSelectedText();
    if (selectedText !== "") {
      searchBox.value = selectedText;
      searchBox.dispatchEvent(new Event("input"));
      window.scrollTo({ top: 0, behavior: "smooth" });
      return; // let this event be handled by _handleKey for now, until we remove all this JS
    }

    event.preventDefault();
    searchBox.focus();
    window.scrollTo({ top: 0, behavior: "smooth" });
  });

  window.addEventListener("popstate", function (e) {
    initStateFromQueryParams();
  });

  if (hasParams()) {
    initStateFromQueryParams();
  } else {
    initControlsFromLocalPrefs();
  }

  renderSearchHistory();
  livePoll();
}

module.exports = {
  init: init,
};

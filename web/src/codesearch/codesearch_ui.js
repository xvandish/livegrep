var KeyCodes = {
  SLASH_OR_QUESTION_MARK: 191,
  LEFT_ARROW: 37,
  UP_ARROW: 38,
  RIGHT_ARROW: 39,
  DOWN_ARROW: 40,
};

function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}

var searchBox;
var resultsContainer;
var helpArea;
var caseSelect;
var regexToggle;
var autocompleteMenu; // used for search suggestions
var autocompleteMenuItems;
// used to keep track of which menu item is focused. -1 indicates the search bar
// is highlighted
var currAutocompleteIdx = -1; 

var searchResults; // giant HTML string

var searchOptions = {
  q: '',
  regex: false,
  context: true, // we don't have an option for disabling context. No one uses it
  case: 'auto',
}

var currUrl;
// We could maybe rethink this to be a function that only updates
// the searchapram for the changed option. Ok for now tho
function updateSearchParamState() {
  if (!currUrl) {
    url = new URL(window.location);
  }

  var sp = url.searchParams;

  sp.set('q', encodeURIComponent(searchOptions.q));
  sp.set('regex', searchOptions.regex);
  sp.set('fold_case', searchOptions.case);
  window.history.pushState({}, '', url);

  // TODO - doSearch();
  doSearch();
}

// Take the present search options, perform a search
// then update the 
function doSearch() {
  if (searchOptions.q === '') {
    helpArea.style.display = "initial";
    resultsContainer.innerHTML = "";
    return;
  };
  console.time('query');
  var searchResults = fetch("/api/v2/getRenderedSearchResults/?q=" + 
    searchOptions.q + "&fold_case=" + searchOptions.case + "&regex=" + searchOptions.regex + "&context=" + 
    searchOptions.context)
  .then(function(r) {
    console.timeEnd('query');
    if (!r.ok) {
      return "Error " + r.status + ": " + r.statusText;
    } else {
      return r.text();
    }
  })
  .then(function (text) {
    helpArea.style.display = "none";
    resultsContainer.innerHTML = text;
  });
  /* const inf = await res.json(); */

  // TODO: handle errors (404, 500 etc)
  /* sampleRes.results = [...inf.results]; */
  /* sampleRes.fileResults = [...inf.file_results]; */
  // sampleRes.stats = {
  //   exitReason: "COOL",
  //   totalTime: 200,
  //   totalMatches: 200
  // }

}

function updateQuery(inputEvnt) {
  searchOptions.q = inputEvnt.target.value;
  updateSearchParamState();
}

function toggleControlButton() {
  var currValue = this.getAttribute('data-selected') === 'true';
  this.setAttribute('data-selected', !currValue);
  searchOptions[this.getAttribute('name')] = !currValue;
  updateSearchParamState();
}

// Set the textInput value and all selection controls
// TODO: validate the given options
var validControlOptions = {
  "regex": [true, false],
  "context": [true, false],
  "case": ["auto", false, true]
};
function initStateFromQueryParams() {
  var currURL = new URL(document.location);
  var sp = currURL.searchParams;

  var currentQ = decodeURIComponent(sp.get('q') || '');
  var caseVal = sp.get('fold_case') || 'auto'; 

  searchBox.value = currentQ;
  caseSelect.value = caseVal
  

  searchOptions = {
    q: currentQ,
    regex: sp.get('regex') || false,
    context: sp.get('context') || true,
    case: caseVal,
  };

  doSearch();
}

function toggleMoreFileMatches(e) {
  document.querySelector('.path-results .extra-results').classList.toggle('hidden');
  var textContainer = e.currentTarget.querySelector('#toggle-btn-text');
  textContainer.innerText = textContainer.innerText === 'Show all' ? 'Show less' : 'Show all';
  e.currentTarget.querySelector('img').classList.toggle('open');
}

function init(initData) {
  "use strict"
  console.log('initData: ', initData);

  searchBox = document.querySelector('#searchbox')
  resultsContainer = document.querySelector('#resultarea > #results');
  helpArea = document.querySelector('#helparea');
  caseSelect = document.querySelector('#case-sensitivity-toggle');
  regexToggle = document.querySelector('button[id=toggle-regex]');
  autocompleteMenu = document.getElementById("autocomplete-menu");
  autocompleteMenuItems = autocompleteMenu.querySelectorAll("li");

  regexToggle.addEventListener('click', toggleControlButton);
  searchBox.addEventListener('input', updateQuery);
  searchBox.addEventListener('focusin', function() {
    autocompleteMenu.style.display = "initial";
  });
  searchBox.addEventListener('focusout', function() {
    var menu = document.getElementById("autocomplete-menu");
    autocompleteMenu.style.display = "none";
  });
  searchBox.addEventListener('keydown', function(e) {

    // let's leave list cycling out of this for the moment. or maybe not
    if (e.keyCode == KeyCodes.UP_ARROW) {
      
    } else if (e.keyCode == KeyCodes.DOWN_ARROW) {

      // unfocus the current element
      if (currAutocompleteIdx >= 0) {
        autocompleteMenuItems[currAutocompleteIdx].classList.remove('focused');
      }

      var nextIdx = currAutocompleteIdx + 1;

      if (nextIdx >= autocompleteMenuItems.length) {
        currAutocompleteIdx = -1; // roll over
        return;
      }

      // focus the next element, and se the text content of the search box to it
      var currItem = autocompleteMenuItems[nextIdx];
      currItem.classList.add('focused');

      // We don't want this. Since we do search as you type, we don't want to
      // launch a bunch of searches just from scrolling the list.
        // this.value = currItem.innerText;
      // what we should do instead if have a "Enter" span that indicated you
      // press enter to search.

      currAutocompleteIdx = nextIdx;
    }
  });

  document.addEventListener('click', function(e) {
    var clickedElem = event.target;

    var btn = e.target.closest('button');
    if (btn && btn.id == "showMoreFilematchesBtn") {
      toggleMoreFileMatches(e);
    }
  });

  initStateFromQueryParams();
  // var caseInput = document.querySelector(
  // Get the search input and each of the search options
  //
  // Then on type, doSearch
  // then 
}

module.exports = {
  init: init
}

var KeyCodes = {
  SLASH_OR_QUESTION_MARK: 191,
  LEFT_ARROW: 37,
  UP_ARROW: 38,
  RIGHT_ARROW: 39,
  DOWN_ARROW: 40,
  ENTER: 13,
  ESCAPE: 27,
  BACKSPACE: 8,
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

function renderSearchHistory() {
  var currHistory = localStorage.getItem('search-history') || '[]';
      try {
        currHistory = JSON.parse(currHistory);
      } catch (err) {
        console.error('error parsing localStorage search history. Resetting it.');
        currHistory = [];
      }

      var c = document.querySelector('#helparea #recent-searches .searches-container');
      // empty the container. Note we can't replaceChildren(historyElems)
      // because the current UglifyJs plugin used by webpack doesn't support the
      // .../spread operator and replaceChildren expects a comma delimited list
      // of nodes
      c.replaceChildren();

      for (var i = 0; i < currHistory.length; i++) {
        var searchText = currHistory[i];
        var elem = document.createElement('button');
        elem.innerText = searchText;
        elem.title = 'Do search for: ' + searchText;
        elem.classList.add('search-item');
        elem.addEventListener('click', function(e) {
          searchBox.value = e.target.innerText;
          searchBox.dispatchEvent(new Event('input'))
        });
        c.appendChild(elem);
      };
}

function addSearchQueryToHistory(e) {
    if (e.target.value.trim() == '') {
      return
    };

    var currHistory = localStorage.getItem('search-history') || '[]';
      try {
        currHistory = JSON.parse(currHistory);
      } catch (err) {
        console.error('error parsing localStorage search history. Resetting it.');
        currHistory = [];
      }

    var dedupedHistory = currHistory.filter(function (hElem) {
        return hElem !== e.target.value;
      });

    dedupedHistory.unshift(e.target.value); // Add the new item to the front
    dedupedHistory = dedupedHistory.slice(0, 5); // Only keep the last 5 entries

    localStorage.setItem('search-history', JSON.stringify(dedupedHistory));
    renderSearchHistory();
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
  regexToggle.addEventListener('click', toggleControlButton);
  searchBox.addEventListener('input', updateQuery);

  // add search events to recent searches
  searchBox.addEventListener('blur', addSearchQueryToHistory);

  document.addEventListener('click', function(e) {
    var clickedElem = event.target;

    var btn = e.target.closest('button');
    if (btn && btn.id == "showMoreFilematchesBtn") {
      toggleMoreFileMatches(e);
    }
  });

  // listen for the '/' key to trigger search input focus
  // or, if text is selected, trigger a search for it
  document.addEventListener('keyup', function (e) {
     if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey)
      return;
     if (event.key !== "/" || (searchBox === document.activeElement)) return;

     // if there is some selected text, then start a new search for it
    var selectedText = getSelectedText();
    if (selectedText !== "") {
      searchBox.value = selectedText;
      searchBox.dispatchEvent(new Event('input'))
      window.scrollTo({ top: 0, behavior: 'smooth' });
      return; // let this event be handled by _handleKey for now, until we remove all this JS 
    }

    event.preventDefault();
    searchBox.focus();
    window.scrollTo({ top: 0, behavior: 'smooth' });

  });

  initStateFromQueryParams();
  renderSearchHistory();
}

module.exports = {
  init: init
}

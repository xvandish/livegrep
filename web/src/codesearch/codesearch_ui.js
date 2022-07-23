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

function toggleMoreFileMatches(e) {
  document.querySelector('.path-results .extra-results').classList.toggle('hidden');
  var textContainer = e.currentTarget.querySelector('#toggle-btn-text');
  textContainer.innerText = textContainer.innerText === 'Show all' ? 'Show less' : 'Show all';
  e.currentTarget.querySelector('img').classList.toggle('open');
}

function isAutocompleteMenuOpen() {
  return autocompleteMenu.style.display == 'initial';
}

function openAutcompleteMenu() {
  autocompleteMenu.style.display = 'initial';
}

function clearCurAutocompleteSelectionHighlight() {
  if (currAutocompleteIdx >= 0 && currAutocompleteIdx <= autocompleteMenuItems.length - 1) {
    autocompleteMenuItems[currAutocompleteIdx].classList.remove('focused');
  }
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

  autocompleteMenu.addEventListener('mouseover', function (e) {
    console.log('e.target.type', e.target.type);
    // clearCurAutocompleteSelectionHighlight();
    // currAutocompleteIdx = -1;
  });

  regexToggle.addEventListener('click', toggleControlButton);
  searchBox.addEventListener('input', updateQuery);
  searchBox.addEventListener('focusin', function() {
    autocompleteMenu.style.display = "initial";
  });
  searchBox.addEventListener('focusout', function() {
    var menu = document.getElementById("autocomplete-menu");
    autocompleteMenu.style.display = "none";
  });
  searchBox.addEventListener('input', function (e) {
      // The user is typing something. Filter out "suggestions" that don't match
      // also, set the currAutocompleteIdx to -1
      clearCurAutocompleteSelectionHighlight();
      currAutocompleteIdx = -1; 

      // we don't use this.value, since we're in the keydown handler, we fire
      // before the value actually changes.
      var currText = this.value;
      var exp = e.target.value;
      
      // rather than split the logic into two handlers, one that tracks addition
      // with 'input' events, and one that tracks deletion with 'onkeydown'
      // events, we just do a brute force traversal of all li, without regard to
      // whether they were shown/hidden before. With n < 10 suggestions, this is
      // fine.

      var allItems = autocompleteMenu.querySelectorAll('li');
      console.log(allItems);
      for (var i = 0; i < allItems.length; i++) {
        var item = allItems[i];
        console.log('item is: ', item);
        if (currText.length > item.innerText.length) {
          console.log('currText len is greater');
          item.dataset.hidden = 'true';
          continue;
        }

        var c = item.innerText.slice(0, currText.length);
        console.log('c: ', c);
        console.log('currText: ', currText);
        if (c != currText) {
          item.dataset.hidden = 'true';
        } else {
          item.dataset.hidden = 'false';
        }
      }
  });

  // we handle opening, closing and iterating through the autocomplete list here
  // HOWEVER - we handle filtering the list based on input in another handler,
  // since 'keydown' doesn't get us access to the currentText of the input
  searchBox.addEventListener('keydown', function(e) {
    if (e.keyCode == KeyCodes.ENTER) {
      console.log('enter pressed');
      // If the key is enter, and we have a selected history item, fill the
      // search with it and cose the autocomplete box
      if (currAutocompleteIdx >= 0 && currAutocompleteIdx <= autocompleteMenuItems.length - 1) {
        this.value = autocompleteMenuItems[currAutocompleteIdx].innerText;
        autocompleteMenu.style.display = 'none';
        this.dispatchEvent(new Event('input')); // trigger the search
        this.blur();
        // reset the autocomplete index 
        autocompleteMenuItems[currAutocompleteIdx].classList.remove('focused'); 
        currAutocompleteIdx = -1;
      }
    } else if (e.keyCode == KeyCodes.UP_ARROW) {
      clearCurAutocompleteSelectionHighlight();

      if (!isAutocompleteMenuOpen()) {
        currAutocompleteIdx = -1;
        openAutcompleteMenu();
        return;
      }

      var nextIdx = currAutocompleteIdx - 1;

      if (nextIdx == -1) {
        currAutocompleteIdx = -1;
        return;
      } if (nextIdx == -2) {
        nextIdx = autocompleteMenuItems.length - 1; // roll to the start of the list
      }

      var currItem = autocompleteMenuItems[nextIdx];
      currItem.classList.add('focused');

      currAutocompleteIdx = nextIdx;
      
    } else if (e.keyCode == KeyCodes.DOWN_ARROW) {
      clearCurAutocompleteSelectionHighlight(); 

      if (!isAutocompleteMenuOpen()) {
        currAutocompleteIdx = -1;
        openAutcompleteMenu();
        return;
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
    } else if (e.keyCode == KeyCodes.ESCAPE) { // if the user wants to close the dialog.
      clearCurAutocompleteSelectionHighlight();
      currAutocompleteIdx = -1;
      autocompleteMenu.style.display = 'none';
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

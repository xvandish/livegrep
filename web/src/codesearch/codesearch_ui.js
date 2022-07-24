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

function createSearchHistory() {
  var currHistory = localStorage.getItem('search-history') || '[]';
      try {
        currHistory = JSON.parse(currHistory);
      } catch (err) {
        console.error('error parsing localStorage search history. Resetting it.');
        currHistory = [];
      }

  // we ship some default suggestions, so don't override those
  if (currHistory.length == 0) {
    return;
  }

  var c = autocompleteMenu.querySelector('ul');

  // empty the container. Note we can't replaceChildren(historyElems)
  // because the current UglifyJs plugin used by webpack doesn't support the
  // .../spread operator and replaceChildren expects a comma delimited list
  // of nodes
  c.replaceChildren();

  for (var i = 0; i < currHistory.length; i++) {
        var searchText = currHistory[i];
        var elem = document.createElement('li');
        elem.dataset.hidden = 'false'; // TODO: show/hide based on query
        elem.title = 'Do search for: ' + searchText;
        elem.innerText = searchText;
        elem.dataset.value = searchText;
        c.appendChild(elem);
  }

  var header = autocompleteMenu.querySelector('#suggestions-header');
  header.innerText = "Recent searches:";
}

// returns the number of "shown" items
function filterShownAutocompleteItems() {
      // The user is typing something. Filter out "suggestions" that don't match
      // also, set the currAutocompleteIdx to -1
      clearCurAutocompleteSelectionHighlight();
      currAutocompleteIdx = -1; 

      // we don't use this.value, since we're in the keydown handler, we fire
      // before the value actually changes.
      var currText = searchBox.value;
      
      // rather than split the logic into two handlers, one that tracks addition
      // with 'input' events, and one that tracks deletion with 'onkeydown'
      // events, we just do a brute force traversal of all li, without regard to
      // whether they were shown/hidden before. With n < 10 suggestions, this is
      // fine.

      var allItems = autocompleteMenu.querySelectorAll('li');
      var countShown = 0;
      if (searchBox.value == "") {
        for (var i = 0; i < allItems.length; i++) {
          allItems[i].dataset.hidden = 'false';
        }
        return allItems.length;
      }
      console.log(allItems);
      for (var i = 0; i < allItems.length; i++) {
        var item = allItems[i];
        var itemText = item.dataset.value;
        console.log('item is: ', item);
        if (currText.length > itemText.length) {
          console.log('currText len is greater');
          item.dataset.hidden = 'true';
          continue;
        }

        var c = itemText.slice(0, currText.length);
        console.log('c: ', c);
        console.log('currText: ', currText);
        if (c != currText) {
          item.dataset.hidden = 'true';
        } else {
          countShown += 1;
          item.dataset.hidden = 'false';
        }
      }
    return countShown;

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
  
  // we use mousedown instead of click so we can ensure this fire's before
  // the focusout event that closes the autocompleteMenu
  autocompleteMenu.addEventListener('mousedown', function (e) {
    console.log('in click handler');
    if (e.target.tagName == "LI") {
      searchBox.value = e.target.dataset.value;
      searchBox.dispatchEvent(new Event('input')); // trigger the search
    }
  });

  regexToggle.addEventListener('click', toggleControlButton);
  searchBox.addEventListener('input', updateQuery);
  searchBox.addEventListener('focusin', function() {
    var numShown = filterShownAutocompleteItems();
    if (numShown == 0) return; // no point in opening the menu
    autocompleteMenu.style.display = "initial";
  });
  searchBox.addEventListener('focusout', function() {
    var menu = document.getElementById("autocomplete-menu");
    autocompleteMenu.style.display = "none";
  });
  // add search events to recent searches
  searchBox.addEventListener('blur', function (e) {

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
    createSearchHistory();
    // delete the current list, and swap in the new list.
  });
  searchBox.addEventListener('input', function (e) {
    console.log('input handler called');
    console.log('text in input e.target.value=', e.target.value);
    var numShown = filterShownAutocompleteItems();
    if (numShown == 0) {
      autocompleteMenu.style.display = "none";
    } else {
      autocompleteMenu.style.display = "initial";
    }
    // if there are no shown items left, close the autocomplete
  });

  // we handle opening, closing and iterating through the autocomplete list here
  // HOWEVER - we handle filtering the list based on input in another handler,
  // since 'keydown' doesn't get us access to the currentText of the input
  searchBox.addEventListener('keydown', function(e) {
    autocompleteMenuItems = autocompleteMenu.querySelectorAll('li[data-hidden="false"]');
    console.log('in keydown handler');
    if (e.keyCode == KeyCodes.ENTER) {
      console.log('enter pressed');
      // If the key is enter, and we have a selected history item, fill the
      // search with it and cose the autocomplete box
      if (currAutocompleteIdx >= 0 && currAutocompleteIdx <= autocompleteMenuItems.length - 1) {
        this.value = autocompleteMenuItems[currAutocompleteIdx].dataset.value;
        autocompleteMenu.style.display = 'none';
        this.dispatchEvent(new Event('input')); // trigger the search
        
        // for now, don't blur the input box, to let the users keep refining the
        // search if they want to
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
  createSearchHistory();
  // var caseInput = document.querySelector(
  // Get the search input and each of the search options
  //
  // Then on type, doSearch
  // then 
}

module.exports = {
  init: init
}

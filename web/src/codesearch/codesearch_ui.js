function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}

var searchBox;
var errorsBox;
var resultsContainer;
var helpArea;
var caseSelect;
var regexToggle;

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

  doSearch();
}

// Take the present search options, perform a search
// then update the 
function doSearch() {
  if (searchOptions.q === '') {
    helpArea.style.display = "initial";
    resultsContainer.innerHTML = "";
    errorsBox.style.display = "none";
    return;
  };
  var time1 = performance.now();
  var time2;
  fetch("/api/v2/getRenderedSearchResults/?q=" + 
    searchOptions.q + "&fold_case=" + searchOptions.case + "&regex=" + searchOptions.regex + "&context=" + 
    searchOptions.context)
  .then(function(r) {
    time2 = performance.now();
    if (!r.ok) {
      return Promise.reject(r.text());
    } else {
      return r.text();
    }
  })
  .then(function (text) {
    helpArea.style.display = "none";
    resultsContainer.innerHTML = text;
    errorsBox.style.display = "none";

    var timeTaken = ((time2 - time1) / 1000).toFixed(4);
    document.getElementById('searchtime').innerText = timeTaken + "s";
    document.getElementById('searchtimebox').style.display = "initial";
  })
  .catch(function (err) {
    err.then(function (errText) {
      // display the help area, clear previous results, and show the new error
      helpArea.style.display="initial";
      resultsContainer.innerHTML = "";
      errorsBox.querySelector('#errortext').innerText = errText;
      errorsBox.style.display = "initial";
    });
  });
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
  var regexVal = sp.get('regex') || false;

  searchBox.value = currentQ;
  caseSelect.value = caseVal
  regexToggle.dataset.selected = regexVal; 
  

  searchOptions = {
    q: currentQ,
    regex: regexVal,
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

  searchBox = document.getElementById('searchbox')
  resultsContainer = document.querySelector('#resultarea > #results');
  helpArea = document.getElementById('helparea');
  caseSelect = document.getElementById('case-sensitivity-toggle');
  regexToggle = document.querySelector('button[id=toggle-regex]');
  errorsBox = document.getElementById('regex-error');

  caseSelect.addEventListener('change', function (e) {
    var newVal = e.target.value;
    searchOptions['case'] = newVal;
    updateSearchParamState();
  });
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

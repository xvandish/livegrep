// this file initializes a stripped down version of the header that removes the
// log and blame options, and (maybe) triggers help text if I feel up to it
// TODO: add back options (triggered by h or b) that takes you back 

var KeyCodes = {
  ESCAPE: 27,
  ENTER: 13,
  SLASH_OR_QUESTION_MARK: 191
};

function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}


function init(repoName, path, urlPattern) {
  var helpScreen = document.getElementById('help-screen');
  var externalLink = document.getElementById('external-link')
  externalLink.setAttribute('href', getExternalLink())


  function doSearch(event, query, newTab) {
    var url;
    if (query !== undefined) {
      url = '/search?q=' + encodeURIComponent(query) + '&repo=' + encodeURIComponent(repoName);
    } else {
      url = '/search';
    }
    if (newTab === true){
      window.open(url);
    } else {
      window.location.href = url
    }
  }

  function showHelp() {
    helpScreen.classList.remove('hidden');
  }

  function hideHelp() {
    helpScreen.classList.add('hidden')
  }

  
  function getExternalLink() {
    var filePath = path;

    var url = urlPattern;

    // If url not found, warn user and fail gracefully
    if (!url) { // deal with both undefined and empty string
        console.error("The index file you provided does not provide repositories[x].metadata.url_pattern. External links to file sources will not work. See the README for more information on file viewing.");
        return;
    }

    // If {path} already has a slash in front of it, trim extra leading
    // slashes from `pathInRepo` to avoid a double-slash in the URL.
    if (url.indexOf('/{path}') !== -1) {
      filePath = filePath.replace(/^\/+/, '');
    }

    // XXX code copied
    url = url.replace('{lno}', ''); // no line number to point to
    url = url.replace('{version}', 'HEAD'); // we don't have a specific commit while on the log page
    url = url.replace('{name}', repoName);
    url = url.replace('{path}', filePath);
    return url;
  }

  function processKeyEvent(event) {
    if(event.code === 'Enter') {
      // Perform a new search with the selected text, if any
      var selectedText = getSelectedText();
      if(selectedText) {
        doSearch(event, selectedText, true);
      }
    } else if(event.code === 'Slash' || event.keyCode == 63) {
        event.preventDefault();
        if(event.shiftKey) {
          showHelp();
        } else {
          hideHelp();
          doSearch(event, getSelectedText(), false);
        }
    } else if(event.code === 'Escape') {
      // Avoid swallowing the important escape key event unless we're sure we want to
      if(!helpScreen.classList.contains('hidden')) {
        event.preventDefault();
        hideHelp();
      }
    //   $('#query').blur(); don't know what this was supposed to do
    } else if(event.code === 'KeyV') {
      // Visually highlight the external link to indicate what happened
      externalLink.focus() 
      window.location = externalLink.getAttribute('href')
    } else if (event.code === 'KeyN' || event.code ==  'KeyP') {
      var goBackwards = event.code === 'KeyP';
      var selectedText = getSelectedText();
      if (selectedText) {
        window.find(selectedText, false /* case sensitive */, goBackwards);
      }
    }
    return true;
  }

  function initializeActionButtons() {
    // Map out action name to function call, and automate the details of actually hooking
    // up the event handling.
    var ACTION_MAP = {
      search: doSearch,
      help: showHelp,
    };

    for(var actionName in ACTION_MAP) {
      document.querySelector(`a[data-action-name=${actionName}]`).addEventListener('click',
        // We can't use the action mapped handler directly here since the iterator (`actioName`)
        // will keep changing in the closure of the inline function.
        // Generating a click handler on the fly removes the dependency on closure which
        // makes this work as one would expect. #justjsthings.
        (function(handler) {
          return function(event) {
            event.preventDefault();
            event.stopImmediatePropagation(); // Prevent immediately closing modals etc.
            handler.call(this, event);
          }
        })(ACTION_MAP[actionName])
      )
    }
  }

  var showSelectionReminder = function () {
    document.getElementsByClassName('without-selection')[0].style.display = 'none'
    document.getElementsByClassName('with-selection')[0].style.display = 'block';
  }

  var hideSelectionReminder = function () {
    document.getElementsByClassName('without-selection')[0].style.display = 'block'
    document.getElementsByClassName('with-selection')[0].style.display = 'none';
  }

  function initializePage() {
    window.document.addEventListener('keydown', (e) => {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        processKeyEvent(e);
    });
    
    window.document.addEventListener('selectionchange', () => {
        var selectedText = getSelectedText();
        if(selectedText) {
          showSelectionReminder(selectedText);
        } else {
          hideSelectionReminder();
        }
    });

    window.document.addEventListener('click', function(event) {
      if (!helpScreen.classList.contains('hidden') && !event.target.closest("#help-screen-card")) { // check against card, not overlay
        hideHelp();
      }
    });

    // initializeActionButtons($('.header .header-actions'));
  }

  // The native browser handling of hashes in the location is to scroll
  // to the element that has a name matching the id. We want to prevent
  // this since we want to take control over scrolling ourselves, and the
  // most reliable way to do this is to hide the elements until the page
  // has loaded. We also need defer our own scroll handling since we can't
  // access the geometry of the DOM elements until they are visible.
  initializePage();
  initializeActionButtons();
}

// init()

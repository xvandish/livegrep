// this file initializes a stripped down version of the header that removes the
// log and blame options, and (maybe) triggers help text if I feel up to it

$ = require('jquery');

var KeyCodes = {
  ESCAPE: 27,
  ENTER: 13,
  SLASH_OR_QUESTION_MARK: 191
};

function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}


function init(initData) {
  var helpScreen = document.getElementById('.help-screen');
  var externalLink = document.getElementById('external-link')
  externalLink.setAttribute('href', getExternalLink(range))


  function doSearch(event, query, newTab) {
    console.log('doSearch was called')
    var url;
    if (query !== undefined) {
      console.log('query not undefined, query: ', query)
      url = '/search?q=' + encodeURIComponent(query) + '&repo=' + encodeURIComponent(initData.repo_info.name);
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
    var otherHelp = document.getElementById('help-screen')
    otherHelp.classList.toggle('help-screen')
    otherHelp.addEventListener('click', (e) => {
        e.preventDefault();
    })

    // helpScreen.removeClass('hidden').children().on('click', function(event) {
    //   // Prevent clicks inside the element to reach the document
    //   event.stopImmediatePropagation();
    //   return true;
    // });
  }

  function hideHelp() {
    helpScreen.classList.toggle('hidden')
  }

  
  function getExternalLink() {
    var repoName = initData.repo_info.name;
    var filePath = initData.file_path;

    var url = initData.repo_info.metadata['url_pattern'];

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
    url = url.replace('{version}', initData.commit);
    url = url.replace('{name}', repoName);
    url = url.replace('{path}', filePath);
    return url;
  }

  function processKeyEvent(event) {
    if(event.which === KeyCodes.ENTER) {
      // Perform a new search with the selected text, if any
      var selectedText = getSelectedText();
      if(selectedText) {
        doSearch(event, selectedText, true);
      }
    } else if(event.which === KeyCodes.SLASH_OR_QUESTION_MARK) {
        event.preventDefault();
        if(event.shiftKey) {
          showHelp();
        } else {
          hideHelp();
          doSearch(event, getSelectedText());
        }
    } else if(event.which === KeyCodes.ESCAPE) {
      // Avoid swallowing the important escape key event unless we're sure we want to
      if(!helpScreen.hasClass('hidden')) {
        event.preventDefault();
        hideHelp();
      }
    //   $('#query').blur(); don't know what this was supposed to do
    } else if(String.fromCharCode(event.which) == 'V') {
      // Visually highlight the external link to indicate what happened
      externalLink.focus() 
      window.location = externalLink.getAttribute('href')
    } else if (String.fromCharCode(event.which) == 'N' || String.fromCharCode(event.which) == 'P') {
      var goBackwards = String.fromCharCode(event.which) === 'P';
      var selectedText = getSelectedText();
      if (selectedText) {
        window.find(selectedText, false /* case sensitive */, goBackwards);
      }
    }
    return true;
  }

  function initializeActionButtons(root) {
    // Map out action name to function call, and automate the details of actually hooking
    // up the event handling.
    var ACTION_MAP = {
      search: doSearch,
      help: showHelp,
    };

    for(var actionName in ACTION_MAP) {
      root.on('click auxclick', '[data-action-name="' + actionName + '"]',
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
    window.document.addEventListener('click', (e) => {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        processKeyEvent(e)
    })
    
    window.document.addEventListener('mouseup', () => {
        var selectedText = getSelectedText();
        if(selectedText) {
          showSelectionReminder(selectedText);
        } else {
          hideSelectionReminder();
        }
    })

    initializeActionButtons($('.header .header-actions'));
  }

  // The native browser handling of hashes in the location is to scroll
  // to the element that has a name matching the id. We want to prevent
  // this since we want to take control over scrolling ourselves, and the
  // most reliable way to do this is to hide the elements until the page
  // has loaded. We also need defer our own scroll handling since we can't
  // access the geometry of the DOM elements until they are visible.
  initializePage();
}

init()

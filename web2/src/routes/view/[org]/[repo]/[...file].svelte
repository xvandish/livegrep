<script lang="ts" context="module">

    // get the repoName and path from query parameters
    import { page } from '$app/stores';

    /** @type {import('@sveltejs/kit').Load} */
    export async function load({ params, fetch, session, stuff }) {
        console.log('in load function');
	const repo = `${params.org}/${params.repo}`;
	const filePath = params.file;

        const path = `http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`
        console.log({ path });
        const res = await fetch(`http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`);

        return {
            status: res.status,
            props: {
                fileInfo: res.ok && (await res.json()),
                fullRepoName: repo,
                filePath: filePath,
                commit: "HEAD", // temp for now
                urlPattern: "https://github.com/{name}/blob/{version}/{path}#L{lno}"
            }
        };
    }
</script>

<script>
    export let fileInfo;
    export let fullRepoName;
    export let filePath;
    export let urlPattern;
    export let commit;

    import { onMount } from 'svelte';
    
    var root;
    var lineNumberContainer; 
    var helpScreen;

    onMount(() => {
        root = document.getElementsByClassName('file-content')[0];
        lineNumberContainer = document.querySelector('.file-content > .line-numbers');
        helpScreen = document.getElementsByClassName('help-screen')[0]; 

        console.log(root, lineNumberContainer, helpScreen)
        // The native browser handling of hashes in the location is to scroll
        // to the element that has a name matching the id. We want to prevent
        // this since we want to take control over scrolling ourselves, and the
        // most reliable way to do this is to hide the elements until the page
        // has loaded. We also need defer our own scroll handling since we can't
        // access the geometry of the DOM elements until they are visible.
        setTimeout(function() {
            lineNumberContainer.style.display = "block"; //  css({display: 'block'});
            initializePage();
            initializeActionButtons();
        }, 1);
    })

    function getSelectedText() {
        return window.getSelection ? window.getSelection().toString() : null;
    }

    function getOffset(element){
        if (!element.getClientRects().length)
        {
        return { top: 0, left: 0 };
        }

        let rect = element.getBoundingClientRect();
        let win = element.ownerDocument.defaultView;
        return (
        {
        top: rect.top + win.pageYOffset,
        left: rect.left + win.pageXOffset
        });   
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
        if(!firstLineElement) {
            // We were given a scroll offset to a line number that doesn't exist in the page, bail
            return;
        }
        if(range.start != range.end) {
            // We have a range, try and center the entire range. If it's to high
            // for the viewport, fallback to revealing the first element.
            var lastLineElement = elementContainer.querySelector("#L" + range.end);
            var rangeHeight = (getOffset(lastLineElement).top + lastLineElement.clientHeight) - getOffset(firstLineElement).top;
            if(rangeHeight <= viewportHeight) {
                // Range fits in viewport, center it
                scrollOffset = 0.5 * (viewportHeight - rangeHeight);
            } else {
                scrollOffset = firstLineElement.clientHeight / 2; // Stick to (almost) the top of the viewport
            }
        }

        // viewport.scrollTop(firstLineElement.offset().top - scrollOffset);
        viewport.scrollTo({top: getOffset(firstLineElement).top - scrollOffset });
    }

    function setHash(hash) {
        if(history.replaceState) {
            history.replaceState(null, null, hash);
        } else {
            location.hash = hash;
        }
    }

    function parseHashForLineRange(hashString) {
        var parseMatch = hashString.match(/#L(\d+)(?:-L?(\d+))?/);

        if(parseMatch && parseMatch.length === 3) {
            // We have a match on the regex expression
            var startLine = parseInt(parseMatch[1], 10);
            var endLine = parseInt(parseMatch[2], 10);
            if(isNaN(endLine) || endLine < startLine) {
            endLine = startLine;
            }
            return {
            start: startLine,
            end: endLine
            };
        }

        return null;
    }

    function addHighlightClassesForRange(range, root) {
        var idSelectors = [];
        for(var lineNumber = range.start; lineNumber <= range.end; lineNumber++) {
            root.querySelector("#L" + lineNumber).classList.add('highlighted');
        }
    }

    function expandRangeToElement(element) {
        var range = parseHashForLineRange(document.location.hash);
        if(range) {
            var elementLine = parseInt(element.attr('id').replace('L', ''), 10);
            if(elementLine < range.start) {
            range.end = range.start;
            range.start = elementLine;
            } else {
            range.end = elementLine;
            }
            setHash("#L" + range.start + "-" + range.end);
        }
    }

    function doSearch(event, query, newTab) {
        var url;
        if (query && query !== '') {
            url = '/search?q=' + encodeURIComponent(query) + '&repo=' + encodeURIComponent(fullRepoName);
        } else {
            url = '/search';
        }
        if (newTab === true){
            window.open(url);
        } else {
            window.location.href = url
        }
    }

    function handleHashChange(scrollElementIntoView) {
        if(scrollElementIntoView === undefined) {
            scrollElementIntoView = true; // default if nothing was provided
        }

        // Clear current highlights
        lineNumberContainer.querySelectorAll('.highlighted').forEach(elem => elem.classList.remove('highlighted'));

        // Highlight the current range from the hash, if any
        var range = parseHashForLineRange(document.location.hash);
        if(range) {
            addHighlightClassesForRange(range, lineNumberContainer);
            if(scrollElementIntoView) {
                scrollToRange(range, root);
            }
        }

        // Update the external-browse link
        document.getElementById('external-link').setAttribute('href', getExternalLink(range));
        // updateFragments(range, $('#permalink, #back-to-head'));
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
            return range.start + "-" + range.end;
        }
    }

    function getExternalLink(range) {
        var lno = getLineNumber(range);

        var repoName = fullRepoName;
        var transformedFilePath = filePath;

        var url = urlPattern;

        // If url not found, warn user and fail gracefully
        if (!url) { // deal with both undefined and empty string
            console.error("The index file you provided does not provide repositories[x].metadata.url_pattern. External links to file sources will not work. See the README for more information on file viewing.");
            return;
        }

        // If {path} already has a slash in front of it, trim extra leading
        // slashes from `pathInRepo` to avoid a double-slash in the URL.
        if (url.indexOf('/{path}') !== -1) {
            transformedFilePath = transformedFilePath.replace(/^\/+/, '');
        }

        // XXX code copied
        url = url.replace('{lno}', lno);
        url = url.replace('{version}', commit);
        url = url.replace('{name}', repoName);
        url = url.replace('{path}', transformedFilePath);
        return url;
    }

    function updateFragments(range, anchors) {
        // $anchors.each(function() {
        //     var $a = $(this);
        //     var href = $a.attr('href').split('#')[0];
        //     if (range !== null) {
        //         href += '#L' + getLineNumber(range);
        //     }
        //     $a.attr('href', href);
        // });
  }

  var KeyCodes = {
    ESCAPE: 27,
    ENTER: 13,
    SLASH_OR_QUESTION_MARK: 191
  };

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
          doSearch(event, getSelectedText(), false);
        }
    } else if(event.which === KeyCodes.ESCAPE) {
      // Avoid swallowing the important escape key event unless we're sure we want to
      if(!helpScreen.classList.contains('hidden')) {
        event.preventDefault();
        hideHelp();
      }
    } else if(String.fromCharCode(event.which) == 'V') {
      // Visually highlight the external link to indicate what happened
      const externalLink = document.getElementById('external-link');
      externalLink.focus();
      window.location.href = externalLink.getAttribute('href');
    } else if (String.fromCharCode(event.which) == 'Y') {
      var permalinkLink = document.getElementById('permalink');
      if (permalinkLink) {
        permalinkLink.focus();
        window.location.href = permalinkLink.getAttribute('href'); // .attr('href');
      }
    } else if (String.fromCharCode(event.which) == 'N' || String.fromCharCode(event.which) == 'P') {
      var goBackwards = String.fromCharCode(event.which) === 'P';
      var selectedText = getSelectedText();
      if (selectedText) {
        // window.find(selectedText, false /* case sensitive */, goBackwards);
        console.log('not implemented yet!')
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
    document.getElementsByClassName('without-selection')[0].style.display = 'none';
    document.getElementsByClassName('with-selection')[0].style.display = 'block';
  }

  var hideSelectionReminder = function () {
    document.getElementsByClassName('without-selection')[0].style.display = 'block'
    document.getElementsByClassName('with-selection')[0].style.display = 'none';
  }

  function initializePage() {
    // Initial range detection for when the page is loaded
    handleHashChange(false);

    // Allow shift clicking links to expand the highlight range
    // lineNumberContainer.on('click', 'a', function(event) {
    //   event.preventDefault();
    //   if(event.shiftKey) {
    //     expandRangeToElement($(event.target), lineNumberContainer);
    //   } else {
    //     setHash($(event.target).attr('href'));
    //   }
    //   handleHashChange(false);
    // });
    // $(window).on('hashchange', function(event) {
    //   event.preventDefault();
    //   // The url was updated with a new range
    //   handleHashChange();
    // });

    window.document.addEventListener('keydown', (e) => {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        processKeyEvent(e);
    });
    
    window.document.addEventListener('selectionchange', () => {
        var selectedText = getSelectedText();
        if(selectedText) {
          showSelectionReminder();
        } else {
          hideSelectionReminder();
        }
    });

    window.document.addEventListener('click', function(event) {
      const helpScreenCard = document.querySelector('.help-screen-card');
      if (!helpScreen.classList.contains('hidden') && !helpScreenCard.contains(event.target)) { // check against card, not overlay
        hideHelp();
      }
    });

    // initializeActionButtons($('.header .header-actions'));
  }

    function showHelp() {
        helpScreen.classList.remove('hidden');
    }

    function hideHelp() {
        helpScreen.classList.add('hidden')
    }

    

</script>

<svelte:head>
    <title>File View</title>
<script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.14.0/prism.min.js" integrity="sha384-55dGHwJ+p8K+4zJGgJR7q7Fl9FuG++oKmlhKuS+dWjEMj6rBCp7AFYw55b0E5/K8" crossorigin="anonymous"></script>
<script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.14.0/plugins/autoloader/prism-autoloader.min.js" integrity="sha384-S+UYfywCk42UjE2CVTgW2zT3c/X5Uw25LTU93Pn5HmyD5D31yHRu6I5VadHu3Qf5" crossorigin="anonymous"></script>
<script>
  Prism.plugins.autoloader.languages_path = 'https://cdnjs.cloudflare.com/ajax/libs/prism/1.14.0/components/';
      console.log('set prism path');
</script>
</svelte:head>

    <section class="file-viewer">
        <header class="header">
            <nav class="header-title">
                <a href="/view/{fileInfo["repo_info"].name}" class="path-segment repo" title="Repository: {fileInfo["repo_info"].name}">{fileInfo.repo_info.name}</a>:
                {#each fileInfo.data.PathSegments as pSeg, idx}
                    {#if idx > 0}/{/if}<a href={pSeg.Path} class="path-segment">{pSeg.Name}</a>
                {/each}
            </nav>
            <ul class="header-actions without-selection">
            <li class="header-action">
                <a data-action-name="search" title="Perform a new search. Keyboard shortcut: /" href="/search">new search [<span class='shortcut'>/</span>]</a>
            </li>,
            <li class="header-action">
                <a id="external-link" data-action-name="" title="View at {fileInfo.data.ExternalDomain}. Keyboard shortcut: v" href="#">view at {fileInfo.data.ExternalDomain} [<span class='shortcut'>v</span>]</a>
            </li>,
            {#if fileInfo.data.Permalink !== ""}
            <li class="header-action">
                <a id="permalink" title="Permalink. Keyboard shortcut: y" href={fileInfo.data.Permalink}>permalink [<span class='shortcut'>y</span>]</a>
            </li>,
            {:else}
            <li class="header-action">
                <a id="back-to-head" title="return to HEAD revision" href={fileInfo.data.HeadLink}>back to HEAD</a>
            </li>,
            {/if}
            <li class="header-action">
                <a data-action-name="help" title="View the help screen. Keyboard shortcut: ?" href="#">help [<span class='shortcut'>?</span>]</a>
            </li>
            </ul>
            <ul class="header-actions with-selection" style="display:none">
            <li class="header-action">
                search for selected text [/]
            </li>,
            <li class="header-action">
                <a id="external-link" data-action-name="" title="View at {fileInfo.data.ExternalDomain}. Keyboard shortcut: v" href="#">view at {fileInfo.data.ExternalDomain} [<span class='shortcut'>v</span>]</a>
            </li>,
            <li class="header-action">
                previous match [p]
            </li>,
            <li class="header-action">
                next match [n]
            </li>,
            <li class="header-action">
                <a data-action-name="help" title="View the help screen. Keyboard shortcut: ?" href="#">help [<span class='shortcut'>?</span>]</a>
            </li>
            </ul>
        </header>

        <div class="content-wrapper">
            {#if fileInfo.data.DirContent}
                <ul class="file-list">
                    {#each fileInfo.data.DirContent.Entries as dirEntry}
                        <li class="file-list-entry {dirEntry.IsDir && 'is-directory'} {dirEntry.SymlinkTarget && 'is-symlink'}">
                            {#if dirEntry.Path}
                                <a href={dirEntry.Path}>{dirEntry.Name}{#if dirEntry.IsDir}/{/if}</a>
                            {/if}
                            {#if dirEntry.SymlinkTarget}
                                &rarr; (<span class="symlink-target">{dirEntry.SymlinkTarget}</span>)
                            {/if}
                        </li>
                    {/each}
                </ul>
            {/if}
            {#if fileInfo.data.FileContent}
                <div class="file-content">
                    <code id="source-code" class="code-pane language-{fileInfo.data.FileContent.Language}">{fileInfo.data.FileContent.Content}</code>
                    <div id="line-numbers" class="line-numbers hide-links">
                        {#each {length: fileInfo.data.FileContent.LineCount +1} as _, lno}
                            <a id="L{lno+1}" href="#L{lno+1}">{lno+1}</a>
                        {/each}
                    </div>
                </div>
            {/if}
        </div>

          <section class="help-screen u-modal-overlay hidden">
            <div class="help-screen-card u-modal-content">
            <ul>
                <li>Click on a line number to highlight it</li>
                <li>Shift + click a second line number to highlight a range</li>
                <li>Press <kbd class="keyboard-shortcut">/</kbd> to start a new search</li>
                <li>Press <kbd class="keyboard-shortcut">v</kbd> to view this file/directory at {fileInfo.data.ExternalDomain}</li>
                <li>Press <kbd class="keyboard-shortcut">y</kbd> to create a permalink to this version of this file</li>
                <li>Select some text and press <kbd class="keyboard-shortcut">/</kbd> to search for that text</li>
                <li>Select some text and press <kbd class="keyboard-shortcut">enter</kbd> to search for that text in a new tab</li>
                <li>Select some text and press <kbd class="keyboard-shortcut">p</kbd> for the previous match for that text</li>
                <li>Select some text and press <kbd class="keyboard-shortcut">n</kbd> for the next match for that text</li>
            </ul>
            </div>
        </section>

    </section>

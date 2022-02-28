<script context="module" lang="ts">
	export const prerender = true;

        export async function load({ url }) {
          
          const res = await fetch("http://localhost:8910/api/v2/search/getInitInfo");

          return {
            status: res.status,
            props: {
              serverInfo: res.ok && (await res.json()),
              query: url.searchParams.get('q') || '',
              isRegexSearch: url.searchParams.get('regex') === 'true',
              isContextEnabled: url.searchParams.get('context') === 'true', 
              isCaseSensitive: url.searchParams.get('fold_case') === 'true',
            }
          }
        }
</script>

<script lang="ts">
	import Counter from '$lib/Counter.svelte';
        import CodeResult from '$lib/CodeResult/index.svelte';
        import FileHeader from '$lib/CodeResult/header.svelte';
    import { onMount } from 'svelte'

    let indexName = "testing"
    let backends = [{ id: "id", indexName: "testing" }]
    let sampleRepo = "xvandish/go-photo-thing"

    export let serverInfo = {};
    let sampleRes = { code: [], files: [], stats: { totalTime: -1, }};

    // TODO:
    // 1. Load Controls state based on the URL search params
    // 2. Implement the search that calls /api/v1/search based on the query
    //    1. Actually call the api
    //    2. Render the FileResults
    //    3. Render the MatchResults
    //    4. Deal with cancelling/not-displaying requests that were sent but not valid anymore
    // 3. Hook the search up so it get's called as a user types, or on initial page load
    // 4. The CSS in codesearch needs a real tune up for interactivity/responsiveness
    // 5. Inline the SearchControls into the Search line
    // 6. Add back the repo selector + add an API endpoint that lets us know what repos are possible,
    //    since the data won't be available to use at render time anymore

    // TODO: Add a (show more) button under the file results if there are more than 10
    // TODO: Inline most of the query controls into the search bar
    // TODO: See what we can do about the fact that case has 3 possible states - 
    //      maybe reduce down to two? although I hate to lose the functionality


    // -------------- Functions --------------------

    function shorten(ref) {
        var match = /^refs\/(tags|branches)\/(.*)/.exec(ref);
        if (match)
            return match[2];
        match = /^([0-9a-f]{8})[0-9a-f]+$/.exec(ref);
        if (match)
            return match[1];
        // If reference is origin/foo, assume that foo is
        // the branch name.
        match = /^origin\/(.*)/.exec(ref);
        if (match) {
            return match[1];
        }
        return ref;
    }

    /* function url(tree, version, path, lno) { */
    /*     // going to assume internalViewRepos is a map, even if I have */
    /*     // to transform it somewhere */
    /*     if (tree in internalViewRepos) { */
    /*         return internalUrl(tree, path, lno); */
    /*     } else { */
    /*         return externalRepoUrl(tree, version, path, lno); */
    /*     } */
    /* } */

    /* function internalUrl(tree, path, lno) { */
    /*     path = path.replace(/^\/+/, '');  // Trim any leading slashes */
    /*     var url = "/view/" + tree + "/" + path; */
    /*     if (lno !== undefined) { */
    /*         url += "#L" + lno; */
    /*     } */
    /*     return url; */
    /* } */

    
    /* function externalRepoUrl(tree, version, path, lno) { */
    /*     // the backend of the most recent search */
    /*     var backend = Codesearch.in_flight.backend; */
    /*     var repo_map = CodesearchUI.repo_urls[backend]; */
    /*     if (!repo_map) { */
    /*         return null; */
    /*     } */
    /*     if (!repo_map[tree]) { */
    /*         return null; */
    /*     } */
    /*     return externalUrl(repo_map[tree], tree, version, path, lno); */
    /* } */

    // Rather than not display anything at all if we know there's a
    // future incoming fetch request, let's instead show a 
    // "loading..." indicator to indicate that more results are loading
    // We can decide how best to handle the fetches that won't be used later
    // on.

    // while I'm here I can implement this using websockets maybe?
    // can detect browser functionality.

    export let isRegexSearch = false;
    export let isContextEnabled = false;
    export let isCaseSensitive = false;
    export let searchOptions = {
      q: '',
      regex: false,
      context: false,
      case: false,
    };

    function toggleOption(option) {
      searchOptions[option] = !searchOptions[option];
      updateSearchParamState();
    }

    function toggleRegex() {
      isRegexSearch = !isRegexSearch;
      updateSearchParamState();
    }

    function toggleContext() {
      isContextEnabled = !isContextEnabled;
      updateSearchParamState();
    }

    function toggleCaseSensitivity() {
      console.log('toggle func called');
      isCaseSensitive = !isCaseSensitive;
      updateSearchParamState();
    }


    // at the moment super simple
    export let query;
    
    function clearQuery() {
      query = '';
      updateSearchParamState();
    }

    function updateQuery(inputEvnt) {
      query = inputEvnt.target.value;
      console.log('updateQuery: ', inputEvnt.target.value);
      updateSearchParamState();
    }
      

    function updateSearchParamState() {
      // TODO: this is run on initial page load, which it probably shouldn't be
      // it might mess up links, and it also pollutes the browser history
      if (typeof window === 'undefined') return;
      console.log('updateSearchParamState called');
      /* if (query === '') return; */
      var url = new URL(window.location);

      url.searchParams.set("q", encodeURIComponent(query));
      url.searchParams.set("regex", isRegexSearch);
      url.searchParams.set("context", isContextEnabled);
      url.searchParams.set("fold_case", isCaseSensitive);
      window.history.pushState({}, '', url);
      doSearch();
      /* window.location.search = searchParams.toString(); */
    }

    // getting mixed results here
    async function doSearch() {
      if (query === '') {
        // clear the previous results
        sampleRes = { code: [], files: [], stats: { totalTime: -1, }};
        console.log('resetting sample_res');
        console.log(sampleRes.stats.totalTime);
        return;
      };
      console.log('making new query');
      const res = await fetch(`http://localhost:8910/api/v1/search/?q=${query}&fold_case=${!isCaseSensitive}&regex=${isRegexSearch}&context=${isContextEnabled}`);
      const inf = await res.json();
      let shaped = reshapeResults(inf);
      sampleRes.code = [...shaped.code];
      sampleRes.files = [...shaped.files];
      sampleRes.stats = {...shaped.stats};
    }

    // run a search when we initially mount in case we need to. if we don't, doSearch
    // will short circuit anyways
    onMount(() => doSearch());

    
    // TODO: move this reshaping of results into the server
    // TODO: also add something that counts + tells us the number of results,
    //       see codesearch_ui.js#L755 for reference
    //
    // https://source.static.kevinlin.info/webgrep/file/src/server/logic/search.js#l130
    // THIS IS A COPY PASTE OF THE DEDUPING FUNCTION THAT WEBGREP BY KEVIN LIN USES - 
    // ALL CREDIT TO ABOVE. I'll eventually port this to server side work, but for this
    // initial start this will remain client side
    /**
    * Massage the response from livegrep into a form that can be more easily interpreted by the
    * webgrep frontend.
    *
    * @param {Object} data Raw response payload from livegrep.
    * @return {Object} Object of code and file results, and search stats.
    * @private
    */
   function reshapeResults(data) {  // eslint-disable-line class-methods-use-this
     let codeMatches = 0;
     let fileMatches = data.file_results.length;
     const code = Object.values(data.results
       // Aggregate lines by repo and path, so that each unique (repo, path) combination is
       // described by an array of all matching lines and the left/right bounds for each line.
       .reduce((aggregated, result) => {
         // Aggregation key: combine all results for the same file in the same repo
         const key = `${result.tree}-${result.path}`;
         // Line number of the matching line
         const lineNumber = parseInt(result.lno, 10);
         // The existing entry for this repo/path combination, if it exists
         const existing = aggregated[key] || {};
 
         // Create a map of line numbers -> { bounds description, line }, being careful not to
         // override the bounds if they have already been specified. Since context lines are
         // overlapping, it's possible that a context line does not have a bounds description,
         // but it has one from an earlier result.
         const contextLines = [
           ...result.context_before.reverse(),
           result.line,
           ...result.context_after,
         ].reduce((lines, line, idx) => {
          /* console.log({ lines, line, idx }); */
           const contextLno = idx + lineNumber - result.context_before.length;
           /* console.log({ contextLno }); */
           const bounds = (() => {
             // Examining the matching line, for which bounds information is available
             if (contextLno === lineNumber) {
              codeMatches += 1;
              /* console.log('matching line'); */
               return [result.bounds[0], result.bounds[1]];
             }
 
             // Defer to existing bounds information
             return existing.lines &&
               existing.lines[contextLno] &&
               existing.lines[contextLno].bounds;
           })();
 
           return {
             ...lines,
             [contextLno]: { bounds, line },
           };
         }, {});
 
         return {
           ...aggregated,
           [key]: {
             repo: result.tree,
             version: result.version,
             path: result.path,
             lines: {
               ...existing.lines || {},
               ...contextLines,
             },
           },
         };
       }, {}))
       .map((resultGroup) => ({
         ...resultGroup,
         lines: Object.entries(resultGroup.lines)
           .map(([number, details]) => ({
             ...details,
             number: parseInt(number, 10),
           }))
           .sort((a, b) => a.number - b.number),
       }));
 
     const files = Object.values(data.file_results.reduce((acc, file) => ({
       ...acc,
       // Deduplicate results keyed by its repository and file path
       [`${file.tree}-${file.path}`]: {
         repo: file.tree,
         version: file.version,
         path: file.path,
         bounds: [file.bounds[0], file.bounds[1]],
       },
     }), {}));

    const stats = {
      exitReason: data.info.why,
      totalTime: parseInt(data.info.total_time, 10),
      totalMatches: data.search_type === 'filename_only' ? fileMatches : codeMatches
    }
 
     return { code, files, stats };
   }

  // TODO: Move the auto "case" option into a dropdown that clicking the button will trigger
</script>


<svelte:head>
    <title>Home</title>
</svelte:head>

<div id='searcharea'>
  <div class="search-inputs">
    <div class="input-line">
      <div class="inline-search-options left">
        <label for="searchbox">Query:</label>
      </div>
      <div class="inline-search-options">
        <button type="button" class="regex-toggle" on:click={toggleCaseSensitivity} data-selected={isCaseSensitive}>
          <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
            <g id="regular-expression">
                    <path id="upper-case" d="M7.53 7L4 17h2.063l.72-2.406h3.624l.72 2.406h2.062L9.65 7h-2.12zm1.064 1.53L9.938 13H7.25l1.344-4.47z"/>
                    <path id="lower-case" d="M18.55 17l-.184-1.035h-.055c-.35.44-.71.747-1.08.92-.37.167-.85.25-1.44.25-.564 0-.955-.208-1.377-.625-.42-.418-.627-1.012-.627-1.784 0-.808.283-1.403.846-1.784.568-.386 1.193-.607 2.208-.64l1.322-.04v-.335c0-.772-.396-1.158-1.187-1.158-.61 0-1.325.18-2.147.55l-.688-1.4c.877-.46 1.85-.69 2.916-.69 1.024 0 1.59.22 2.134.662.545.445.818 1.12.818 2.03V17h-1.45m-.394-3.527l-.802.027c-.604.018-1.054.127-1.35.327-.294.2-.442.504-.442.912 0 .58.336.87 1.008.87.48 0 .865-.137 1.152-.414.29-.277.436-.645.436-1.103v-.627"/>
            </g>
          </svg>
        </button>
        <button type="button" class="regex-toggle" on:click={toggleRegex} data-selected={isRegexSearch} title="{isRegexSearch ? "Don't use" : "Use"} Regex">
          <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">
            <g id="regular-expression">
                    <path id="left-bracket" d="M3 12.045c0-.99.15-1.915.45-2.777A6.886 6.886 0 0 1 4.764 7H6.23a7.923 7.923 0 0 0-1.25 2.374 8.563 8.563 0 0 0 .007 5.314c.29.85.7 1.622 1.23 2.312h-1.45a6.53 6.53 0 0 1-1.314-2.223 8.126 8.126 0 0 1-.45-2.732"/>
                    <path id="dot" d="M10 16a1 1 0 1 1-2 0 1 1 0 0 1 2 0z"/>
                    <path id="star" d="M14.25 7.013l-.24 2.156 2.187-.61.193 1.47-1.992.14 1.307 1.74-1.33.71-.914-1.833-.8 1.822-1.38-.698 1.296-1.74-1.98-.152.23-1.464 2.14.61-.24-2.158h1.534"/>
                    <path id="right-bracket" d="M21 12.045c0 .982-.152 1.896-.457 2.744A6.51 6.51 0 0 1 19.236 17h-1.453a8.017 8.017 0 0 0 1.225-2.31c.29-.855.434-1.74.434-2.66 0-.91-.14-1.797-.422-2.66a7.913 7.913 0 0 0-1.248-2.374h1.465a6.764 6.764 0 0 1 1.313 2.28c.3.86.45 1.782.45 2.764"/>
            </g>
          </svg>
        </button>
        <button type="button" class="regex-toggle" on:click={toggleContext} data-selected={isContextEnabled}>
          <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="feather feather-align-center"><line x1="18" y1="10" x2="6" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/><line x1="21" y1="14" x2="3" y2="14"/><line x1="18" y1="18" x2="6" y2="18"/></svg>
        </button>

        <button type="button" class="clear-input" on:click={clearQuery}>
          <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="feather feather-x"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>
      <div class="query-input-wrapper">
        <input type="text" bind:value={query} on:input={updateQuery} id='searchbox' tabindex="1" required="required" />
      </div>
    </div>
  </div>
    <div id='regex-error'>
      <span id='errortext'></span>
    </div>

    <div class='query-hint'>
      Special terms:
      <code>path:</code>
      <code>-path:</code>
      <code>repo:</code>
      <code>-repo:</code>
      <code>max_matches:</code>
    </div>

  <div class="search-options">
    {#if backends.length > 1}
      <div class="search-option">
        <span class="label">Search:</span>
        <select id='backend' tabindex="7">
        {#each backends as bk (bk.id)}
          <option value={bk.id}>{bk.name}</option>
        {/each}
        </select>
      </div>
    {:else}
        <select id='backend' style='display: none;'>
          <option value={backends[0].id}>{backends[0].name}</option>
        </select>
        {#if backends[0].name !== "-"}
          <div class="search-option">
            <span class="label">Searching:</span>
            {backends[0].indexName}
          </div>
        {/if}
    {/if}
  </div>
</div>

<div id='resultbox'>
    <div class:hidden={query !== ''} id='helparea'>
    <div class='helpsection'><h5>Special query terms</h5></div>
    <table>
        <tr>
        <td><code>path:</code></td>
        <td>Only include results from matching files.</td>
        <td><a href="/search?q=hello+path:test">example</a></td>
        </tr>
        <tr>
        <td><code>-path:</code></td>
        <td>Exclude results from matching files.</td>
        <td><a href="/search?q=hello+-path:test">example</a></td>
        </tr>
        <tr>
        <td><code>repo:</code></td>
        <td>Only include results from matching repositories.</td>
        <td><a href="/search?q=hello+repo:{sampleRepo}">example</a></td>
        </tr>
        <tr>
        <td><code>-repo:</code></td>
        <td>Exclude results from matching repositories.</td>
        <td><a href="/search?q=hello+-repo:{sampleRepo}">example</a></td>
        </tr>
        <tr>
        <td><code>max_matches:</code></td>
        <td>Adjust the limit on number of matching lines returned.</td>
        <td><a href="/search?q=hello+max_matches:5">example</a></td>
        </tr>
        <tr>
        <td><code>(<em>special-term</em>:)</code></td>
        <td>Escape one of the above terms by wrapping it in parentheses (with regex enabled).</td>
        <td><a href="/search?q=(file:)&regex=true">example</a></td>
        </tr>
    </table>
    <div class='helpsection'><h5>Regular Expressions</h5></div>
    <p>
        See <a href="https://github.com/google/re2/wiki/Syntax">the RE2
        documentation</a> for a complete listing of supported regex syntax.
    </p>
</div>
<div id='resultarea' class:hidden={sampleRes.stats.totalTime === -1}>
  <div id='countarea'>
    <span id='numresults'>{sampleRes.stats.totalMatches}{sampleRes.stats.exitReason !== 'NONE' ? "+" : ""}</span> matches found
    <span id='searchtimebox'>
      <span class='label'>
        /
      </span>
      <span id='searchtime'>
        {sampleRes.stats.totalTime / 1000}s
      </span>
    </span>
  </div>
  <div class:hidden={query === ''} id='results' tabindex='-1'>
  <div id="file-results">
    {#each sampleRes.files.slice(0,10) as f (f)}
      <FileHeader path={f.path} repo={f.repo} numMatches={-1} bounds={f.bounds} />
    {/each}
  </div>
  <!-- keying by the entire object is unfortunate, maybe we want to create an id -->
  <!-- but if we don't do this, then lines get-reused and so have bad highlighting -->
  <div id="code-results">
    {#each sampleRes.code as cr (cr)}
      <CodeResult {...cr} />
    {/each}
  </div>
  </div>
</div>
<p class='credit'>
Livegrep project &copy; Nelson Elhage
</p>
</div>


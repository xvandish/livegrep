<script context="module" lang="ts">
	export const prerender = true;
</script>

<script lang="ts">
	import Counter from '$lib/Counter.svelte';
    import { onMount } from 'svelte'

    let indexName = "testing"
    let backends = [{ id: "id", indexName: "testing" }]
    let sampleRepo = "xvandish/go-photo-thing"
    let repoUrls = {} // keyed by backendId
    let internalViewRepos = {} // these are the repos that we can view with 
    let defaultSearchRepos = [] // these are the repos taht will be used for the help
    let linkConfigs = [] // or a mapped version

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

    /* function getInitialInfo() {} */

    /* function doSearch() {} */

</script>


<svelte:head>
	<title>Home</title>
    <link rel="stylesheet" href="../../static/css/codesearch.css" />
</svelte:head>

<div id='searcharea'>
  <div class="search-inputs">
    <div class="prefixed-input filter-code">
      <label class="prefix-label" for="searchbox">Query:</label>
      <input type="text" id='searchbox' tabindex="1" required="required" />
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
  </div>

  <div class="search-options">
    <div class="search-option">
      <span class="label">Case:</span>
      <input type='radio' name='fold_case' value='false' id='case-match' tabindex="3" />
      <label for='case-match'>match</label>
      <input type='radio' name='fold_case' value='auto' id='case-auto' tabindex="4" />
      <label for='case-auto'>auto</label>
      [<span class="tooltip-target">?<div class="tooltip">
        Case-sensitive if the query contains capital letters
      </div></span>]
      <input type='radio' name='fold_case' value='true' id='case-ignore' tabindex="5" />
      <label for='case-ignore'>ignore</label>
    </div>

    <div class="search-option">
      <span class="label">Regex:</span>
      <input type='checkbox' name='regex' id='regex' tabindex="6" />
      <label for='regex'>on</label>
    </div>

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

    <div class="search-option">
      <span class="label">Context:</span>
      <input type='checkbox' name='context' id='context' tabindex="8" checked="CHECKED" />
      <label for='context'>on</label>
    </div>
  </div>
</div>

<div id='resultbox'>
    <div id='helparea'>
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
<div id='resultarea'>
  <div id='countarea'>
    <span id='numresults'>0</span> matches found
    <span id='searchtimebox'>
      <span class='label'>
        /
      </span>
      <span id='searchtime'>
      </span>
    </span>
  </div>
  <div id='results' tabindex='-1'>
  </div>
</div>
<p class='credit'>
Livegrep project &copy; Nelson Elhage
</p>
</div>


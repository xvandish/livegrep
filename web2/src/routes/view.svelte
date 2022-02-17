<script lang="ts">

    // get the repoName and path from query parameters
    import { page } from '$app/stores';
    import { onMount } from 'svelte';

    // page.url.searchParams is a readable store, don't write to it
    const repo = $page.url.searchParams.get('repo')
    const filePath = $page.url.searchParams.get('filePath')
    // then query /getFileInfo
    // then display the result. Straightforward enough
    let fileInfo;
    let isLoading = true;

    onMount(async () => {
        console.log('on mount file fetch');
        const res = await fetch(`http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`);
        const initInfo = await res.json();
        console.log({ initInfo });
        fileInfo = initInfo
        isLoading = false;

        /* console.log(initInfo.data.FileContent.Content.split('\n').length) */
    });
</script>


<svelte:head>
	<title>File View</title>
    <link rel="stylesheet" href="../../static/css/codesearch.css" />
</svelte:head>

{#if isLoading}
    <div>
        <p>Loading...</p>
    </div>
{/if}

{#if !isLoading}
    <section class="file-viewer">
        <header class="header">
            <nav class="header-title">
                <a href="/view?repo={fileInfo["repo_info"].name}" class="path-segment repo" title="Repository: {fileInfo["repo_info"].name}">{fileInfo.repo_info.name}</a>:
                {#each fileInfo.data.PathSegments as pSeg, idx}
                    {#if idx > 0}/{/if}<a href={pSeg.Path} class="path-segment">{pSeg.Name}</a>
                {/each}
            </nav>
            <ul class="header-actions without-selection">
            <li class="header-action">
                <a data-action-name="search" title="Perform a new search. Keyboard shortcut: /" href="#">new search [<span class='shortcut'>/</span>]</a>
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
                    {/each}
                </ul>
                Not supported yet
            {/if}
            {#if fileInfo.data.FileContent}
                <div class="file-content">
                    <code id="source-code" class="code-pane language={fileInfo.data.FileContent.Language}">{fileInfo.data.FileContent.Content}</code>
                    <div id="line-numbers" class="line-numbers hide-links">
                        {#each {length: fileInfo.data.FileContent.LineCount +1} as _, lno}
                            <a id="L{lno+1}" href="#L{lno+1}">{lno+1}</a>
                        {/each}
                    </div>
                </div>
            {/if}
        </div>
    </section>
{/if}


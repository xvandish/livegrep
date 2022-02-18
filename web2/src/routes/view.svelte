<script lang="ts" context="module">

    // get the repoName and path from query parameters
    import { page } from '$app/stores';
    import { onMount } from 'svelte';

    /** @type {import('@sveltejs/kit').Load} */
    export async function load({ params, fetch, url, session, stuff }) {
	const repo = url.searchParams.get("repo");
	const filePath = url.searchParams.get("path");

        const path = `http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`
        const res = await fetch(`http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`);

        return {
            status: res.status,
            props: {
                fileInfo: res.ok && (await res.json())
            }
        };
    }
</script>

<script>
    export let fileInfo;
        console.log('whaaat');
        console.log({ fileInfo });
</script>

<svelte:head>
    <title>File View</title>
</svelte:head>


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
                                <a href="something">{dirEntry.Name}{#if dirEntry.IsDir}/{/if}</a>
                            {/if}
                            {#if dirEntry.SymlinkTarget}
                                &rarr; (<span class="symlink-target">{dirEntry.SymlinkTarget}</span>)
                            {/if}
                        </li>
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


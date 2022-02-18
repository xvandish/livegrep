<script lang="ts" context="module">

    // get the repoName and path from query parameters
    import { page } from '$app/stores';
    import { onMount } from 'svelte';

    /** @type {import('@sveltejs/kit').Load} */
    export async function load({ params, fetch, session, stuff }) {
	const repo = `${params.org}/${params.repo}`;
	const filePath = params.file;

        const path = `http://localhost:8910/api/v2/getFileInfo?repo=${repo}&path=${filePath}`
        console.log({ path });
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
    </section>

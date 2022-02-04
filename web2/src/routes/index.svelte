<script context="module" lang="ts">
	export const prerender = true;
</script>

<script lang="ts">
    import { onMount } from 'svelte'
    // to avoid doing this, I could have the golang server 301 to /search
    // and this index route be whats in search.svelte right now..
    onMount(() => {window.location.href = '/search' });
</script>



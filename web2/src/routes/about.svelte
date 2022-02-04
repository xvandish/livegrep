<script context="module">
	import { browser, dev } from '$app/env';

	// we don't need any JS on this page, though we'll load
	// it in dev so that we get hot module replacement...
	export const hydrate = dev;

	// ...but if the client-side router is already loaded
	// (i.e. we came here from elsewhere in the app), use it
	export const router = browser;

	// since there's no dynamic data here, we can prerender
	// it so that it gets served as a static asset in prod
	export const prerender = true;
</script>

<svelte:head>
	<title>About</title>
</svelte:head>

<div class="content">
    <div class='textarea'>
        <p>
            This livegrep project was an experiment in scaling real-time
            regex search, inspired in part by the death of <a
            href="http://google.com/codesearch">Google Codesearch.</a>
        </p>
        <p>
            livegrep uses Russ Cox's excellent <a
            href="http://code.google.com/p/re2/">RE2</a> regular expression
            library, underneath a custom indexing and searching backend
            written in C++ with pthreads. The frontend is implemented in <a
            href="http://golang.org/">Go</a>.
        </p>
        <p>
            livegrep is written and run by <a
            href="https://nelhage.com/">Nelson Elhage</a> and is <a
            href="https://github.com/livegrep/livegrep">open source</a>.
        </p>
    </div>
</div>

<style>
	.content {
		width: 100%;
		max-width: var(--column-width);
		margin: var(--column-margin-top) auto 0 auto;
	}
</style>

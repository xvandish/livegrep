<script>
	import Header from './header.svelte';
	import CodeLine from './code-line.svelte';

	export let repo;
	export let path;
	export let lines;
	export let version;
	export let urlPattern = '';

	const numMatches = lines.reduce((acc, line) => (line.bounds ? acc + 1 : acc), 0);
</script>

<div class="code-result">
	<Header {repo} {path} {urlPattern} {numMatches} />
	<div class="cr-actions" />
	<div class="cr-lines">
		{#each lines as line (line)}
			<CodeLine {...line} {repo} {path} {urlPattern} />
		{/each}
	</div>
</div>

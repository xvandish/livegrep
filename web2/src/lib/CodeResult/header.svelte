<script>
  export let repo
  export let path
  export let urlPattern
  export let numMatches
  export let bounds = []

	let repoUrl = ""
	let pathUrl = ""

  let prefixPart = ""
  let highlightedPart = ""	  
  let suffixPart = ""
  let isHighlighted = false
  if (bounds.length > 0) {
    isHighlighted = true
    let [start, end] = bounds
    prefixPart = path.substring(0, start);
    highlightedPart = path.substring(start, end);
    suffixPart = path.substring(end);
  }

</script>


<div class="cr-header">
	<div class="links">
		<a class="repo-link" href={repoUrl}>{repo}</a>:
		<a class="path-link" href={pathUrl}>
	{#if isHighlighted}
		<span>{prefixPart}</span><span class="highlighted">{highlightedPart}</span><span>{suffixPart}</span>
	{:else}
		<span>{path}</span>
	{/if}
		</a>
	</div>
	{#if numMatches !== -1}
	<div class="matches">{numMatches} {numMatches === 1 ? 'match': 'matches'} </div>
	{/if}
</div>

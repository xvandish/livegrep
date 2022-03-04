<script>
  export let lno 
  export let line
  export let bounds = [] 
  export let repo 
  export let path
  export let urlPattern = ''

  let prefixPart = ""
  let highlightedPart = ""	  
  let suffixPart = ""
  let isHighlighted = false


  if (bounds && bounds.length > 0) {
    isHighlighted = true
    let [start, end] = bounds
    prefixPart = line.substring(0, start);
    highlightedPart = line.substring(start, end);
    suffixPart = line.substring(end);
  }

  const lineLink = `/view/${repo}/${path}#L${lno}`
</script>

<div class="code-line">
<a
	sveltekit:prefetch
	rel="noreferrer noopener"
	href={lineLink}
	class="num-link"
>
<span class="num" class:bold={bounds && bounds.length > 0}>{lno}</span>
</a>
	<div class="line">
	{#if isHighlighted}
		<span>{prefixPart}</span><span class="highlighted">{highlightedPart}</span><span>{suffixPart}</span>
	{:else}
		<span>{line}</span>
	{/if}
	</div>
</div>

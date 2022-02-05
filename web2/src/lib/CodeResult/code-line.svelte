<script>
  export let number 
  export let line
  export let bounds = [] 
  export let href = ""

  let prefixPart = ""
  let highlightedPart = ""	  
  let suffixPart = ""
  let isHighlighted = false


  if (bounds.length > 0) {
    isHighlighted = true
    let [start, end] = bounds
    prefixPart = line.substring(0, start);
    highlightedPart = line.substring(start, end);
    suffixPart = line.substring(end);
  }
</script>

<div class="code-line">
<a
	rel="noreferrer noopener"
	target="_blank"
	href={href}
	class="num-link"
>
<span class="num" class:bold={bounds.length > 0}>{number}</span>
</a>
	<div class="line">
	{#if isHighlighted}
		<span>{prefixPart}</span><span class="highlighted">{highlightedPart}</span><span>{suffixPart}</span>
	{:else}
		<span>{line}</span>
	{/if}
	</div>
</div>

(function f() {
  const blameLinks = document.querySelectorAll('#blamefile #hashes a');

  const matchingLinksCache = {}; // commitHash: NodeList
  function getLinksWithSameCommitHash(target) {
    const href = target.getAttribute('href') || "";
    const i = href.indexOf('.');
    if (i == -1) return { matchingLinks: [], charAfterDot: '' };
    const commitHash = href.substring(0, i);

    let matchingLinks = matchingLinksCache[commitHash];
    if (!matchingLinks) {
      // fetch
      matchingLinks = document.querySelectorAll(
        '#blamefile #hashes a[href^="' + commitHash + '"]'
      );
      matchingLinksCache[commitHash] = matchingLinks;
    }
    return { matchingLinks, charAfterDot: href.substr(i+1, 1) };
  }

  function highlightMatchingLines(event) {
    const { matchingLinks, charAfterDot } = getLinksWithSameCommitHash(event.target);
    for (var idx = 0; idx < matchingLinks.length; idx++) {
      matchingLinks[idx].classList.add('highlight', charAfterDot);
    }
  }

  function removeHighlightFromMatchingLines(event) {
    const { matchingLinks, charAfterDot } = getLinksWithSameCommitHash(event.target);
    for (var idx = 0; idx < matchingLinks.length; idx++) {
      matchingLinks[idx].classList.remove('highlight', charAfterDot);
    }
  }

  for (var i = 0; i < blameLinks.length; i++) {
    blameLinks[i].addEventListener('mouseenter', highlightMatchingLines);
    blameLinks[i].addEventListener('mouseleave', removeHighlightFromMatchingLines);
  }

  /* When the user clicks a hash, remember the line's y coordinate,
         and warp it back to its current location when we land. */
  // TODO: this wasn't working anyways

  //   $body.on("click", "#hashes > a", function (e) {
  //     var y = $(e.currentTarget).offset().top - $(window).scrollTop();
  //     Cookies.set("pre", y, { expires: 1 });
  //     // (Then, let the click proceed with its usual effect.)
  //   });

  //   var previous_y = Cookies.get("pre");
  //   if (typeof previous_y !== "undefined") {
  //     Cookies.remove("pre");
  //   }

  // After the dom loads, check if we have a line # selected
  // if we do, attempt to scroll to it -
  // not sure how target
  window.addEventListener('DOMContentLoaded', function () {
    let lineNum = window.location.hash;
    if (!lineNum) return;

    // strip the leading # character e.g. #56
    lineNum = lineNum.substr(1);

    // otherwise, get the <a id={lineNum} and scroll to it
    const target = document.getElementById(lineNum);

    // when I later refactor the blame links, I can use this function instead,
    // while taking into consideration the postion of a previously clicked blame
    // link (the one stored into cookies above)
    // idk what to tell say - without this timeout, this line behaves
    // like default scrollIntoView(), e.g. scrollIntoView({ block: 'start' })
    setTimeout(function () {
      target.scrollIntoView({ block: 'center' });
    }, 2);
  });
})();

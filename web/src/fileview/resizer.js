/*
* Given a resize element, resize either the pane/element to either the left
* or right of the element, depending on the "direction" of the resizer.
* Variants of this code are fairly common in the wild, see 
* https://sourcegraph.com/github.com/JetBrains/IntelliJ-Log-Analyzer/-/blob/frontend/src/assets/js/resizer.js?L49:13&subtree=true
*/
export function resizable(resizer) {
    const direction = resizer.getAttribute("data-direction");
    const prevSibling = resizer.previousElementSibling;
    const nextSibling = resizer.nextElementSibling;

    // get current mouse position
    let x = 0;
    let y = 0;
    let prevSiblingHeight = 0;
    let prevSiblingWidth = 0;
    let nextSiblingHeight = 0;

    function mouseDownHandler(e) {
      e.preventDefault();
      x = e.clientX;
      y = e.clientY;
      const rect = prevSibling.getBoundingClientRect();
      prevSiblingHeight = rect.height;
      prevSiblingWidth = rect.width;
      nextSiblingHeight = nextSibling.getBoundingClientRect().height;
      
      // don't allow prev/next siblings to be interacted with until the
      // mouse is released
      prevSibling.style.userSelect = 'none';
      prevSibling.style.pointerEvents = 'none';

      nextSibling.style.userSelect = 'none';
      nextSibling.style.pointerEvents = 'none';

      // addatch the listeners for move/release to document
      document.addEventListener('mousemove', mouseMoveHandler);
      document.addEventListener('mouseup', mouseUpHandler);
    }

    function mouseMoveHandler(e) {
        // How far the mouse has been moved
        const dx = e.clientX - x;
        const dy = e.clientY - y;

        switch (direction) {
            case 'vertical':
                // most code in the wild does prevSiblingHeight + dy
                // however, in our case we want to update the height of the element
                // visually below, programatically "next" when using the vertical splitter
                // so we use nextSiblingHeight - dy
                const h =
                    ((nextSiblingHeight - dy) * 100) /
                    resizer.parentNode.getBoundingClientRect().height;
                console.log({ nextSiblingHeight, dy, y, h })
                nextSibling.style.height = `${h}%`;
                break;
            case 'horizontal':
            default:
                const w =
                    ((prevSiblingWidth + dx) * 100) / resizer.parentNode.getBoundingClientRect().width;
                prevSibling.style.width = `${w}%`;
                break;
        }

        const cursor = direction === 'horizontal' ? 'col-resize' : 'row-resize';
        resizer.style.cursor = cursor;
        document.body.style.cursor = cursor;
    }

    function mouseUpHandler() {
        resizer.style.removeProperty('cursor');
        document.body.style.removeProperty('cursor');

        prevSibling.style.removeProperty('user-select');
        prevSibling.style.removeProperty('pointer-events');

        nextSibling.style.removeProperty('user-select');
        nextSibling.style.removeProperty('pointer-events');

        // Remove the handlers of `mousemove` and `mouseup`
        document.removeEventListener('mousemove', mouseMoveHandler);
        document.removeEventListener('mouseup', mouseUpHandler);
    }

    resizer.addEventListener('mousedown', mouseDownHandler);
}

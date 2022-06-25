var functionMap = {
  "expandText": expandText,
  "getNextPage": getNextCommits
};


// we can request a server-rendered-page and then embed-the html that
// comes back.
// how do we deal with the next button? replace it on every-render so
// that its state is correct?
function getNextCommits(event) {
  event.target.disabled = true; // don't allow a click while one in flight

  // we need to get the hash of the currently displayed last commit
  // it would be nice if something computed it for us. 
  var new_url = new URL(window.location.href);
  new_url.searchParams.set('firstParent', event.target.value);
  new_url.searchParams.set('partial', 'true');

  var enableBtn = false;
  fetch(new_url)
      .then(function (r) {
        var nextParent = r.headers.get("X-next-parent");
        enableBtn = r.headers.get("X-maybe-last") === "false";  
        event.target.value = nextParent;
        
        return r.text();  
        })
      .then(function (html) {
        event.target.insertAdjacentHTML("beforebegin", html);
        if (enableBtn) {
          event.target.disabled = false;
        }
      });
}

function expandText(event) {
  // Get the row that triggered this click
  var row = event.target.closest('tr');
  row.querySelector('.expanded-row-content').classList.toggle('hide-row');
}

function dealWithClick(event) {
  if (event.target.tagName != "BUTTON") {
      return
  }

  functionMap[event.target.dataset.action](event);
}

// We use a global handler, since we dynamically add commit-rows on
// pagination
document.addEventListener('click', function(e) {
  if (event.target.tagName != "BUTTON") {
    return
  };

  var action = event.target.getAttribute('data-action');
  if (action == "expandText") {
    expandText(e);
  } else if (action == "getNextPage") {
    getNextCommits(event);
  }

});

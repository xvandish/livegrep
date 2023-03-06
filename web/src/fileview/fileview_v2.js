import { resizable } from "./resizer";

var blameVisible = false;
var commitForLastBlame = "";
var linkExtractReg = /\/delve\/(.*)\/(blob|tree)\/(\w*)\/(.*)/;
var urlPathRegex = /\/experimental\/(.*)\/\+\/(.*):(.*)/;
var historyFetched = false;

function toggleBlame(e) {
  console.log(e.target.dataset);
  console.log({ blameVisible });
  console.log({ commitForLastBlame });
  var blameLines = document.querySelectorAll("td.blame-col .blame-line");

  // if the blame viewer is shown, hide it
  if (e.target.dataset.shown == "true") {
    console.log("show/hide");
    for (var i = 0; i < blameLines.length; i++) {
      blameLines[i].classList.add("hidden");
    }

    // remove row striping frome fileviewer
    var stripedRows = document.querySelectorAll("tr.blame-striped");
    for (var i = 0; i < stripedRows.length; i++) {
      stripedRows[i].classList.remove("blame-striped");
    }
    e.target.dataset.shown = "false";
    blameVisible = false;
  } else {
    // re-show the blame for the same file by just un-hiding the blame rows
    // this is problematic. When we go file->blame->diff->file->blame, on the same
    // file, we actually load back in (via network) the file. So when blame goes to re-load
    // the blame-cols it depends on are no longer there. So what we need to do is cache the contents
    // of the fileviewer in a different pane, and toggle visibility with css. That way, when switching
    // modes with the same file as what was previously being viewed, we can avoid the network, and avoid
    // the rework of recalculating blame
    if (commitForLastBlame == window.scriptData.fileCommitHash) {
      console.log("blame previously shown for the same file, redisplaying");
      for (var i = 0; i < blameLines.length; i++) {
        blameLines[i].classList.remove("hidden");
      }

      // re-calculate the striped rows
      stripeFileRowsForBlame(window.scriptData.blameMeta.blameColsValid);
      e.target.dataset.shown = "true";
      blameVisible = true;
      return;
    }

    // otherwise, load the blame anew
    // but first, delete the old blame lines
    blameLines.forEach((e) => e.remove());
    loadBlame();

    // set information for the loaded blame
    commitForLastBlame = window.scriptData.fileCommitHash;
    e.target.dataset.shown = "true";
    blameVisible = true;
  }

}

// whats the nicest, most ergonomic
function stripeFileRowsForBlame(blameColsValid) {
  for (var i = 0; i < blameColsValid.length; i++) {
    var { startRow, endRowIdx } = blameColsValid[i];

    var prevRow = startRow.previousElementSibling;
    if (!prevRow) {
      // TODO: if we add a thead, account for it
      continue;
    }

    // if prevRow is striped, leave this chunk alone
    if (prevRow.classList.contains("blame-striped")) {
      continue;
    }

    // otherwise, stripe startRow-endRow
    var currRow = startRow;
    for (var j = startRow.rowIndex; j < endRowIdx; j++) {
      currRow.classList.add("blame-striped");
      currRow = currRow.nextElementSibling;
    }
  }
}

async function loadBlame() {
  console.log("in onload func");
  var blame = await fetch(
    `/api/v2/json/git-blame/${window.scriptData.repo}/${window.scriptData.fileCommitHash}/${window.scriptData.filepath}`
  )
    .then((r) => r.json())
    .then((j) => j);

  // Now that we have blame, we need to:
  // Iterate each blame chunk.
  // get the td at each StartLine
  // fill in the <div class="blame-line">
  //

  var blameCols = document.querySelectorAll("td.blame-col");
  var blameColsValid = []; // stores an array of all the indexes of blame cols that aren't deleted, as well their rowspan
  var blameColsToDelete = []; // stores the indexes of blame cols to be removed from dom
  var seenStartLines = new Map();

  // TODO(xvandish): Add highlights/striping to alternating blame chunks
  var blame_chunks = blame.blame_chunks;
  for (var i = 0; i < blame_chunks.length; i++) {
    var chunk = blame_chunks[i];
    console.log("processing chunk with has=" + chunk.ShortHash);

    for (var j = 0; j < chunk.LineRanges.length; j++) {
      var chunkRange = chunk.LineRanges[j];
      var startLine = chunkRange.StartLine;
      var endLine = chunkRange.EndLine;

      var blameContainer = elFactory(
        "div",
        { class: "blame-line" },
        elFactory("a", { href: chunk.CommitLink }, chunk.CommitSummary)
      );

      if (seenStartLines.has(startLine - 1)) {
        console.error("startLine=" + (startLine - 1) + " has been seen before");
      }

      console.log(
        "commitHash=" +
          chunk.ShortHash +
          " startLine=" +
          startLine +
          " startLine+1=" +
          (startLine + 1)
      );
      var blameCol = blameCols[startLine - 1];
      blameCol.setAttribute("rowspan", endLine - startLine + 1);
      blameCol.style.borderBottom = "1px solid #dadcd0";
      blameCol.classList.add("blame-col");
      blameCol.appendChild(blameContainer);
      seenStartLines.set(startLine - 1, "true");

      var startRow = blameCol.closest("tr");
      blameColsValid.push({
        startRow,
        endRowIdx: startRow.rowIndex + (endLine - startLine) + 1,
      });

      // now mark all the empty blameCols in between start and end as ready to be deleted
      for (var l = startLine; l < endLine; l++) {
        blameColsToDelete.push(l);
      }

      // store the blame meta
    }
  }

  // delete the unneded blamecols
  for (var i = 0; i < blameColsToDelete.length; i++) {
    blameCols[blameColsToDelete[i]].remove();
  }

  // sort the blameCols by rowStartIdx, so we can highlight alternating chunks
  // from top to bottom
  blameColsValid.sort((a, b) => a.startRow.rowIndex - b.startRow.rowIndex);
  stripeFileRowsForBlame(blameColsValid);
  window.scriptData.blameMeta.blameColsValid = blameColsValid;
  // now highlight (in alternating mode) the blame col and the rows it spans
  // we need to sort the blameCols by the row they st
}

/*
* Toggle the history panel and splitter that is rendered over it allowing it
* to expand/collapse
*/
function toggleHistoryPanel(e) {
    if (e.target.dataset.toggled == "false") {
      e.target.dataset.toggled = "true";
    } else {
      e.target.dataset.toggled = "false";
    }

    // load the history if this is the first time opening the panel
    if (!historyFetched) {
      loadHistory();
      historyFetched = true;
    }

    historyPanel.classList.toggle("closed");
    horizontalLowerPaneSplitter.classList.toggle("hidden");
}

// TODO(xvandish): general features to add
// highlight searchString in files. We can use the q? parameter in the url
// add button to auto expand all hidden context lines
// take over browser command f for something nicer
// collapse all folders that aren't in the search path
// repo selector
// hash selector
// file search?

// TODO(xvandish): fetchHistory should only return the last `x` commits to avoid
//   overwhelming either the frontend or backend
// TODO(xvandish): Decide whether to do this by fetching SSR content or keeping it
//  as is.
var gitLogAfterCursor = 0;
var cursorStep = 1000;
// the afterCursor needs to be stored by key
// keyed by request in form revspec=x&path=y
// we also clear out 

function createAndInsertLogPaginationButtons() {
    var btnsContainer = elFactory("div", { id: "git-history-table-pagination-btns" },
        elFactory("button", { "id": "git-log-show-more", "data-type": "more", "data-cursor": cursorStep }, "Show more"),
        elFactory("button", { "id": "git-log-show-all", "data-type": "all", "data-cursor": cursorStep }, "Show all")
      );
  gitHistoryTable.parentNode.appendChild(btnsContainer);
}

async function loadHistory(paginationBtn) {
  // always fetch history at head, in case someone is viewing the file at a non head link, that way
  // they'll see future history and be able to navigate to it
  // only include revspec if we are viewing the non-head rev
  //

  var pathToLoadLogFor = window.scriptData.filepath;

  // if the log currently shown is for a different file, reset the afterCount
  if (gitHistoryTable.dataset.filepath != pathToLoadLogFor) {
    gitLogAfterCursor = 0;
  } else if (paginationBtn) {
    // if this is a request for pagination, increment the gitLogAfterCursor
    gitLogAfterCursor = paginationBtn.dataset.cursor;
  }

  var sp = new URLSearchParams();
  sp.set("path", pathToLoadLogFor);
  sp.set("afterCursor", gitLogAfterCursor);
  sp.set("first", (paginationBtn && paginationBtn.dataset.type) === "all" ? "0" : cursorStep);

  if (window.scriptData.repoRev != window.scriptData.headRev) {
    // if repoRev ever changes it will trigger a reload, so don't worry about it
    // changing in between the time this function fires and it finishes
    sp.set("revspec", window.scriptData.repoRev); 
  }

  var gitHistory = await fetch(
    `/api/v2/json/git-log/${window.scriptData.repo}/?${sp.toString()}`
  )
    .then((r) => r.json())
    .then((r) => r)
    .catch(( err ) => {
      console.error("error fetching git log: " + err);
      gitHistoryTable.parentNode.insertBefore(
        elFactory("div", { "class": "error-container" }, "Error fetching commits. Check console for more")
      , gitHistoryTable.nextElementSibling);
      return new Error(err);
    });

  if (gitHistory instanceof Error) {
    return;
  }

  var commits = gitHistory.Commits;

  // if the file we're viewing has been changed, remove all the current rows in
  // the body
  var paginationButtonsContainer = document.getElementById("git-history-table-pagination-btns");
  if (gitHistoryTable.dataset.filepath != pathToLoadLogFor) {
    // delete all rows except the first  
    var rowCount = gitHistoryTable.rows.length;
    for (var i = 1; i < rowCount; i++) {
      gitHistoryTable.deleteRow(1); // always delete 1, since rows is modified on each loop
    }

    // delete the pagination buttons, even if we're about to update/remove them
    // for this new query
    if (paginationButtonsContainer) paginationButtonsContainer.remove();
  }

  if (commits.length >= cursorStep && !gitHistory.MaybeLastPage) {
    // if the pagination buttons don't exist, create them
    if (!paginationButtonsContainer) {
      createAndInsertLogPaginationButtons();
    } else { // otherwise, update the cursors for "next" and "all" buttons
      var btns = paginationButtonsContainer.querySelectorAll("btn");
      for (var i = 0; i < btns.length; i++) {
        btns[i].dataset.afterCursor = gitLogAfterCursor + cursorStep;
      }
    }
  } else if (paginationButtonsContainer) { // if theres no more to paginate, and the buttons exist 
    paginationButtonsContainer.remove();
  }

  // if we clicked on the "showAll" button, remove the pagination buttons
  if (paginationBtn && paginationBtn.dataset.type === "all") {
    paginationButtonsContainer.remove();
  }


  var foundMatchingCommit = false;
  for (var i = 0; i < commits.length; i++) {
    const tr = gitHistoryTable.insertRow();
    var commit = commits[i];
    tr.dataset.commit = commit.ID;

    // when viewing the repo at a given commit, not all files have a log for that commit,
    // even if they existed at that time. Hence, we're not gauranteed that any commit
    // in the history pane matches the selected one. In that case, we select the first at the
    // end of the for loop
    // TODO: when the commit matching isn't the first, scroll the commit history window so that it
    // shows as the first. Somehow, visually indicate that there are more recent commits above.
    var isMatchingCommit = false;
    if (!foundMatchingCommit && window.scriptData.fileCommitHash == commit.ID) {
      tr.classList.add("current-commit");
      isMatchingCommit = true;
      foundMatchingCommit = true;
    }

    var expandContainer = elFactory("div", { class: "row-expander" });
    if (commit.Body.length > 0) {
      var btn = elFactory(
        "button",
        { class: "icon-toggle log", "data-toggled": "true" },
        elFactory("img", { src: "/assets/img/chevron-down.svg" })
      );

      // we use the same css classes/data selectors to control various
      // things, and sometimes open means ~= face up and sometimes face down
      // so we eat the confusion and just set "toggled" to somethign
      // that means, icon down
      expandContainer.appendChild(btn);
    }

    // always add expandContainer, in case some row can expand we dont want shifts
    tr.append(
      elFactory("td", {}, expandContainer),
      elFactory(
        "td",
        {},
        elFactory(
          "a",
          { href: gitHistory.CommitLinkPrefix + "/commit/" + commit.ID },
          commit.ID.slice(0, 7)
        )
      ),
      elFactory("td", {}, commit.Author.Email),
      elFactory("td", {}, commit.Author.Date),
      elFactory("td", {}, commit.Subject)
    );

    // Now, add the action buttons that only show on hover
    var shouldDiffLinkBeHidden = false;
    // if this is the last (well, last rendered, first chronologically) commit, or we are using the fileviewer
    // on something thats not a file (directory, repo root) don't render the diff button
    if (
      (i == commits.length - 1 && gitHistory.MaybeLastPage) ||
      pathToLoadLogFor == ""
    ) {
      console.log(
        "diff link should be hidden: isLast=" +
          (i == commits.length - 1 && gitHistory.MaybeLastPage) +
          " filepath=" +
          pathToLoadLogFor
      );
      shouldDiffLinkBeHidden = true;
    }

    var fileviewButtons = elFactory(
      "div",
      { class: "fileview-buttons-container" },
      // we still inject the button since we use its data attribute to know what to diff (atm)
      elFactory(
        "a",
        {
          href: "blah",
          "data-commit": commit.ID,
          class: "diff-link" + (shouldDiffLinkBeHidden ? " hidden" : ""),
        },
        "diff"
      ),
      elFactory(
        "a",
        {
          href: "blah",
          "data-commit": commit.ID,
          class: "view-link",
          "data-toggled": isMatchingCommit ? "true" : "false",
        },
        "view"
      )
    );

    tr.appendChild(
      elFactory(
        "td",
        {},
        elFactory("div", { class: "actions-container" }, fileviewButtons)
      )
    );

    // now append the collapsible commit body
    if (commit.Body.length > 0) {
      var collapsibleCell = elFactory(
        "td",
        { class: "expanded-row-content hide-row" },
        elFactory("pre", {}, commit.Body)
      );
      tr.appendChild(collapsibleCell);
    }
  }

  if (!foundMatchingCommit && commits.length > 0) {
    // go to the first row and mark it "current-commit"
    // select the "view" button
    var firstRow = gitHistoryTable.rows[1]; // not 0 since thats the thead
    console.log({ firstRow });
    firstRow.classList.add("current-commit");
    firstRow.getElementsByClassName("view-link")[0].dataset.toggled = "true";
  }

  gitHistoryTable.dataset.filepath = pathToLoadLogFor;
}

// given a click on a tree, hide or show all children
function toggleTree(event) {
  // this event is on a button. Toggle the "closed" class on it
  event.target.classList.toggle("expanded");

  // <div>
  //   <div>click in here</div>
  //   <div class="children"></div>
  // </div>

  // get the parentContainer of the folder button/description
  var parentContainer = event.target.closest("div");
  // now get that parent
  var rootContainer = parentContainer.parentNode;
  // now, get the children container
  var childrenContainer = rootContainer.querySelector(".children");
  // and, finally, toggle the childrens expanded status
  childrenContainer.classList.toggle("expanded");
}

function getRepoRevAndFilepathFromUrl() {
  return window.location.pathname.split("/+/");
}

// TODO: create a central function that can create urls, this is getting bad

function replacePathBreadcrumbs(newPath) {
  var newPathElements = [];

  var pathSplit = newPath.split("/");
  for (var i = 0; i < pathSplit.length; i++) {
    var pathPart = pathSplit[i];
    var parentPath = pathSplit.slice(0, i).join("/");
    var fullPath = parentPath;
    if (parentPath.length > 0) {
      fullPath += "/" + pathPart;
    } else {
      fullPath += pathPart;
    }
    var link = elFactory(
      "a",
      {
        href: window.location.pathname.replace(
          urlPathRegex,
          `/experimental/$1$2:${fullPath}`
        ),
      },
      pathPart
    );
    newPathElements.push(link);
  }

  console.log({ newPathElements });
  document
    .getElementById("path-breadcrumbs")
    .replaceChildren(...newPathElements);
}

// var historyPanel = document.getElementsByClassName("lower-detail-content")[0];
// TODO(xvandish): Switch to global handler so that we use less memory
// TODO(xvandish): We need another query parameter for the following case:
//    1. user is viewing repo at hash xyz
//    2. user clicks on a file that does not have a commit entry for xyz
//    3. User opens the history panel. The first commit, `abc` is highlighted, since there is no matching commit
//    4. The user clicks "View" event though its already selected
//    5. At the point, we need to mark in the URL that we are viewing the repo at xyz and the file at `abc`.
//    Google uses the `drc` query parameter to indicate this.
// TODO(xvandish): In the same vein as the comment above, we need to change so that viewing a file at a commit
// only updates a query parameter, it does not change the commit/branch you are viewing the repo at. E/g, it should
// not change the hash after /blob/, it should instead append a query parameter. dfc or, data-file-commit makes sense
// to me.

// this looks gnarly, but captures urls of the form
// /eventtracker/org/repo/+/repoRev:filePath
async function loadFileAtCommit(event, repo, path, commitHash, clickLocation) {
  // given a path like so - experimental/nytimes/eventtracker-server/blob/HEAD/service.go
  // which is experimental/${repo}/blob/${commitHash}/${filePath}
  // var oldPath = window.location.pathname;
  // oldPath = oldPath.replace(`/experimental/${repo}/blob/${window.scriptData.commitHash}/${window.scriptData.filepath}`, "");

  var sp = new URLSearchParams(window.location.search);
  sp.set("dfc", commitHash);

  // only set dfc when clickLocation == history?
  // that way, navigation in other parts of the side
  // respects the repoCommitHash
  if (clickLocation != "commit-history") {
    sp.delete("dfc");
  }

  // replace the file pathname with the new one
  var newPathExp = window.location.pathname.replace(
    urlPathRegex,
    `/experimental/$1/+/$2:${path}`
  );
  console.log({ newPathExp });
  console.log(window.location.pathname);
  window.history.pushState({}, "", `${newPathExp}?${sp.toString()}`);
  console.log(window.location.pathname);

  if (
    window.scriptData.filepath === path &&
    window.scriptData.fileCommitHash === commitHash
  ) {
    // update the external link
    if (sp.has("dfc")) {
      var range = parseHashForLineRange(document.location.hash);
      document
        .getElementById("external-link")
        .setAttribute("href", getExternalLink(range));
    }
    return;
  }

  var fileContent = await fetch(`/raw-blob/${repo}/+/${commitHash}:${path}`)
    .then((r) => r.text())
    .then((r) => r);

  // Now replace the file content
  document.getElementsByClassName("file-content")[0].innerHTML = fileContent;

  // Replace the header that says at what hash we're at
  var commitLink = document.getElementById("short-hash-link");
  commitLink.href = `/delve/${repo}/commit/${commitHash}`;
  commitLink.innerHTML = commitHash.slice(0, 8);

  // Replace the header that tells us what file we're viewing
  var filename = path.split(/[\\/]/).pop();
  document.getElementById("filename").innerHTML = filename;

  replacePathBreadcrumbs(path);

  // TODO(xvandish): replace the url part once we actually load via urls..
  var prevFileCommitHash = window.scriptData.fileCommitHash;
  var prevFilepath = window.scriptData.filepath;
  var prevFilename = window.scriptData.filename;
  var prevRepo = window.scriptData.repo;

  // TODO: We don't really want to do this, otherwise it makes all ops behave as is we're acting on this hash,
  // e.g., viewing the repo at this hash.
  // TODO ^^^
  window.scriptData.fileCommitHash = commitHash;
  window.scriptData.filepath = path;
  window.scriptData.filename = filename;
  window.scriptData.repo = repo;
  window.scriptData.prevRepo = prevRepo;
  window.scriptData.prevFileCommitHash = prevFileCommitHash;
  window.scriptData.prevFilepath = prevFilepath;
  window.scriptData.prevFilename = prevFilename;
  window.scriptData.currentMode = "fileviewer";

  // replace the external link, depends on window.scriptData globals
  // which is why we don't do it earlier
  var range = parseHashForLineRange(document.location.hash);
  document
    .getElementById("external-link")
    .setAttribute("href", getExternalLink(range));

  if (clickLocation == "commit-history") {
    // replace the current "selected" commit in the history pane
    var currentCommitRow = document.querySelector("tr.current-commit");
    currentCommitRow.classList.remove("current-commit");

    // now select the tr that contains the "view" button that was pressed
    event.target.closest("tr").classList.add("current-commit");

    // now get the previously selected "view" button and untoggle it
    currentCommitRow.querySelector("a.view-link").dataset.toggled = "false";

    // now select the clicked button
    event.target.dataset.toggled = "true";
  } else {
    // replace the current "selected" file
    var selectedPath = document.querySelector("#side-nav div.selected");
    if (selectedPath) {
      selectedPath.classList.remove("selected");
    }

    // select the file that was clicked
    var clickedFileLink = document.querySelector(
      `#side-nav a[data-path='${path}']`
    );
    if (clickedFileLink) clickedFileLink.parentNode.classList.add("selected");
  }

  // unhide the blame button if it wasn't shown before (we started with an
  // invalid path)
  document.getElementById("toggle-blame").classList.remove("hidden");

  // if the blameLayer was open, re-open it
  if (blameVisible) {
    loadBlame();
    commitForLastBlame = commitHash;
  }

  // if the commitHistory was open, re-load it's contents
  if (!historyPanel.classList.contains("closed")) {
    console.log("fetching history since we switched to a new file and panel is open");
    loadHistory();
  }

  updateFileLinksButtons();
}

function expandLogLine(event) {
  var row = event.target.closest("tr");
  row.querySelector(".expanded-row-content").classList.toggle("hide-row");
}

// given a click on a "diff" button, get the commit of the "prev"
// commit, and diff against that.
// TODO: Disallow functionality on last diff button
function decideWhatToDiff(event) {
  var clickedCommitHash = event.target.dataset.commit;

  var clickedRow = event.target.closest("tr");

  // prev is technically next, since rows are rendered top down
  // so prev here stands for "previous" commit
  var prevRow = clickedRow.nextElementSibling;

  // not great, but eh
  var prevCommit = prevRow.querySelector(".diff-link").dataset.commit;

  return [prevCommit, clickedCommitHash];
}

function createOrShowLeftRightDiffButtonsToAllEntriesInHistoryPanel() {
  var diffButtons = elFactory(
    "div",
    { class: "diff-buttons-container" },
    elFactory("a", { href: "#0", class: "diff-left" }, "left"),
    elFactory("a", { href: "#0", class: "diff-right" }, "right")
  );

  var tableRows = document.getElementById("git-history-table").rows;
  for (var i = 1; i < tableRows.length; i++) {
    // start at 1 to skip thead
    var row = tableRows[i];

    // hide the fileviewer buttons
    row.querySelector(".fileview-buttons-container").classList.add("hidden");

    // if the buttons were already created, show them
    var dbc = row.querySelector(".diff-buttons-container");
    if (dbc) {
      dbc.classList.remove("hidden");
      continue;
    }

    // otherwise, create the buttons
    row
      .getElementsByClassName("actions-container")[0]
      .appendChild(diffButtons.cloneNode(true));
  }
}

function transformButtonsForDiff(event) {
  var actionsContainer = event.target.parentNode;
  console.log({ actionsContainer });

  // take the clicked "diff" button, hide it, then
  // 1. add "left" and "right" buttons. Highlight right
  // hide "diff" and "view" buttons

  // if the row we clicked "diff" in isn't the row that we were "view"ing
  // then we need to remove the "current-commit" class from that row,
  // and add it to the row thats clicked
  var currentCommitRow = document.querySelector("tr.current-commit");
  currentCommitRow.classList.remove("current-commit");
  currentCommitRow.querySelector(".view-link").dataset.toggled = "false";

  // now select the tr that contains the "diff" button that was pressed
  var clickedRow = event.target.closest("tr");
  clickedRow.classList.add("current-commit");

  createOrShowLeftRightDiffButtonsToAllEntriesInHistoryPanel();

  // highlight the "right" button of the clicked row
  clickedRow.querySelector("a.diff-right").setAttribute("data-toggled", "true");

  // highight the "left" button of the prevRow
  var prevRow = clickedRow.nextElementSibling;
  if (prevRow == null) {
    return;
  }
  prevRow.classList.add("secondary-row");
  prevRow.querySelector("a.diff-left").setAttribute("data-toggled", "true");
}
async function loadDiff(
  event,
  path,
  leftCommitHash,
  rightCommitHash,
  clickLocation
) {
  // if the diff we want, given left & right is already present, unhide it
  if (
    window.scriptData.diffLeftCommit == leftCommitHash &&
    window.scriptData.diffRightCommit == rightCommitHash
  ) {
    document.getElementsByClassName("file-content")[0].classList.add("hidden");
    document
      .getElementsByClassName("diff-content")[0]
      .classList.remove("hidden");
    document
      .getElementsByClassName("file-header-wrapper")[0]
      .classList.add("hidden");
    // TODO: add a <diff-header> we can hide/show
    updateOrCreateDiffHeaderWrapper(leftCommitHash, rightCommitHash);
    window.scriptData.currentMode = "split-diff-mode";
    return;
  }

  // load the diff
  var diffContent = await fetch(
    `/diff/${window.scriptData.repo}/${leftCommitHash}/${rightCommitHash}/${window.scriptData.filepath}`
  )
    .then((r) => r.text())
    .then((r) => r);
  // TODO: error handling

  var diffContentContainer = document.getElementsByClassName("diff-content")[0];
  var fileContentContainer = document.getElementsByClassName("file-content")[0];

  // insert the diffContent into .diff-content
  document.getElementsByClassName("diff-content")[0].innerHTML = diffContent;

  // hide the fileContent
  fileContentContainer.classList.add("hidden");

  // show the diffContent
  diffContentContainer.classList.remove("hidden");

  // hide file-header-wrapper
  document
    .getElementsByClassName("file-header-wrapper")[0]
    .classList.add("hidden");

  // TODO: add a <diff-header> we can hide/show
  updateOrCreateDiffHeaderWrapper(leftCommitHash, rightCommitHash);
  window.scriptData.currentMode = "split-diff-mode";
  window.scriptData.diffLeftCommit = leftCommitHash;
  window.scriptData.diffRightCommit = rightCommitHash;
}

// a small function to make adding html attributes easier
function elFactory(type, attributes, ...children) {
  var el = document.createElement(type);

  for (key in attributes) {
    if (key === "innerHTML") {
      el.innerHTML = attributes[key];
    } else {
      el.setAttribute(key, attributes[key]);
    }
  }

  children.forEach(function (child) {
    if (typeof child === "string") {
      el.appendChild(document.createTextNode(child));
    } else {
      el.appendChild(child);
    }
  });

  return el;
}
function updateOrCreateDiffHeaderWrapper(leftCommitHash, rightCommitHash) {
  var existingDiffHeader = document.getElementsByClassName(
    "diff-header-wrapper"
  );
  if (existingDiffHeader.length > 0) {
    document.getElementById("diff-left-commit-sha").innerText =
      leftCommitHash.slice(0, 8);
    document.getElementById("diff-right-commit-sha").innerText =
      rightCommitHash.slice(0, 8);
    return;
  }

  var diffHeader = elFactory(
    "div",
    { class: "diff-header-wrapper" },
    elFactory(
      "div",
      {},
      elFactory(
        "span",
        { id: "diff-left-commit-sha" },
        leftCommitHash.slice(0, 8)
      )
    ),
    elFactory(
      "div",
      { class: "right-commit" },
      elFactory(
        "span",
        { id: "diff-right-commit-sha" },
        rightCommitHash.slice(0, 8)
      ),
      elFactory(
        "div",
        { class: "diff-actions " },
        elFactory("button", { "data-action": "close-diff" }, "Close Diff")
      )
    )
  );

  document.getElementsByClassName("file-header")[0].appendChild(diffHeader);
}

function switchFromDiffViewToFileView(event) {
  // TODO: this is really not very clean, decide on some abstractions

  var diffContentContainer = document.getElementsByClassName("diff-content")[0];
  var fileContentContainer = document.getElementsByClassName("file-content")[0];

  diffContentContainer.classList.add("hidden");
  fileContentContainer.classList.remove("hidden");

  // load the file at diffRightCommit
  loadFileAtCommit(
    event,
    window.scriptData.repo,
    window.scriptData.filepath,
    window.scriptData.diffRightCommit,
    "close-diff-button"
  );

  // delete the diff-header-wrapper
  document.getElementsByClassName("diff-header-wrapper")[0].remove();

  // unhide the file-header-wrapper
  document
    .getElementsByClassName("file-header-wrapper")[0]
    .classList.remove("hidden");

  // loop through all the buttons, hide the diff-buttons-container, show the fileview-buttons-container
  // additionally, reset buttons and rows to remove toggled/selected highlighting

  var tableRows = document.getElementById("git-history-table").rows;
  for (var i = 1; i < tableRows.length; i++) {
    // start at 1 to skip thead
    var row = tableRows[i];
    var dbc = row.querySelector(".diff-buttons-container");

    // unselect the diff buttons, and unselect the rows
    if (
      row.classList.contains("current-commit") ||
      row.classList.contains("secondary-row")
    ) {
      row.classList.remove("current-commit");
      row.classList.remove("secondary-row");
      var toggledDiffButtons = dbc.querySelectorAll('[data-toggled="true"]');
      if (!toggledDiffButtons) {
        console.error("selected row should have a diff button toggled. " + row);
      } else {
        for (var j = 0; j < toggledDiffButtons.length; j++) {
          toggledDiffButtons[j].dataset.toggled = "false";
        }
      }
    }

    dbc.classList.add("hidden");
    row.querySelector(".fileview-buttons-container").classList.remove("hidden");

    // now, select the row with the commit we're loading in and its "view" button
    if (row.dataset.commit == window.scriptData.diffRightCommit) {
      console.log("index matches");
      row.classList.add("current-commit");
      row.querySelector("a.view-link").dataset.toggled = "true";
    }
  }
}

window.addEventListener("click", function (event) {
  console.log(event.target);
  console.log(event.target.pathname);
  console.log(event.target.innerHTML);

  // we may have to drag our variables up, or put this on the window
  // semantically, the searchBoxContainer contains the autocomplete menu, even though
  // they don't appear that way. Hence, checking whether the click is inside the searchBoxContainer
  // is enough to check both the input and the autocomplete menu
  console.log(searchBoxContainer.contains(event.target));
  if (autocompleteMenuOpen) {
    if (!searchBoxContainer.contains(event.target)) {
      console.log("menu is open, closing");
      autocompleteMenu.style.display = "none";
      autocompleteMenuOpen = "false";
    } else {
      // No SPA like behavior. Typicall search results come from HEAD
      // not fun to try to reconcile HEAD against the rev we have selected,
      // then update if necessary, etc... Just let the page reload
    }
  }

  if (repoAutocompleteMenuOpen) {
    if (!repoSearchBoxContainer.contains(event.target)) {
      console.log("menu is open, closing");
      repoAutocompleteMenu.style.display = "none";
      repoAutocompleteMenuOpen = false;
    } else if (event.target.hasAttribute("data-repo-name")) {
      console.log(event.target);
      console.log(event.target.dataset.repoName);
      console.log("button pressed. Asumming favorite repo click");
      toggleRepoFavorite(event);
    } else {
      console.log("click is inside of repo autocmplete");
    }
  }

  if (gitAutocompleteMenuOpen) {
    if (!gitSearchBoxContainer.contains(event.target)) {
      console.log("git meno open, closing");
      gitAutocompleteMenu.style.display = "none";
      gitAutocompleteMenuOpen = false;
    } else {
      console.log("click is inside of git autocomplete");
    }
  }

  if (fileLinksMenuOpen) {
    if (!fileLinksMenuContainer.contains(event.target)) {
      toggleFileLinksMenu();
    } else {
      // check if we clicked on one of the buttons. If we did, copy its text to
      // the clipboard, and then close the popup
      if (event.target.classList.contains("copy-button")) {
        var textToCopy = event.target.querySelector("span");
        if (!textToCopy) {
          console.error("copy button has no text, but should");
          return;
        }

        textToCopy = textToCopy.innerText;
        console.log({ textToCopy });
        // TODO: nicer ux around failures here
        this.navigator.clipboard.writeText(textToCopy);
    
        toggleFileLinksMenu();
      }
    }
  }

  if (
    event.target.tagName == "A" &&
    event.target.pathname.endsWith("/blah") &&
    event.target.innerHTML == "view"
  ) {
    console.log("viewing");
    event.preventDefault();
    loadFileAtCommit(
      event,
      window.scriptData.repo,
      window.scriptData.filepath,
      event.target.dataset.commit,
      "commit-history"
    );
  }

  if (
    event.target.tagName == "A" &&
    event.target.pathname.endsWith("/blah") &&
    event.target.innerHTML == "diff"
  ) {
    console.log("diffing");
    event.preventDefault();

    transformButtonsForDiff(event);
    const [leftCommit, rightCommit] = decideWhatToDiff(event);
    loadDiff(
      event,
      window.scriptData.filepath,
      leftCommit,
      rightCommit,
      "commit-history"
    );
  }

  if (event.target.tagName == "A" && event.target.href.endsWith("#0")) {
    console.log("in handler");
    event.preventDefault();
    var leftCommit = window.scriptData.diffLeftCommit;
    var rightCommit = window.scriptData.diffRightCommit;

    if (event.target.innerHTML == "left") {
      leftCommit = event.target.closest("tr").dataset.commit;
    } else if (event.target.innerHTML == "right") {
      rightCommit = event.target.closest("tr").dataset.commit;
    }

    loadDiff(
      event,
      window.scriptData.filepath,
      leftCommit,
      rightCommit,
      "commit-history"
    );

    window.scriptData.diffLeftCommit = leftCommit;
    window.scriptData.diffRightCommit = rightCommit;

    // left is always the secondary commit
    if (event.target.innerHTML == "left") {
      var prevRow = document.querySelector("tr.secondary-row");

      // unselect the prev row
      prevRow.classList.remove("secondary-row");

      // unselect the prev left button
      prevRow.querySelector("a[data-toggled='true']").dataset.toggled = "false";
      event.target.closest("tr").classList.add("secondary-row");
    }
    // right is always the primary commit
    else if (event.target.innerHTML == "right") {
      var prevRow = document.querySelector("tr.current-commit");
      prevRow.classList.remove("current-commit");
      prevRow.querySelector("a[data-toggled='true']").dataset.toggled = "false";
      event.target.closest("tr").classList.add("current-commit");
    }

    event.target.dataset.toggled = "true";
  }

  if (
    event.target.tagName == "A" &&
    event.target.href &&
    sideNav.contains(event.target)
  ) {
    console.log("side nav click");
    console.log(event.target.href);
    event.preventDefault();

    console.log(event.target.pathname);
    if (
      event.target.pathname.includes(
        `/experimental/${window.scriptData.repo}/tree/`
      )
    ) {
      console.log("tree click");
      toggleTree(event);
      return;
    }

    // we want to load the file at the ref indicated in the clicked url
    var commitHash = event.target.dataset.hash;
    var filePath = event.target.dataset.path;

    loadFileAtCommit(
      event,
      window.scriptData.repo,
      filePath,
      commitHash,
      "side-nav"
    );
  }

  console.log(event.target.classList);
  if (
    event.target.classList.contains("icon-toggle") &&
    event.target.classList.contains("log")
  ) {
    if (event.target.dataset.toggled == "true") {
      event.target.dataset.toggled = "false";
    } else {
      event.target.dataset.toggled = "true";
    }

    console.log("row expander clicked");
    expandLogLine(event);
  }

  if (event.target.dataset.action == "close-diff") {
    console.log("closing diff");
    switchFromDiffViewToFileView(event);
  }

  // if the click is on a git search tab
  if (
    gitSearchTabsContainer.contains(event.target) &&
    event.target.tagName == "LI"
  ) {
    switchGitSearchTab(event);
  }

  if (event.target.id === "git-log-show-more" || event.target.id === "git-log-show-all") {
    // loadHistory will decide what to do based on the data attributes of the
    // button we clicked
    loadHistory(event.target);
  }
});

// REMEMBER: When implementing left-right diff capability, the last commit
// Should not get a "diff" button on hover becase.. there's nothing to diff it to

// Potential nicety - when clicking on a commit hash in the fileviewer, we could
// probably display the diff right in the viewpane ...
// This comes with some problems though. We'd have to close the commits/history
// pane, and we'd have to unselect the current blob frome the treeviewer

// What follows is a stripped down verison of codesearch.js
// We don't:
//   track searches in history
//   init state from recent params
//   keep a recent history

// GLOBAL REFERENCES TO DOM ELEMENTS
var searchBoxContainer;
var searchBox;
var repoSearchBox;
var repoSearchBoxContainer;
var errorsBox;
var resultsContainer;
var repoSearchResultsContainer;
var caseSelect;
var regexToggle;
var autocompleteMenu;
var autocompleteMenuOpen = false;
var repoAutocompleteMenu;
var repoAutocompleteMenuOpen = false;
var gitAutocompleteMenu;
var gitAutocompleteMenuOpen = false;
var gitSearchBox;
var gitSearchBoxContainer;
var gitSearchTabsContainer;
var fileLinksMenu;
var fileLinksMenuContainer;
var fileLinksMenuOpen = false;
var lineNumberContainer;
var root;
var sideNav;
var historyPanel;  
var gitHistoryTable;
var verticalNavigationSplitter;
var horizontalLowerPaneSplitter;
var navigationPane;
var openGitMetaTab;

var searchOptions = {
  q: "",
  regex: false,
  context: true, // we don't have an option for disabling context. No one uses it
  case: "auto",
};


function toggleRepoSeachAutocompleteMenu() {
  if (repoAutocompleteMenuOpen) {
    repoAutocompleteMenu.style.display = "none";
    repoAutocompleteMenuOpen = false;
  } else {
    repoAutocompleteMenu.style.display = "initial";
    repoAutocompleteMenuOpen = true;
    loadRepoFavoritesFromLocalStorage(); // reload the favorites

    // for now, launch a new search every time to keep results up to date
    // later on we can add a "haveDoneInitialSearch" repo to keep track
    // of initialization
    searchRepos();
  }
}

function toggleGitAutocompleteMenu() {
  console.log("in here git");
  if (gitAutocompleteMenuOpen) {
    gitAutocompleteMenu.style.display = "none";
    gitAutocompleteMenuOpen = false;
  } else {
    gitAutocompleteMenu.style.display = "initial";
    gitAutocompleteMenuOpen = true;
  }
}

function toggleFileLinksMenu() {
  console.log("in here file links popup");
  if (fileLinksMenuOpen) {
    fileLinksMenu.style.display = "none";
    fileLinksMenuOpen = false;
  } else {
    fileLinksMenu.style.display = "initial";
    fileLinksMenuOpen = true;
  }
}

// we do a simple search
function searchRepos(inputEvnt) {
  var searchQuery = ""; // initialize to empty search so we can manually call

  if (inputEvnt) {
    searchQuery = inputEvnt.target.value;
  }
  // take whatevers in the repo search box, append repo:

  if (searchQuery.trim() === "") {
    // search for/show all repos by doing a complete search
    // ideally this would be updated so favorites aren't returned again
    searchQuery = ".*";
  }

  // we call the v1 api to get json results
  // we only ever look at the tree_results
  var query = "repo:" + searchQuery;
  var urlToFetch = `/api/v1/search/?q=${encodeURIComponent(query)}`;

  fetch(urlToFetch)
    .then(function (r) {
      if (!r.ok) {
        return Promise.reject(r.text());
      } else {
        return r.json();
      }
    })
    .then(function (j) {
      var treeResults = j.tree_results;

      // we want to display:
      // 1. the number of matching repos
      // 2. A list of the matching repos, as links
      var numResults = treeResults.length;
      var linksContainer = document.createElement("div");

      // TODO: cache this
      var favorites = getFavoritesFromLocalStorage();
      console.log({ favorites });

      for (var i = 0; i < treeResults.length; i++) {
        var tree = treeResults[i];

        console.log(favorites[tree.name]);
        // don't add this repo if its in favorites. It's already available
        if (favorites[tree.name]) {
          numResults--;
          continue;
        }

        var c = elFactory(
          "div",
          { class: "rlc", "data-repo-name": tree.name },
          elFactory("a", { href: `/experimental/${tree.name}/+/HEAD:` }, tree.name),
          elFactory(
            "button",
            { "data-repo-name": tree.name },
            elFactory("div", { innerHTML: favoritesStarSvg })
          )
        );

        linksContainer.appendChild(c);
      }

      var cnt = document.createTextNode(`${numResults} repo(s) found.`);

      var ct = elFactory("div", {}, cnt, linksContainer);

      // not sure if I can do this
      console.log({ linksContainer });
      repoSearchResultsContainer.innerHTML = "";
      repoSearchResultsContainer.appendChild(ct);

      // clear the old results container
    });
}

// search branches, tags, commits
function searchOpenGitMetaTab(inputEvnt) {
  var searchQuery = inputEvnt.target.value;
  searchQuery = searchQuery.trim();

  if (searchQuery === "") {
    // unhide everything
  }

  // otherwise, take the query, run it over every repo in repos,
  // for matching repos, data-shown="true", otherwise, "false"

  const re = new RegExp(searchQuery);
  var container = document.getElementById(`git-${openGitMetaTab.dataset.tab}-container`);
  if (!container) {
    console.error("container for git tab=" + openGitMetaTab + " could not be found. Exiting search");
    return
  }

  var childLen = container.children.length;

  for (var i = 0; i < childLen; i++) {
    var link = container.children[i];
    var name = link.dataset.name; // repo-name
    if (name.search(re) != -1) {
      link.dataset.shown = "true";
    } else {
      link.dataset.shown = "false";
    }
  }
}


function switchGitSearchTab(clickEvnt) {
  var tabToSwitchTo = clickEvnt.target.dataset.tab;

  if (tabToSwitchTo == openGitMetaTab.dataset.tab) return;

  // switches selected state of tabs
  openGitMetaTab.dataset.selected = "false";
  clickEvnt.target.dataset.selected = "true";

  // hide the old content
  document
    .getElementById(`git-${openGitMetaTab.dataset.tab}-container`)
    .classList.toggle("hidden");

  // show the new content
  document
    .getElementById(`git-${tabToSwitchTo}-container`)
    .classList.toggle("hidden");

  openGitMetaTab = clickEvnt.target;
}

// ----------- Start of line number and hash handling ------------ //
function setHash(hash) {
  if (history.replaceState) {
    history.replaceState(null, null, hash);
  } else {
    location.hash = hash;
  }
}

function expandRangeToElement(element) {
  var range = parseHashForLineRange(document.location.hash);
  if (range) {
    var elementLine = parseInt(element.getAttribute("id").replace("L", ""), 10);
    if (elementLine < range.start) {
      range.end = range.start;
      range.start = elementLine;
    } else {
      range.end = elementLine;
    }
    setHash("#L" + range.start + "-" + "L" + range.end);
  }
}

function parseHashForLineRange(hashString) {
  var parseMatch = hashString.match(/#L(\d+)(?:-L?(\d+))?/);

  if (parseMatch && parseMatch.length === 3) {
    // We have a match on the regex expression
    var startLine = parseInt(parseMatch[1], 10);
    var endLine = parseInt(parseMatch[2], 10);
    if (isNaN(endLine) || endLine < startLine) {
      endLine = startLine;
    }
    return {
      start: startLine,
      end: endLine,
    };
  }

  return null;
}

function getOffset(element) {
  if (!element.getClientRects().length) {
    return { top: 0, left: 0 };
  }

  var rect = element.getBoundingClientRect();
  var win = element.ownerDocument.defaultView;
  return {
    top: rect.top + win.pageYOffset,
    left: rect.left + win.pageXOffset,
  };
}

function scrollToRange(range, elementContainer) {
  // - If we have a single line, scroll the viewport so that the element is
  // at 1/3 of the viewport.
  // - If we have a range, try and center the range in the viewport
  // - If the range is to high to fit in the viewport, fallback to the single
  //   element scenario for the first line

  // TODO: almost perfect, sometimes leaves the line just a few lines out
  // of range
  var viewport = elementContainer;
  var viewportHeight = viewport.clientHeight;

  var scrollOffset = Math.floor(viewportHeight / 3.0);

  var firstLineElement = root.querySelector("#L" + range.start);
  if (!firstLineElement) {
    // We were given a scroll offset to a line number that doesn't exist in the page, bail
    return;
  }
  if (range.start != range.end) {
    // We have a range, try and center the entire range. If it's to high
    // for the viewport, fallback to revealing the first element.
    var lastLineElement = elementContainer.querySelector("#L" + range.end);
    var rangeHeight =
      getOffset(lastLineElement).top +
      lastLineElement.clientHeight -
      getOffset(firstLineElement).top;
    if (rangeHeight <= viewportHeight) {
      // Range fits in viewport, center it
      scrollOffset = 0.5 * (viewportHeight - rangeHeight);
    } else {
      scrollOffset = firstLineElement.clientHeight / 2; // Stick to (almost) the top of the viewport
    }
  }

  // viewport.scrollTop(firstLineElement.offset().top - scrollOffset);
  viewport.scrollTo({ top: getOffset(firstLineElement).top - scrollOffset });
}

function addHighlightClassesForRange(range, root) {
  for (var lineNumber = range.start; lineNumber <= range.end; lineNumber++) {
    root
      .querySelector("#LC" + lineNumber)
      .parentNode.classList.add("highlighted");
  }
}

function handleHashChange(shouldScrollElementIntoView) {
  // clear the current highlighted lines
  lineNumberContainer.querySelectorAll(".highlighted").forEach(function (elem) {
    elem.classList.remove("highlighted");
  });

  // highligh the current range from the hash, if any
  var range = parseHashForLineRange(document.location.hash);
  if (range) {
    addHighlightClassesForRange(range, lineNumberContainer);
    if (shouldScrollElementIntoView) {
      scrollToRange(range, root);
    }
  }

  // update the external-browse link
  document
    .getElementById("external-link")
    .setAttribute("href", getExternalLink(range));
  // TODO: update fragments
}

function initLineNumbers(lineNumberContainer) {
  // Initial range detection for when the page is loaded
  if (lineNumberContainer) {
    handleHashChange(true);
    // Allow shift clicking links to expand the highlight range
    // rather than adding an event handler to all links, we just
    // add a handler to the container, then check if the target
    // is a link
    lineNumberContainer.addEventListener("click", function (event) {
      if (!event.target.classList.contains("lno")) {
        return;
      }
      event.preventDefault();
      if (event.shiftKey) {
        expandRangeToElement(event.target);
      } else {
        setHash("#" + event.target.getAttribute("id"));
      }
      handleHashChange(false);
    });
    window.addEventListener("hashchange", function (event) {
      console.log("hashchange here");
      event.preventDefault();
      handleHashChange(true);
    });
  } else {
    // TODO
    document
      .getElementById("external-link")
      .setAttribute("href", getExternalLink(null));
  }
}

// ----------------------- End of Line numbers handling -------------- //

// ----------------------- Start of handling for favorite repos -------//

var favoritesStarSvg =
  '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="feather feather-star"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"></polygon></svg>';

// TODO: we're going to have to build a `-repo:favorite1|favorite2|` string so that
// repos aren't duplicated in search results
function loadRepoFavoritesFromLocalStorage() {
  var favorites = window.localStorage.getItem("repo-favorites");
  try {
    favorites = JSON.parse(favorites);
  } catch (err) {
    window.localStorage.setItem("repo-favorites", JSON.stringify({})); // store blank favorites
    return; // no more work to do
  }
  if (!favorites) {
    favorites = {};
  }

  if (favorites.length == 0) return; // nothing to do

  var favoritesContainer = document.getElementById("favorite-repos");

  // remove elements from favorites that already exist in the container
  for (var i = 0; i < favoritesContainer.children.length; i++) {
    var repoName =
      favoritesContainer.children[i].getAttribute("data-repo-name");
    if (favorites[repoName]) {
      delete favorites[repoName];
    }
  }

  var repos = Object.keys(favorites);

  console.log({ repos });
  for (var i = 0; i < repos.length; i++) {
    var repoName = repos[i];
    console.log({ repoName });
    var c = elFactory(
      "div",
      { class: "rlc", "data-repo-name": repoName },
      elFactory("a", { href: "/experimental/" + repoName + "/+/HEAD:" }, repoName),
      elFactory(
        "button",
        { class: "starred", "data-repo-name": repoName },
        elFactory("div", { innerHTML: favoritesStarSvg })
      )
    );

    favoritesContainer.appendChild(c);
  }
}

// add or remove a repo from favorites. We can determine whether a repo
// was or was not in favorites by the html state of clickEvnt. If someone
// attempts to break it, thats on them.
function toggleRepoFavorite(clickEvnt) {
  var repoName = clickEvnt.target.dataset.repoName; // we include the repoName as a data attribute on the star button
  var favorites = getFavoritesFromLocalStorage();

  if (!clickEvnt.target.classList.contains("starred")) {
    favorites[repoName] = 1;
    clickEvnt.target.classList.add("starred");
  } else {
    delete favorites[repoName];
    clickEvnt.target.classList.remove("starred");
    clickEvnt.target.closest("div").remove();
  }

  // store our updated favorites
  window.localStorage.setItem("repo-favorites", JSON.stringify(favorites));
}

function getFavoritesFromLocalStorage() {
  var favorites = window.localStorage.getItem("repo-favorites");
  try {
    favorites = JSON.parse(favorites);
  } catch (err) {
    console.error(
      "failed to parse repo-favorites value. Starting fresh. Err:" + err
    );
    favorites = {};
  }

  if (!favorites) {
    favorites = {};
  }

  return favorites;
}

// Unused at the moment, TODO: add in a settings page with this option
function toggleLineWrapping(event) {
  var root = document.documentElement;

  // line wrapping should effect both diff/fileview mods (probably)
  if (event.target.dataset.toggled == "true") {
    root.style.setProperty("--code-line-wrap", "pre");
    root.style.setProperty("--diff-table-layout", "auto");
  } else {
    root.style.setProperty("--code-line-wrap", "pre-wrap");
    root.style.setProperty("--diff-table-layout", "fixed");
  }
}

function getLineNumberFromRange(range) {
  if (range == null) {
    // Default to first line if no lines are selected.
    return "1";
  } else if (range.start == range.end) {
    return range.start.toString();
  } else {
    // We blindly assume that the external viewer supports linking to a
    // range of lines. Github doesn't support this, but highlights the
    // first line given, which is close enough.
    return range.start + "-L" + range.end;
  }
}

// given the current file, get a link to it
function getExternalLink(lineRange) {
  console.log("getExternalLink called");
  var lno = 0;
  if (lineRange) {
    lno = getLineNumberFromRange(lineRange);
  }

  var url = window.scriptData.repoConfig.metadata["url_pattern"];
  var githubUrl = window.scriptData.repoConfig.metadata["github"];
  if (!url) {
    console.error(
      `The index file you provided does not provide repositories[${window.scriptData.repo}].metadata.url_pattern. External links to file sources will not work. See the README for more information on file viewing.`
    );
    return;
  }

  var transformedFilePath = window.scriptData.filepath;
  // do I really need to do the root stuff I was doing before? TKTK
  if (transformedFilePath == "") {
    if (githubUrl != "") return githubUrl;

    // otherwise,
    // otherwise, take the url pattern, then split off everthing after '{name}/'
    // e.g "https://github.com/{name}/blob/{version}/{path}#L{lno}" -> "https://github.com/{name}/"
    var idx = url.indexOf("{name}/");
    var cutStr = url.substring(0, idx + 7); // idx + len of search term
    url = cutStr.replace("{name}", repoName);
    return url;
  }

  // If {path} already has a slash in front of it, trim extra leading
  // slashes from `pathInRepo` to avoid a double-slash in the URL.
  if (url.indexOf("/{path}") !== -1) {
    transformedFilePath = transformedFilePath.replace(/^\/+/, "");
  }

  var version = window.scriptData.repoRev;
  var dfc = new URLSearchParams(window.location.search).get("dfc");
  if (dfc) {
    // if we're viewing this file at this specific commit, use it. Otherwise, use repo branch/tag/commit
    version = dfc;
  }

  if (lno > 0) {
    url = url.replace("{lno}", lno);
  } else {
    url = url.replace("#L{lno}", "");
  }
  url = url.replace("{version}", version);
  url = url.replace("{name}", window.scriptData.repo);
  url = url.replace("{path}", transformedFilePath);
  console.log({ url });
  return url;
}

// ----------------------- End of handling for favorite repos ---------//
//

// container -> button -> span
function getSpanFromContainer(elemId) {
  var container = document.getElementById(elemId);
  if (!container) {
    return null;
  }

  return container.querySelector("button.copy-button span");
}

function updateFileLinksButtons() {
  var pathL = getSpanFromContainer("path-link-container");
  var headL = getSpanFromContainer("head-link-container");
  var commitL = getSpanFromContainer("commit-link-container");

  // these should never be undefined, so don't gaurd against it
  pathL.innerText = window.scriptData.filepath;
  headL.innerText = window.location.origin + "/experimental/" + window.scriptData.repo + "/+/" + window.scriptData.repoRev + ":" + window.window.scriptData.filepath; 
  commitL.innerText = window.location.origin + "/experimental/" + window.scriptData.repo + "/+/" + window.scriptData.repoRev + ":" + window.window.scriptData.filepath + "?" + "dfc=" + window.scriptData.fileCommitHash;

}

// initData gets
function initScriptData(initData) {
  console.log({ initData })
  // TODO: this is no bueno............. .......................................
  window.scriptData = {
    repo: initData.RepoName,
    repoCommit: initData.Commit,
    repoCommitHash: initData.CommitHash,
    repoRev: initData.RepoRev,
    headRev: initData.HeadRev,
    filepath: initData.FilePath,
    filename: initData.FileName,
    fileCommitHash: initData.CommitHash,
    currentMode: "fileviewer",
    diffLeftCommit: "",
    diffRightCommit: "",
    prevRepo: initData.RepoName,
    prevRepoCommit: initData.Commit,
    prevRepoCommitHash: initData.CommitHash,
    prevRepoRev: initData.RepoRev,
    prevFilepath: initData.FilePath,
    prevFilename: initData.FileName,
    prevFileCommitHash: initData.CommitHash,
    branches: initData.Branches,
    blameMeta: {
      // information stored by the blame viewer
      blameColsValid: [], // stores { startRow, endRowIdx } for every start of a blame hunk
    },
    repoConfig: initData.RepoConfig,
  };
}



// initData is passed to us via the go template setting script_data,
// then entry.js calling `init(window.script_data);`
function init(initData) {
  "use strict";

  console.log("init fileviewV2Init");
  console.log({ initData });
  initScriptData(initData);


  searchBoxContainer = document.getElementsByClassName("input-line")[0];
  searchBox = document.getElementById("searchbox");
  resultsContainer = document.querySelector("#resultarea > #results");
  repoSearchResultsContainer = document.querySelector(
    "#repos-resultarea > #repos-results"
  );
  caseSelect = document.getElementById("case-sensitivity-toggle");
  regexToggle = document.querySelector("button[id=toggle-regex]");
  errorsBox = document.getElementById("regex-error");
  autocompleteMenu = document.getElementById("autocomplete-menu");
  repoAutocompleteMenu = document.getElementById("repo-autocomplete-menu");
  repoSearchBox = document.getElementById("repo-search-input");
  repoSearchBoxContainer = document.getElementById("repo-selector-container");
  gitAutocompleteMenu = document.getElementById("git-autocomplete-menu");
  gitSearchBox = document.getElementById("git-search-input");
  gitSearchTabsContainer = document.getElementById("git-tabs");
  gitSearchBoxContainer = document.getElementById("git-selector-container");
  lineNumberContainer = document.querySelector(".file-content");
  root = document.querySelector(".file-content"); // TODO: this is identical to lineNumberContainer
  sideNav = document.getElementById("side-nav");
  historyPanel = document.getElementsByClassName("lower-detail-wrapper")[0];
  gitHistoryTable = document.getElementById("git-history-table");
  // get the open git search tab
  openGitMetaTab = document.querySelector("#git-tabs > li[data-selected='true']");
  
  // there's probably a better, more abstract way to do this, but for now this is how we roll
  // verticalNavigationSplitter = document.querySelector(".splitter.vertical");
  horizontalLowerPaneSplitter = document.querySelector("#file-pane-splitter");
  fileLinksMenuContainer = document.getElementById("file-links-container");
  fileLinksMenu = document.getElementById("file-links-popup");

  // attatch resize handlers to every splitter
  document.querySelectorAll('.splitter').forEach(function (el) {
    resizable(el);
  });

  initLineNumbers(lineNumberContainer);
  loadRepoFavoritesFromLocalStorage();

  var scopedSearchQuery = `repo:${window.scriptData.repo} `;
  searchBox.value = scopedSearchQuery;
  searchBox.addEventListener("focusin", function () {
    autocompleteMenu.style.display = "initial";
    autocompleteMenuOpen = true;
  });


  repoSearchBox.addEventListener("input", searchRepos);
  gitSearchBox.addEventListener("input", searchOpenGitMetaTab);

  document.getElementById("toggle-blame").addEventListener("click", toggleBlame);
  document.getElementById("toggle-history").addEventListener("click", toggleHistoryPanel);
  document.getElementById("toggle-file-links").addEventListener("click", toggleFileLinksMenu);
  updateFileLinksButtons();

  document.addEventListener("click", function (e) {
    var btn = e.target.closest("button");
    if (btn && btn.id == "showMoreFilematchesBtn") {
      toggleMoreFileMatches(e);
    } else if (btn && btn.classList.contains("file-extension")) {
      handleFileExtBtnClick(e);
    } else if (btn && btn.id == "repo-search-toggle") {
      toggleRepoSeachAutocompleteMenu();
    } else if (btn && btn.id == "git-search-toggle") {
      toggleGitAutocompleteMenu();
    } else if (
      e.target.tagName == "A" &&
      e.target.href != "" &&
      e.target.id == "next-page"
    ) {
      e.preventDefault();
      // just update the search input with the value of "q"
      var sp = new URLSearchParams(e.target.href);
      var newQ = sp.get("q");
      searchOptions.q = newQ;
      searchBox.value = newQ;
      updateSearchParamState();
    }
  });
}

// window.onload = function () {
//   init(window.scriptData);
// };
window.fileviewV2Init = init;

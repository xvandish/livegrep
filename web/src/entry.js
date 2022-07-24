pages = {
  codesearch: require('codesearch/codesearch_ui.js'),
  fileview: require('fileview/fileview.js')
};

(function(){
  if (window.page) {
    window.onload = function () {
      pages[window.page].init(window.scriptData);
    };
  }
})();

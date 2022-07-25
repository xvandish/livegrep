pages = {
  codesearch: require('./codesearch/codesearch.js'),
  fileview: require('./fileview/fileview.js')
};

(function(){
  if (window.page) {
    window.onload = function () {
      pages[window.page].init(window.scriptData);
    };
  }
})();

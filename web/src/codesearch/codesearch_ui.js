// var html = require('html');
// var Backbone = require('backbone');
// var Cookies = require('js-cookie');

// var Codesearch = require('codesearch/codesearch.js').Codesearch;
// var RepoSelector = require('codesearch/repo_selector.js');

var KeyCodes = {
  SLASH_OR_QUESTION_MARK: 191
};

function getSelectedText() {
  return window.getSelection ? window.getSelection().toString() : null;
}

function init(initData) {
  "use strict"
  console.log('initData: ', initData);

  var textInput = document.querySelector('#searchbox')
  // var caseInput = document.querySelector(
  console.log(textInput)
  // Get the search input and each of the search options
  //
  // Then on type, doSearch
  // then 
}

module.exports = {
  init: init
}

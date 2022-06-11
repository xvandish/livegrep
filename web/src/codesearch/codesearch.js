$ = require('jquery');
_ = require('underscore');

"use strict";

var liveDot;
var liveText;
var liveSubInfo; 

var Codesearch = function() {
  return {
    delegate: null,
    retry_time: 50,
    next_search: null,
    in_flight: null,
    connect: function(delegate) {
      if (delegate !== undefined)
        Codesearch.delegate = delegate;
      if (Codesearch.delegate.on_connect)
        setTimeout(Codesearch.delegate.on_connect, 0)
      liveDot = document.getElementById("live-status-dot");
      liveText = document.getElementById("live-status-text");
      liveSubInfo = document.getElementById("live-status-sub-info");
    },
    live_poll: function() {
      var url = "/api/v1/bkstatus/";
      var lastText = "";
      var lastDate;
      var lastDateNum;
      // TODO: add the selected bk from the select
      setInterval(function () {
        // Don't make network requests while tab is in background
        if (document.hidden) return;
        var resp = fetch(url)
         .then(function(r) {
          return r.text()
         })
         .then(function (text) {
           // closure land
           // if (text === lastText) return;

            var split = text.split(',');
            var status = split[0];
            if (status === "0") {
              // TODO: If the indexTime here is different than what we have in
              // state, then add a "reload" button

              liveDot.dataset.status = 'up';
              liveText.innerText = 'Connected';

              var indexText = 'Querying index created ' + split[1] + ' ago';
              if (indexText !== liveSubInfo.innerText) {
                liveSubInfo.innerText = indexText;
              }
            } else {
              var timeDown = split[1];

              // https://pkg.go.dev/google.golang.org/grpc@v1.45.0/codes#Code
              if (status === '14') {
                liveDot.dataset.status = 'reloading';
                liveText.innerText = 'Search Index Reloading (' + timeDown + ')';
                liveSubInfo.innerText = 'Queries will resolve once the index is reloaded';
                return;
              }
              // Some other unhandled grpc issue
              liveDot.dataset.status = 'down';
              liveText.innerText = 'Disconnected (' + timeDown + ')';
              liveSubInfo.innerText = 'Search backend down'
            }
         })
        .catch(function (error) {
          console.log(error);
          liveDot.dataset.status = 'down';
          liveText.innerText = 'Disconnected';
          liveSubInfo.innerText = 'No connection to webserver';
        });
      }, 1000);
    },
    new_search: function(opts) {
      Codesearch.next_search = opts;
      if (Codesearch.in_flight == null)
        Codesearch.dispatch()
    },
    dispatch: function() {
      if (!Codesearch.next_search)
        return;
      Codesearch.in_flight = Codesearch.next_search;
      Codesearch.next_search = null;

      var opts = Codesearch.in_flight;

      var url = "/api/v1/search/";
      if ('backend' in opts) {
        url = url + opts.backend;
      }
      var q = {
        q: opts.q,
        fold_case: opts.fold_case,
        regex: opts.regex
      };

      url = url + "?" + $.param(q);

      var xhr = $.getJSON(url);
      var start = new Date();
      xhr.done(function (data) {
        var elapsed = new Date() - start;
        data.results.forEach(function (r) {
          Codesearch.delegate.match(opts.id, r);
        });
        data.file_results.forEach(function (r) {
          Codesearch.delegate.file_match(opts.id, r);
        });
        data.tree_results.forEach(function (t) {
          Codesearch.delegate.tree_match(opts.id, t);
        });
        Codesearch.delegate.search_done(opts.id, elapsed, data.search_type, data.info.why);
      });
      xhr.fail(function(data) {
        window._err = data;
        if (data.status >= 400 && data.status < 500) {
          var err = JSON.parse(data.responseText);
          Codesearch.delegate.error(opts.id, err.error.message);
        } else {
          var message = "Cannot connect to server";
          if (data.status) {
            message = "Bad response " + data.status + " from server";
          }
          Codesearch.delegate.error(opts.id, message);
          console.log("server error", data.status, data.responseText);
        }
      });
      xhr.always(function() {
        Codesearch.in_flight = null;
        setTimeout(Codesearch.dispatch, 0);
      });
    }
  };
}();

module.exports = {
  Codesearch: Codesearch
}

var $ = require('jquery');
var _ = require('underscore');

function init() {
    $('#repos').selectpicker({
        actionsBox: true,
        selectedTextFormat: 'count > 4',
        countSelectedText: '({0} repositories)',
        noneSelectedText: '(all repositories)',
        liveSearch: true,
        width: '20em'
    });
    $(window).on('keyup', '.bootstrap-select .bs-searchbox input', function(event) {
        var keycode = (event.keyCode ? event.keyCode : event.which);
        if(keycode == '13'){
            $(this).val("");
            $("#repos").selectpicker('refresh');
        }
    });
    $(window).keyup(function (keyevent) {
        var code = (keyevent.keyCode ? keyevent.keyCode : keyevent.which);
        if (code == 9 && $('.bootstrap-select button:focus').length) {
            $("#repos").selectpicker('toggle');
            $('.bootstrap-select .bs-searchbox input').focus();
        }
    });

    // We don't want the "Select All" button as that results in an HTTP request
    // that's too large. Additionally, the UI doesn't properly
    // bubble up that error, leading to a confusing state. We could just check
    // if "all repos" are selected, then just remove them since "all repos" are
    // searched by default. But for now, we simply remove the "Select All"
    // button to prevent the scenario entirely. The bootstrap-select library
    // provides no native way to do this, so we do it the hard way
    var replaceSelectAllInterval = setInterval(function () {
        selectAllBtn = document.getElementsByClassName("actions-btn bs-select-all btn btn-default");
        if (selectAllBtn.length > 0) {
            selectAllBtn[0].remove();
            clearInterval(replaceSelectAllInterval);
        }
    }, 10);
}

function updateOptions(newOptions) {
    // Skip update if the options are the same, to avoid losing selected state.
    var currentOptions = [];
    $('#repos').find('option').each(function(){
        currentOptions.push($(this).attr('value'));
    });
    if (_.isEqual(currentOptions, newOptions)) {
        return;
    }

    $('#repos').empty();
    for (var i = 0; i < newOptions.length; i++) {
        var option = newOptions[i];
        $('#repos').append($('<option>').attr('value', option).text(option));
    }
    $('#repos').selectpicker('refresh');
}

function updateSelected(newSelected) {
    $('#repos').selectpicker('val', newSelected);
}

module.exports = {
    init: init,
    updateOptions: updateOptions,
    updateSelected: updateSelected
}

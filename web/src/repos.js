
function init() {
    var searchbox = document.getElementById("searchbox");
    var reposList = document.getElementById("repos-container").children;

    searchbox.addEventListener('keyup', function () {
        var searchTerm = this.value;
        if (searchTerm == "") {
            for (var i = 0; i < reposList.length; i++) {
                var repo = reposList[i];
                repo.dataset.shown = 'true';
            }
            
        }

        for (var i = 0; i < reposList.length; i++) {
            var repo = reposList[i];
            if (repo.innerText.includes(searchTerm)) {
                repo.dataset.shown = 'true';
            } else {
                repo.dataset.shown = 'false';
            }
        }
    });
}

module.exports = {
    init: init
}

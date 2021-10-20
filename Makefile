bb:
	@bazel build //...

xvandish-frontend:
	@bazel-bin/cmd/livegrep/livegrep_/livegrep -index-config ./repos/livegrep.json

xvandish-backend:
	@bazel-bin/src/tools/codesearch -grpc localhost:9999 -reload_rpc \
		-load-index xvandish.idx

xvandish-indexer:
	@bazel-bin/cmd/livegrep-github-reindex/livegrep-github-reindex_/livegrep-github-reindex -user=xvandish -forks=file -name=github.com/xvandish -out xvandish.idx

xfb:
	xvandish-backend
	xvandish-frontend

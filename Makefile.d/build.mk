# Binary + static-asset builds.
.PHONY: build build-tailwind

build:  ## Build cmd/regatta with engine-version + dirty flag pinned from git (#549). Replay-skew detection relies on this binary having a real SHA stamp.
	@SHA=$$(git rev-parse HEAD 2>/dev/null || echo unknown); \
	DIRTY=$$(test -n "$$(git status --porcelain 2>/dev/null)" && echo true || echo false); \
	go build -buildvcs=true \
		-ldflags "-X github.com/trilamsr/regatta/internal/program.compileEngineVersion=$$SHA -X github.com/trilamsr/regatta/internal/program.compileEngineDirty=$$DIRTY" \
		-o ./bin/regatta ./cmd/regatta

build-tailwind:  ## Re-compile internal/web/static/tailwind.min.css from CSS source + templates. Developer-machine only (npx tailwindcss@3.4.1). Commit the output; CI does NOT run this.
	npx tailwindcss@3.4.1 -c ./internal/web/tailwind.config.js \
		-i ./internal/web/css/input.css \
		-o ./internal/web/static/tailwind.min.css \
		--minify

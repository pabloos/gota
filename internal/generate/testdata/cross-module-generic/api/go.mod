module github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/api

go 1.23

require (
	github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/other v0.0.0
	github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/repository v0.0.0
)

replace github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/other => ../other

replace github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/repository => ../repository

module github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/api

go 1.23

require (
	github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/one v0.0.0
	github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/two v0.0.0
)

replace github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/one => ../one
replace github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/two => ../two

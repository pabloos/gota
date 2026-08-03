module github.com/pabloos/gota/internal/generate/testdata/cross-module/api

go 1.23

require github.com/pabloos/gota/internal/generate/testdata/cross-module/lib v0.0.0

replace github.com/pabloos/gota/internal/generate/testdata/cross-module/lib => ../lib

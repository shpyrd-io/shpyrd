//go:build foss

package main

import "github.com/shpyrd-io/shpyrd/pkg/ext"

// enterprise is empty in a foss build: no ee code is compiled in.
func enterprise() []ext.Extension { return nil }

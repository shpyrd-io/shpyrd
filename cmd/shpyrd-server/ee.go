//go:build !foss

package main

import (
	eeall "github.com/shpyrd-io/shpyrd/ee/all"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// enterprise is the ee extensions: always on in the server, the license
// decides what they do.
func enterprise() []ext.Extension { return eeall.Extensions() }

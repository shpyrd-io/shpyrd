//go:build !foss

package main

import (
	eeall "github.com/shpyrd-io/shpyrd/ee/all"
	"github.com/shpyrd-io/shpyrd/pkg/ext"
)

// enterprise is the ee extensions: their commands join shpyrd-ctl.
func enterprise() []ext.Extension { return eeall.Extensions() }

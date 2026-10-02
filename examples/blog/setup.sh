#!/bin/sh
# Run once, when the project is created (by hand, or by the Examples workflow):
# the volume the blog keeps its data on.
set -eu
shpyrd volumes create data --size 1Gi --project blog

#!/bin/sh
# Run once, when the project is created (by hand, or by the Examples workflow):
# the database and the cache the shop needs, attached to it.
set -eu
shpyrd pg create db --project shop --size shared-m --storage 2Gi
shpyrd redis create cache --project shop
shpyrd attach db --project shop
shpyrd attach cache --project shop

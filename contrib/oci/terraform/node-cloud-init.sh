#!/bin/bash
# First boot of every OKE node of this module (node pools' user_data).
#
# The boot volume is node_boot_volume_gb (100 GB by default), but the
# Oracle Linux image's root partition keeps its factory size (about 38 GB)
# until it is grown: without this every node used 38 GB of a 100 GB volume
# and hit kubelet's disk-pressure eviction with most of the disk unused
# (shpyrd-io/shpyrd#45). Grow it first, so the kubelet registers with the
# full filesystem, then run OKE's own bootstrap, which a custom user_data
# replaces and must therefore call itself.
#
# A failure to grow never keeps the node out of the cluster: the bootstrap
# runs whatever happens before it.

/usr/libexec/oci-growfs -y || echo "oci-growfs failed; the node joins with its factory-sized root filesystem" >&2

curl --fail -H "Authorization: Bearer Oracle" -L0 \
  http://169.254.169.254/opc/v2/instance/metadata/oke_init_script \
  | base64 --decode > /var/run/oke-init.sh
bash /var/run/oke-init.sh

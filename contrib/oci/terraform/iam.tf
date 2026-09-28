# Identity for the platform's DNS automation (RFC-0061): ExternalDNS and the
# cert-manager DNS-01 webhook manage records in the zone. Two ways to grant it:
#
#   dns_auth = "key"       a dedicated IAM user in group <name>-dns with an API
#                          signing key (any cluster type); the private key is
#                          written next to the Terraform state and passed to
#                          `shpyrd cluster init --dns oci --dns-key-file ...`
#   dns_auth = "workload"  OKE workload identity: the two service accounts of
#                          the cluster get the permission directly, no key at
#                          all (ENHANCED_CLUSTER only)
#
# IAM lives in the tenancy's home region, whatever region the cluster is in.

variable "dns_auth" {
  description = "How the cluster's DNS automation authenticates: none, key (an IAM user with an API key; any cluster type) or workload (OKE workload identity; enhanced clusters only). Needs dns_zone."
  type        = string
  default     = "none"

  validation {
    condition     = contains(["none", "key", "workload"], var.dns_auth)
    error_message = "none, key or workload."
  }
}

data "oci_identity_tenancy" "this" {
  tenancy_id = var.tenancy_ocid
}

data "oci_identity_regions" "all" {}

locals {
  home_region = [for r in data.oci_identity_regions.all.regions : r.name if r.key == data.oci_identity_tenancy.this.home_region_key][0]
  dns_key     = var.dns_zone != "" && var.dns_auth == "key"
  dns_wi      = var.dns_zone != "" && var.dns_auth == "workload"
  # "in tenancy" when the compartment is the root, else "in compartment id ...".
  policy_location = local.compartment_id == var.tenancy_ocid ? "in tenancy" : "in compartment id ${local.compartment_id}"
}

# --- API key -----------------------------------------------------------------

resource "oci_identity_group" "dns" {
  count    = local.dns_key ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-dns"
  description    = "DNS automation of the ${var.name} platform (ExternalDNS, cert-manager DNS-01)"
}

resource "oci_identity_user" "dns" {
  count    = local.dns_key ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-dns"
  description    = "DNS automation of the ${var.name} platform; API key only, no console access"
  email          = "${var.name}-dns@${var.dns_zone}"
}

resource "oci_identity_user_group_membership" "dns" {
  count    = local.dns_key ? 1 : 0
  provider = oci.home

  group_id = oci_identity_group.dns[0].id
  user_id  = oci_identity_user.dns[0].id
}

resource "tls_private_key" "dns" {
  count     = local.dns_key ? 1 : 0
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "oci_identity_api_key" "dns" {
  count    = local.dns_key ? 1 : 0
  provider = oci.home

  user_id   = oci_identity_user.dns[0].id
  key_value = tls_private_key.dns[0].public_key_pem
}

resource "local_sensitive_file" "dns_key" {
  count = local.dns_key ? 1 : 0

  filename        = "${coalesce(var.output_dir, path.root)}/${var.name}-dns.pem"
  content         = tls_private_key.dns[0].private_key_pem
  file_permission = "0600"
}

resource "oci_identity_policy" "dns_key" {
  count    = local.dns_key ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-dns"
  description    = "Let the ${var.name}-dns group manage DNS and (when autoscaling) the node pool"
  statements = [
    "Allow group ${oci_identity_group.dns[0].name} to manage dns ${local.policy_location}",
    # The cluster autoscaler uses the same API key (mounted as a Secret)
    # to resize the node pool on a BASIC_CLUSTER where instance principal
    # is not available. Only added when autoscaling is configured.
    (var.node_min_count > 0 || var.apps_max_count > 0) ? "Allow group ${oci_identity_group.dns[0].name} to manage cluster-node-pools ${local.policy_location}" : null,
    (var.node_min_count > 0 || var.apps_max_count > 0) ? "Allow group ${oci_identity_group.dns[0].name} to manage instance-family ${local.policy_location}" : null,
    (var.node_min_count > 0 || var.apps_max_count > 0) ? "Allow group ${oci_identity_group.dns[0].name} to use virtual-network-family ${local.policy_location}" : null,
  ]
}

# --- Cluster autoscaler (instance principal) ---------------------------------
# The cluster autoscaler pod runs on a worker node and uses the node's
# instance principal (no key files) to resize the node pool via the OCI API.
# The policy grants any instance in the cluster's compartment the minimum
# permissions needed: read the node pool, update its size.

resource "oci_identity_policy" "cluster_autoscaler" {
  count    = var.node_min_count > 0 || var.apps_max_count > 0 ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-cluster-autoscaler"
  description    = "Let ${var.name} worker nodes resize the cluster's node pool (cluster autoscaler)"
  statements = [
    "Allow dynamic-group id ${oci_identity_dynamic_group.workers[0].id} to manage cluster-node-pools ${local.policy_location}",
    "Allow dynamic-group id ${oci_identity_dynamic_group.workers[0].id} to manage cluster-family ${local.policy_location}",
    "Allow dynamic-group id ${oci_identity_dynamic_group.workers[0].id} to use virtual-network-family ${local.policy_location}",
    "Allow dynamic-group id ${oci_identity_dynamic_group.workers[0].id} to manage instance-family ${local.policy_location}",
  ]
}

resource "oci_identity_dynamic_group" "workers" {
  count    = var.node_min_count > 0 || var.apps_max_count > 0 ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-workers"
  description    = "Worker nodes of the ${var.name} cluster (for instance principal auth)"
  # Match all instances in the cluster's compartment that are OKE nodes.
  # Scope to compartment; if this compartment hosts multiple clusters,
  # tighten by adding the node pool OCID as a freeform tag on instances.
  matching_rule = "Any {instance.compartment.id = '${local.compartment_id}'}"
}

# --- Workload identity -------------------------------------------------------

resource "oci_identity_policy" "dns_workload" {
  count    = local.dns_wi ? 1 : 0
  provider = oci.home

  compartment_id = var.tenancy_ocid
  name           = "${var.name}-dns-workload"
  description    = "Let the ${var.name} cluster's DNS service accounts manage the platform's DNS zone (workload identity)"
  statements = [
    "Allow any-user to manage dns ${local.policy_location} where all {request.principal.type = 'workload', request.principal.cluster_id = '${oci_containerengine_cluster.this.id}', request.principal.namespace = 'shpyrd-system', request.principal.service_account = 'external-dns'}",
    "Allow any-user to manage dns ${local.policy_location} where all {request.principal.type = 'workload', request.principal.cluster_id = '${oci_containerengine_cluster.this.id}', request.principal.namespace = 'cert-manager', request.principal.service_account = 'dns01-oci-cert-manager-webhook-oci'}",
  ]

  lifecycle {
    precondition {
      condition     = var.cluster_type == "ENHANCED_CLUSTER"
      error_message = "Workload identity needs cluster_type = \"ENHANCED_CLUSTER\"; use dns_auth = \"key\" on a basic cluster."
    }
  }
}

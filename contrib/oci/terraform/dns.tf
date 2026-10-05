# A reserved public address for the external load balancer (ingress-nginx
# takes it through spec.loadBalancerIP; shpyrd: --set SHPYRD_LB_IP=<address>)
# and, optionally, the platform's public zone in OCI DNS with the wildcard
# record pointing at it. Delegate the zone from its parent with the NS records
# in the outputs; RFC-0061 later manages the records from inside the cluster.

resource "oci_core_public_ip" "lb" {
  count = var.reserved_public_ip ? 1 : 0

  compartment_id = local.compartment_id
  display_name   = "${var.name}-lb"
  lifetime       = "RESERVED"

  lifecycle {
    # The load balancer holds the address; Terraform must not fight over it.
    ignore_changes = [private_ip_id]
  }
}

resource "oci_dns_zone" "platform" {
  count = var.dns_zone != "" ? 1 : 0

  compartment_id = local.compartment_id
  name           = var.dns_zone
  zone_type      = "PRIMARY"
  scope          = "GLOBAL"
}

resource "oci_dns_rrset" "wildcard" {
  count = var.dns_zone != "" && var.reserved_public_ip ? 1 : 0

  zone_name_or_id = oci_dns_zone.platform[0].id
  domain          = "*.${var.dns_zone}"
  rtype           = "A"
  compartment_id  = local.compartment_id

  items {
    domain = "*.${var.dns_zone}"
    rtype  = "A"
    rdata  = oci_core_public_ip.lb[0].ip_address
    ttl    = 300
  }
}

# The workspaces zone (cloud layer): one zone per production, with the
# development cluster's zone delegated from it (dev.<zone>) so every
# environment answers for its own names and none can touch another's.
resource "oci_dns_zone" "workspaces" {
  count = var.workspaces_zone != "" ? 1 : 0

  compartment_id = local.compartment_id
  name           = var.workspaces_zone
  zone_type      = "PRIMARY"
  scope          = "GLOBAL"

  lifecycle {
    precondition {
      # ExternalDNS, the DNS user and the DNS-01 issuer only exist with a
      # platform zone; a workspaces zone alone would be records nobody writes.
      condition     = var.dns_zone != ""
      error_message = "workspaces_zone needs dns_zone: the same DNS automation serves both."
    }
  }
}

# The apps' zone (cloud layer, RFC-0033 names): every app of every
# workspace at <workspace>-<app>.<zone>, one wildcard for all of them.
resource "oci_dns_zone" "apps" {
  count = var.apps_zone != "" ? 1 : 0

  compartment_id = local.compartment_id
  name           = var.apps_zone
  zone_type      = "PRIMARY"
  scope          = "GLOBAL"

  lifecycle {
    precondition {
      condition     = var.workspaces_zone != "" && var.apps_zone != var.workspaces_zone
      error_message = "apps_zone needs a workspaces_zone of its own: workspaces answer there, their apps here."
    }
  }
}

resource "oci_dns_rrset" "workspaces_delegation" {
  for_each = var.workspaces_zone != "" ? var.workspaces_delegations : {}

  zone_name_or_id = oci_dns_zone.workspaces[0].id
  domain          = "${each.key}.${var.workspaces_zone}"
  rtype           = "NS"
  compartment_id  = local.compartment_id

  dynamic "items" {
    for_each = each.value
    content {
      domain = "${each.key}.${var.workspaces_zone}"
      rtype  = "NS"
      rdata  = items.value
      ttl    = 3600
    }
  }
}

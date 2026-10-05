# Identity and placement

variable "tenancy_ocid" {
  description = "Tenancy OCID (oci iam availability-domain list shows it as compartment-id)."
  type        = string
}

variable "compartment_ocid" {
  description = "Compartment for every resource; the tenancy root when empty (fine for a development cluster; production wants its own compartment)."
  type        = string
  default     = ""
}

variable "region" {
  description = "OCI region identifier, for example sa-saopaulo-1."
  type        = string
}

variable "config_file_profile" {
  description = "Profile in ~/.oci/config used to authenticate."
  type        = string
  default     = "DEFAULT"
}

variable "name" {
  description = "Name of the platform; prefixes every resource and names the cluster."
  type        = string
  default     = "shpyrd-dev"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}$", var.name))
    error_message = "Lowercase letters, digits and dashes, starting with a letter."
  }
}

# Cluster

variable "kubernetes_version" {
  description = "OKE Kubernetes version (oci ce cluster-options get --cluster-option-id all lists them)."
  type        = string
  default     = "v1.36.1"
}

variable "cluster_type" {
  description = "BASIC_CLUSTER is free; ENHANCED_CLUSTER adds workload identity, add-on management and an SLA for a per-cluster fee. Basic upgrades to enhanced in place."
  type        = string
  default     = "BASIC_CLUSTER"

  validation {
    condition     = contains(["BASIC_CLUSTER", "ENHANCED_CLUSTER"], var.cluster_type)
    error_message = "BASIC_CLUSTER or ENHANCED_CLUSTER."
  }
}

variable "services_cidr" {
  description = "Kubernetes Service (ClusterIP) range."
  type        = string
  default     = "10.96.0.0/16"
}

# Workers

variable "node_shape" {
  description = "Worker shape. VM.Standard.A1.Flex (Ampere, Always Free) when the region has capacity; VM.Standard.E5.Flex (AMD) otherwise."
  type        = string
  default     = "VM.Standard.E5.Flex"
}

variable "node_ocpus" {
  type    = number
  default = 2
}

variable "node_memory_gb" {
  type    = number
  default = 12
}

variable "node_count" {
  type    = number
  default = 2
}

# Node pools (RFC-0077). The "workers" pool is the platform pool: fixed at
# node_count, it carries the platform's components and every stateful
# resource (databases, stores). The "apps" pool carries application
# processes, builds and one-off runs only, and is what the cluster
# autoscaler scales. apps_max_count = 0 means no apps pool: a single pool
# as before, and node_min_count/node_max_count bound the autoscaler on it.

variable "node_min_count" {
  description = "Single-pool mode only (apps_max_count = 0): minimum worker nodes for the cluster autoscaler; 0 disables autoscaling."
  type        = number
  default     = 0
}

variable "node_max_count" {
  description = "Single-pool mode only (apps_max_count = 0): maximum worker nodes for the cluster autoscaler."
  type        = number
  default     = 5
}

variable "apps_min_count" {
  description = "Minimum nodes of the apps pool. 0 lets the pool empty when every app sleeps (the first request then waits for a node, ~2 min); 1 keeps wakes at seconds."
  type        = number
  default     = 1
}

variable "apps_max_count" {
  description = "Maximum nodes of the apps pool; 0 means no apps pool (single-pool cluster)."
  type        = number
  default     = 0
}

variable "apps_node_ocpus" {
  description = "OCPUs of an apps-pool node. Smaller than the platform node so the autoscaler scales in finer steps."
  type        = number
  default     = 1
}

variable "apps_node_memory_gb" {
  type    = number
  default = 8
}

# Data pool (RFC-0077 Q1): customer databases and Redis, separate from the
# platform pool. Same autoscaler mechanism as the apps pool. Enabled when
# data_max_count > 0.

variable "data_min_count" {
  description = "Minimum nodes of the data pool. 1 keeps wakes at seconds; 0 lets the pool empty (node boot adds ~2 min to a database cold wake)."
  type        = number
  default     = 1
}

variable "data_max_count" {
  description = "Maximum nodes of the data pool; 0 means no data pool (databases share the platform pool)."
  type        = number
  default     = 0
}

variable "data_node_ocpus" {
  description = "OCPUs of a data-pool node."
  type        = number
  default     = 1
}

variable "data_node_memory_gb" {
  description = "Memory of a data-pool node (GB). Databases are memory-bound; 12–16 GB per OCPU is typical."
  type        = number
  default     = 12
}

variable "node_boot_volume_gb" {
  type    = number
  default = 100
}

variable "max_pods_per_node" {
  description = "VCN-native pod networking: pods per node is bounded by the shape's VNICs (31 on a 2-OCPU shape)."
  type        = number
  default     = 31
}

variable "ssh_public_key_path" {
  description = "Public key installed on the workers (ssh through the Bastion service)."
  type        = string
  default     = "~/.ssh/id_ed25519.pub"
}

# Access

variable "admin_cidrs" {
  description = "Who may open Bastion sessions to the Kubernetes API. Empty: this machine's public address."
  type        = list(string)
  default     = []
}

# Network layout (RFC-0035). The pod subnet is a /22 on a multiple of four.

variable "vcn_cidr" {
  type    = string
  default = "10.0.0.0/16"
}

variable "subnet_cidrs" {
  description = "Subnets inside vcn_cidr."
  type = object({
    api        = string                           # Kubernetes API endpoint (private)
    workers    = string                           # worker nodes (private)
    pods       = string                           # pods, VCN-native networking (private)
    lb_public  = string                           # internet-facing load balancers
    lb_private = string                           # internal load balancers (RFC-0036)
    bastion    = string                           # OCI Bastion service
    vpn        = optional(string, "10.0.30.0/24") # the WireGuard instance (public)
  })
  default = {
    api        = "10.0.0.0/24"
    workers    = "10.0.1.0/24"
    pods       = "10.0.4.0/22"
    lb_public  = "10.0.10.0/24"
    lb_private = "10.0.11.0/24"
    bastion    = "10.0.20.0/24"
    vpn        = "10.0.30.0/24"
  }
}

# Public address and DNS

variable "reserved_public_ip" {
  description = "Reserve a public address for the external load balancer so it survives cluster rebuilds (pass it to shpyrd as SHPYRD_LB_IP)."
  type        = bool
  default     = true
}

variable "dns_zone" {
  description = "Create this public zone in OCI DNS with a wildcard record for the platform (for example oci.example.com; delegate it from the parent zone with the NS records in the outputs). Empty: no zone."
  type        = string
  default     = ""
}

variable "workspaces_zone" {
  description = "Public zone in OCI DNS for tenant workspaces (cloud layer): <workspace>.<zone> and *.<workspace>.<zone>, records written by the cluster's ExternalDNS, certificates by DNS-01 (the installer derives the issuer). Written to the vars file as SHPYRD_WORKSPACES_DOMAIN. Delegate it at the registrar with workspaces_zone_nameservers. Empty: workspaces live under dns_zone. OCI DNS keeps a zone and its subzones in one tenancy: a second cluster in another tenancy cannot have dev.<zone>; give it its own registered domain or leave this empty."
  type        = string
  default     = ""
}

variable "apps_zone" {
  description = "Public zone in OCI DNS for the apps of tenant workspaces (cloud layer, RFC-0033 names): <workspace>-<app>.<zone>, one wildcard record (written by the cluster's ExternalDNS from the apps' shared front door) and one wildcard certificate (DNS-01) for every app. Written to the vars file as SHPYRD_APPS_DOMAIN; needs workspaces_zone, where the workspaces then answer as <workspace>.<workspaces_zone>. Delegate it at the registrar with apps_zone_nameservers. Empty: apps live one label under their workspace's address."
  type        = string
  default     = ""
}

variable "workspaces_delegations" {
  description = "Subzones of workspaces_zone served by other nameservers, as label => nameservers: { eu = [\"ns1.example.net.\", ...] } delegates eu.<workspaces_zone>. Within OCI DNS the subzone must be in this tenancy (see workspaces_zone)."
  type        = map(list(string))
  default     = {}
}

# Platform backups (RFC-0037)

variable "output_dir" {
  description = "Directory where Terraform writes generated files: the vars file, the DNS key, the VPN profile. Defaults to path.root of the calling module."
  type        = string
  default     = ""
}

variable "backup_bucket" {
  description = "Object Storage bucket for the platform's encrypted backups, created by contrib/oci/terraform/backups so it outlives the cluster; empty means no backups. Its credentials file goes to `shpyrd cluster init --backup-credentials-file`."
  type        = string
  default     = ""
}

variable "extra_vars" {
  description = "More SHPYRD_* values for the vars file (the cloud layer's workspaces domain, the server image, a certificate issuer); kept here so a terraform apply does not drop them."
  type        = map(string)
  default     = {}
}

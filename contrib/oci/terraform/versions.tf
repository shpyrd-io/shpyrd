terraform {
  # Works with Terraform 1.5+ (the last MPL release) and OpenTofu.
  required_version = ">= 1.5"

  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 9.0"
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.4"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
    wireguard = {
      source  = "OJFord/wireguard"
      version = "~> 0.4"
    }
  }
}

# Providers are configured by the calling root module, not here.
# The module requires two oci provider configurations:
#   provider "oci" {}           — the cluster's region (default alias)
#   provider "oci" { alias = "home" } — the tenancy's home region (IAM)
# Pass them with:
#   providers = { oci = oci, oci.home = oci.home }

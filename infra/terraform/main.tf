terraform {
  required_version = ">= 1.6.0"

  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.34"
    }
  }
}

provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kubeconfig_context
}

locals {
  base_files = [
    for f in fileset("${path.module}/../../k8s/base", "*.yaml") : f
    if f != "kustomization.yaml"
  ]

  manifest_docs = flatten([
    for f in local.base_files : [
      for d in split("\n---\n", file("${path.module}/../../k8s/base/${f}")) : trimspace(d)
      if trimspace(d) != ""
    ]
  ])

  manifests = {
    for i, doc in local.manifest_docs :
    tostring(i) => yamldecode(doc)
  }
}

resource "kubernetes_manifest" "smart_planner" {
  for_each = local.manifests
  manifest = each.value
}

variable "kubeconfig_path" {
  type        = string
  description = "Path to kubeconfig used by Terraform"
  default     = "~/.kube/config"
}

variable "kubeconfig_context" {
  type        = string
  description = "Optional kubeconfig context"
  default     = null
}

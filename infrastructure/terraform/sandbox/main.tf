provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "SyncCam-AI"
      Environment = "sandbox"
      ManagedBy   = "terraform"
    }
  }
}

# Phase 0 intentionally creates no resources. Account, budget, residency,
# encryption, and network decisions must be approved before provisioning.

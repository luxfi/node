packer {
  required_plugins {
    amazon = {
      source  = "github.com/hashicorp/amazon"
      version = "~> 1"
    }
    googlecompute = {
      source  = "github.com/hashicorp/googlecompute"
      version = "~> 1"
    }
    azure = {
      source  = "github.com/hashicorp/azure"
      version = "~> 2"
    }
    ansible = {
      source  = "github.com/hashicorp/ansible"
      version = "~> 1"
    }
  }
}

# ============================================================================
# Variables
# ============================================================================

variable "tag" {
  type        = string
  description = "Git tag/version to build"
  default     = env("TAG")
}

variable "cloud" {
  type        = string
  description = "Target cloud: aws, gcp, azure, or all"
  default     = env("CLOUD")
}

variable "skip_create_image" {
  type    = bool
  default = false
}

# AWS Variables
variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "aws_instance_type" {
  type    = string
  default = "c5.large"
}

# GCP Variables
variable "gcp_project_id" {
  type    = string
  default = env("GCP_PROJECT_ID")
}

variable "gcp_zone" {
  type    = string
  default = "us-central1-a"
}

variable "gcp_machine_type" {
  type    = string
  default = "n2-standard-2"
}

# Azure Variables
variable "azure_subscription_id" {
  type    = string
  default = env("AZURE_SUBSCRIPTION_ID")
}

variable "azure_resource_group" {
  type    = string
  default = env("AZURE_RESOURCE_GROUP")
}

variable "azure_location" {
  type    = string
  default = "eastus"
}

variable "azure_vm_size" {
  type    = string
  default = "Standard_D2s_v3"
}

# ============================================================================
# Locals
# ============================================================================

locals {
  timestamp  = regex_replace(timestamp(), "[- TZ:]", "")
  clean_name = regex_replace(var.tag, "[^a-zA-Z0-9-]", "-")
  image_name = "luxd-ubuntu-22-04-${local.clean_name}-${local.timestamp}"

  # Build targets based on cloud variable
  build_aws   = var.cloud == "aws" || var.cloud == "all"
  build_gcp   = var.cloud == "gcp" || var.cloud == "all"
  build_azure = var.cloud == "azure" || var.cloud == "all"
}

# ============================================================================
# Data Sources
# ============================================================================

# AWS - Find latest Ubuntu 22.04 AMI
data "amazon-ami" "ubuntu" {
  filters = {
    architecture        = "x86_64"
    name                = "ubuntu/images/*ubuntu-jammy-22.04-*-server-*"
    root-device-type    = "ebs"
    virtualization-type = "hvm"
  }
  most_recent = true
  owners      = ["099720109477"]  # Canonical
  region      = var.aws_region
}

# ============================================================================
# Sources
# ============================================================================

# AWS EC2 AMI
source "amazon-ebs" "luxd" {
  ami_name        = local.image_name
  ami_description = "Lux Network Node ${var.tag} - Ubuntu 22.04"
  ami_groups      = ["all"]  # Make public
  instance_type   = var.aws_instance_type
  region          = var.aws_region
  source_ami      = data.amazon-ami.ubuntu.id
  ssh_username    = "ubuntu"
  skip_create_ami = var.skip_create_image

  ami_regions = [
    "us-east-1",
    "us-west-2",
    "eu-west-1",
    "eu-central-1",
    "ap-southeast-1",
    "ap-northeast-1"
  ]

  tags = {
    Name        = local.image_name
    Version     = var.tag
    OS          = "Ubuntu 22.04"
    Application = "luxd"
    ManagedBy   = "Packer"
  }

  run_tags = {
    Name = "packer-builder-luxd"
  }
}

# GCP Compute Image
source "googlecompute" "luxd" {
  project_id          = var.gcp_project_id
  zone                = var.gcp_zone
  machine_type        = var.gcp_machine_type
  source_image_family = "ubuntu-2204-lts"
  ssh_username        = "ubuntu"
  image_name          = local.image_name
  image_description   = "Lux Network Node ${var.tag} - Ubuntu 22.04"
  image_family        = "luxd"
  skip_create_image   = var.skip_create_image

  image_labels = {
    version     = replace(lower(var.tag), ".", "-")
    os          = "ubuntu-22-04"
    application = "luxd"
    managed-by  = "packer"
  }

  labels = {
    name = "packer-builder-luxd"
  }
}

# Azure Managed Image
source "azure-arm" "luxd" {
  subscription_id                   = var.azure_subscription_id
  managed_image_resource_group_name = var.azure_resource_group
  managed_image_name                = local.image_name

  os_type         = "Linux"
  image_publisher = "Canonical"
  image_offer     = "0001-com-ubuntu-server-jammy"
  image_sku       = "22_04-lts-gen2"
  location        = var.azure_location
  vm_size         = var.azure_vm_size

  ssh_username = "ubuntu"

  skip_create_image = var.skip_create_image

  azure_tags = {
    Name        = local.image_name
    Version     = var.tag
    OS          = "Ubuntu 22.04"
    Application = "luxd"
    ManagedBy   = "Packer"
  }
}

# ============================================================================
# Build
# ============================================================================

build {
  name = "luxd"

  # Conditionally include sources based on target cloud
  dynamic "source" {
    for_each = local.build_aws ? ["amazon-ebs.luxd"] : []
    labels   = ["amazon-ebs.luxd"]
    content {}
  }

  dynamic "source" {
    for_each = local.build_gcp ? ["googlecompute.luxd"] : []
    labels   = ["googlecompute.luxd"]
    content {}
  }

  dynamic "source" {
    for_each = local.build_azure ? ["azure-arm.luxd"] : []
    labels   = ["azure-arm.luxd"]
    content {}
  }

  # Wait for cloud-init to complete
  provisioner "shell" {
    inline = [
      "echo 'Waiting for cloud-init to complete...'",
      "while [ ! -f /var/lib/cloud/instance/boot-finished ]; do sleep 1; done",
      "echo 'Cloud-init complete!'",
      "echo 'Waiting for apt locks...'",
      "while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 1; done",
      "while fuser /var/lib/apt/lists/lock >/dev/null 2>&1; do sleep 1; done",
      "echo 'APT ready!'"
    ]
  }

  # Install base dependencies
  provisioner "shell" {
    inline = [
      "sudo apt-get update",
      "sudo apt-get install -y software-properties-common curl wget git jq",
      "sudo add-apt-repository -y ppa:longsleep/golang-backports",
      "sudo apt-get update",
      "sudo apt-get install -y golang-go"
    ]
  }

  # Use Ansible for main provisioning
  provisioner "ansible" {
    playbook_file   = ".github/packer/create_public_ami.yml"
    roles_path      = ".github/packer/roles/"
    use_proxy       = false
    extra_arguments = [
      "-e", "component=public-ami",
      "-e", "build=packer",
      "-e", "os_release=jammy",
      "-e", "tag=${var.tag}"
    ]
  }

  # Cleanup
  provisioner "shell" {
    execute_command = "sudo bash -x {{ .Path }}"
    inline = [
      "apt-get clean",
      "rm -rf /var/lib/apt/lists/*",
      "rm -rf /tmp/*",
      "rm -rf /var/tmp/*",
      "truncate -s 0 /var/log/*.log",
      "history -c"
    ]
  }

  # Azure specific: Deprovision
  provisioner "shell" {
    only = ["azure-arm.luxd"]
    execute_command = "chmod +x {{ .Path }}; {{ .Vars }} sudo -E sh '{{ .Path }}'"
    inline = [
      "/usr/sbin/waagent -force -deprovision+user && export HISTSIZE=0 && sync"
    ]
    inline_shebang = "/bin/sh -x"
  }

  post-processor "manifest" {
    output     = "packer-manifest.json"
    strip_path = true
  }
}

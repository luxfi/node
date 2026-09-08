# Multi-Cloud Image Build Setup

This document explains how to set up the CI/CD pipeline for building Lux Network node images on AWS, GCP, and Azure.

## Overview

The workflows automatically build machine images when:
- A new version tag is pushed (`v*`)
- A GitHub release is published
- Manually triggered via workflow dispatch

## Required GitHub Secrets

### AWS Secrets

| Secret | Description | How to Obtain |
|--------|-------------|---------------|
| `AWS_AMI_ROLE_ARN` | IAM role ARN for OIDC authentication | See AWS Setup below |

### GCP Secrets

| Secret | Description | How to Obtain |
|--------|-------------|---------------|
| `GCP_PROJECT_ID` | Google Cloud project ID | GCP Console |
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Workload Identity Federation provider | See GCP Setup below |
| `GCP_SERVICE_ACCOUNT` | Service account email | See GCP Setup below |

### Azure Secrets

| Secret | Description | How to Obtain |
|--------|-------------|---------------|
| `AZURE_CLIENT_ID` | Azure AD application ID | See Azure Setup below |
| `AZURE_CLIENT_SECRET` | Azure AD application secret | See Azure Setup below |
| `AZURE_TENANT_ID` | Azure AD tenant ID | Azure Portal |
| `AZURE_SUBSCRIPTION_ID` | Azure subscription ID | Azure Portal |
| `AZURE_RESOURCE_GROUP` | Resource group for images | Create in Azure |

---

## AWS Setup

### 1. Create OIDC Identity Provider

```bash
# Create the OIDC provider for GitHub Actions
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

### 2. Create IAM Role

```bash
# Create trust policy file
cat > trust-policy.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::YOUR_ACCOUNT_ID:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "token.actions.githubusercontent.com:sub": "repo:luxfi/node:*"
        }
      }
    }
  ]
}
EOF

# Create the role
aws iam create-role \
  --role-name GitHubActionsLuxAMI \
  --assume-role-policy-document file://trust-policy.json
```

### 3. Attach Permissions

```bash
# Create policy for Packer AMI building
cat > packer-policy.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:AttachVolume",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:CopyImage",
        "ec2:CreateImage",
        "ec2:CreateKeypair",
        "ec2:CreateSecurityGroup",
        "ec2:CreateSnapshot",
        "ec2:CreateTags",
        "ec2:CreateVolume",
        "ec2:DeleteKeyPair",
        "ec2:DeleteSecurityGroup",
        "ec2:DeleteSnapshot",
        "ec2:DeleteVolume",
        "ec2:DeregisterImage",
        "ec2:DescribeImageAttribute",
        "ec2:DescribeImages",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus",
        "ec2:DescribeRegions",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeSnapshots",
        "ec2:DescribeSubnets",
        "ec2:DescribeTags",
        "ec2:DescribeVolumes",
        "ec2:DetachVolume",
        "ec2:GetPasswordData",
        "ec2:ModifyImageAttribute",
        "ec2:ModifyInstanceAttribute",
        "ec2:ModifySnapshotAttribute",
        "ec2:RegisterImage",
        "ec2:RunInstances",
        "ec2:StopInstances",
        "ec2:TerminateInstances"
      ],
      "Resource": "*"
    }
  ]
}
EOF

aws iam put-role-policy \
  --role-name GitHubActionsLuxAMI \
  --policy-name PackerAMIBuilder \
  --policy-document file://packer-policy.json
```

### 4. Add Secret to GitHub

```bash
# Get the role ARN
aws iam get-role --role-name GitHubActionsLuxAMI --query 'Role.Arn' --output text

# Add to GitHub secrets:
# AWS_AMI_ROLE_ARN = arn:aws:iam::YOUR_ACCOUNT_ID:role/GitHubActionsLuxAMI
```

---

## GCP Setup

### 1. Create Service Account

```bash
PROJECT_ID="your-gcp-project"

# Create service account
gcloud iam service-accounts create github-packer \
  --project=$PROJECT_ID \
  --display-name="GitHub Actions Packer"

# Grant permissions
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:github-packer@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.instanceAdmin.v1"

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:github-packer@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/compute.imageAdmin"

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:github-packer@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountUser"
```

### 2. Setup Workload Identity Federation

```bash
# Create workload identity pool
gcloud iam workload-identity-pools create github-pool \
  --project=$PROJECT_ID \
  --location="global" \
  --display-name="GitHub Actions Pool"

# Create provider
gcloud iam workload-identity-pools providers create-oidc github-provider \
  --project=$PROJECT_ID \
  --location="global" \
  --workload-identity-pool="github-pool" \
  --display-name="GitHub Provider" \
  --attribute-mapping="google.subject=assertion.sub,attribute.actor=assertion.actor,attribute.repository=assertion.repository" \
  --issuer-uri="https://token.actions.githubusercontent.com"

# Allow GitHub to impersonate service account
gcloud iam service-accounts add-iam-policy-binding \
  github-packer@$PROJECT_ID.iam.gserviceaccount.com \
  --project=$PROJECT_ID \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/github-pool/attribute.repository/luxfi/node"
```

### 3. Get Provider URL

```bash
# Get the provider resource name
gcloud iam workload-identity-pools providers describe github-provider \
  --project=$PROJECT_ID \
  --location="global" \
  --workload-identity-pool="github-pool" \
  --format="value(name)"

# Output format: projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/github-pool/providers/github-provider
```

### 4. Add Secrets to GitHub

```
GCP_PROJECT_ID = your-gcp-project
GCP_WORKLOAD_IDENTITY_PROVIDER = projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/github-pool/providers/github-provider
GCP_SERVICE_ACCOUNT = github-packer@your-gcp-project.iam.gserviceaccount.com
```

---

## Azure Setup

### 1. Create Resource Group

```bash
az group create --name luxfi-images --location eastus
```

### 2. Create App Registration

```bash
# Create app registration
az ad app create --display-name "GitHub Actions Lux Packer"

# Get the app ID
APP_ID=$(az ad app list --display-name "GitHub Actions Lux Packer" --query "[0].appId" -o tsv)

# Create service principal
az ad sp create --id $APP_ID

# Create client secret
az ad app credential reset --id $APP_ID --display-name "github-actions"
```

### 3. Assign Permissions

```bash
SUBSCRIPTION_ID=$(az account show --query id -o tsv)
RESOURCE_GROUP="luxfi-images"

# Grant Contributor on resource group
az role assignment create \
  --assignee $APP_ID \
  --role "Contributor" \
  --scope "/subscriptions/$SUBSCRIPTION_ID/resourceGroups/$RESOURCE_GROUP"

# Grant permissions to create VMs (for Packer)
az role assignment create \
  --assignee $APP_ID \
  --role "Virtual Machine Contributor" \
  --scope "/subscriptions/$SUBSCRIPTION_ID"
```

### 4. Create Shared Image Gallery (Optional)

```bash
# Create gallery for image distribution
az sig create \
  --resource-group $RESOURCE_GROUP \
  --gallery-name luxdGallery

# Create image definition
az sig image-definition create \
  --resource-group $RESOURCE_GROUP \
  --gallery-name luxdGallery \
  --gallery-image-definition luxd \
  --publisher luxfi \
  --offer luxd \
  --sku node \
  --os-type Linux \
  --os-state Generalized \
  --hyper-v-generation V2
```

### 5. Add Secrets to GitHub

```
AZURE_CLIENT_ID = <App ID from step 2>
AZURE_CLIENT_SECRET = <Secret from step 2>
AZURE_TENANT_ID = <Your Azure AD tenant ID>
AZURE_SUBSCRIPTION_ID = <Your subscription ID>
AZURE_RESOURCE_GROUP = luxfi-images
```

---

## Testing

### Manual Workflow Trigger

```bash
# Trigger AWS build
gh workflow run build-aws-ami.yml -f tag=v1.21.15

# Trigger GCP build
gh workflow run build-gcp-image.yml -f tag=v1.21.15

# Trigger Azure build
gh workflow run build-azure-image.yml -f tag=v1.21.15

# Trigger all clouds
gh workflow run build-all-cloud-images.yml -f tag=v1.21.15
```

### Verify Images

```bash
# AWS
aws ec2 describe-images --owners self --filters "Name=name,Values=luxd-*"

# GCP
gcloud compute images list --filter="family:luxd"

# Azure
az image list --resource-group luxfi-images
```

---

## Launching Nodes

### AWS

```bash
aws ec2 run-instances \
  --image-id ami-XXXXXXXXX \
  --instance-type c5.xlarge \
  --key-name your-key \
  --security-group-ids sg-XXXXXXXX \
  --subnet-id subnet-XXXXXXXX \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=luxd-node}]'
```

### GCP

```bash
gcloud compute instances create luxd-node \
  --image-family=luxd \
  --image-project=your-project \
  --machine-type=n2-standard-4 \
  --boot-disk-size=500GB \
  --zone=us-central1-a
```

### Azure

```bash
az vm create \
  --resource-group luxfi \
  --name luxd-node \
  --image luxfi-images/luxd-ubuntu-22-04-v1-21-15 \
  --size Standard_D4s_v3 \
  --admin-username ubuntu \
  --generate-ssh-keys \
  --os-disk-size-gb 500
```

---

## Recommended Instance Sizes

| Cloud | Minimum | Recommended | Archive Node |
|-------|---------|-------------|--------------|
| AWS | c5.large | c5.xlarge | c5.2xlarge |
| GCP | n2-standard-2 | n2-standard-4 | n2-standard-8 |
| Azure | Standard_D2s_v3 | Standard_D4s_v3 | Standard_D8s_v3 |

## Ports to Open

| Port | Protocol | Purpose |
|------|----------|---------|
| 9630 | TCP | HTTP RPC |
| 9631 | TCP | Staking |
| 9632 | TCP | HTTP API |
| 22 | TCP | SSH (optional) |

#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Error handler
error_exit() {
    echo -e "${RED}Error: $1${NC}" >&2
    exit 1
}

# Command check
check_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        error_exit "$1 is not installed. Please install it first."
    fi
}

# Check required commands
check_command "node"
check_command "npm"
check_command "git"

# Ensure we're in the repository root
if [ ! -f "package.json" ]; then
    error_exit "package.json not found. Please run this script from the repository root."
fi

# Check if version argument is provided
if [ $# -ne 1 ]; then
    echo -e "${YELLOW}Usage: $0 <version>${NC}"
    echo -e "${YELLOW}Example: $0 v1.0.0${NC}"
    exit 1
fi

VERSION=$1

# Validate version starts with 'v' and follows format (vx.y.z)
if ! [[ $VERSION =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    error_exit "Version must start with 'v' and be in format vx.y.z (e.g. v1.0.0)"
fi

# Check if tag exists locally
if git show-ref --tags "$VERSION" --quiet; then
    error_exit "Version $VERSION already exists locally. Please choose a different version."
fi

# Check if tag exists remotely
if git ls-remote --exit-code --tags origin "refs/tags/$VERSION" >/dev/null 2>&1; then
    error_exit "Version $VERSION already exists in remote repository. Please choose a different version."
fi

# Create and switch to a new release branch
BRANCH_NAME="release-${VERSION}"
echo -e "\n${YELLOW}Creating release branch $BRANCH_NAME...${NC}"
git checkout -b "$BRANCH_NAME" main || error_exit "Failed to create release branch"

# Ensure working directory is clean
if ! git diff-index --quiet HEAD --; then
    error_exit "Working directory is not clean. Please commit or stash changes."
fi

# Pull latest changes from main
echo -e "\n${YELLOW}Pulling latest changes from main...${NC}"
git pull origin main || error_exit "Failed to pull latest changes"

# Strip 'v' prefix for npm version command
NPM_VERSION=${VERSION#v}

# Update version in package.json
echo -e "\n${YELLOW}Updating version to $NPM_VERSION...${NC}"
npm version $NPM_VERSION --no-git-tag-version || error_exit "Failed to update version in package.json"

# Commit changes
echo -e "\n${YELLOW}Committing changes...${NC}"
git add package.json || error_exit "Failed to stage package.json"
git commit -m "chore: bump version to $VERSION" || error_exit "Failed to commit version bump"

# Push the release branch
echo -e "\n${YELLOW}Pushing release branch...${NC}"
git push -u origin "$BRANCH_NAME" || error_exit "Failed to push release branch"

echo -e "\n${GREEN}✨ Release branch prepared successfully!${NC}"
echo -e "${GREEN}Branch: $BRANCH_NAME${NC}"
echo -e "\n${YELLOW}Next steps:${NC}"
echo "1. Create a pull request from $BRANCH_NAME to main"
echo "2. Once the PR is merged, tag will be created automatically"
echo -e "\nCreate PR: https://github.com/last9/last9-mcp-server/compare/main...$BRANCH_NAME" 
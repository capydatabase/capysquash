#!/bin/bash
set -euo pipefail

# Docker build and publish script for pgsquash
# This script builds, tests, and optionally publishes Docker images

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${CYAN}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
    exit 1
}

log_step() {
    echo -e "${PURPLE}[STEP]${NC} $1" >&2
}

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TAG=$(git describe --tags --exact-match 2>/dev/null || echo "")
VERSION=${GIT_TAG:-${BUILD_VERSION:-"dev-$GIT_COMMIT"}}

# Default values
REGISTRY=${DOCKER_REGISTRY:-""}
REPOSITORY=${DOCKER_REPOSITORY:-"pgsquash"}
PlatformS=${DOCKER_PlatformS:-"linux/amd64,linux/arm64"}
PUSH=${DOCKER_PUSH:-false}
LOAD=${DOCKER_LOAD:-true}
NO_CACHE=${DOCKER_NO_CACHE:-false}
BUILD_ARGS=${DOCKER_BUILD_ARGS:-""}

# Docker buildx setup
BUILDER_NAME="pgsquash-builder"

show_banner() {
    cat << 'EOF'
   ____  ____      ____   ____  __  __  ____  _____ __  __
  |  _ \/ ___|    / ___| / ___||  \/  |/ _  \/ ___||  \/  |
  | |_) \___ \   \___ \| |    | |\/| |  | | \___ \| |\/| |
  |  __/ ___) |   ___) | |___ | |  | |  |_| |___) | |  | |
  |_|   |____/   |____/ \____||_|  |_|\____|____/|_|  |_|

  🐳 Docker Build & Publish Pipeline

EOF
}

show_help() {
    cat << EOF
USAGE:
    $0 [OPTIONS]

OPTIONS:
    -r, --registry REGISTRY     Docker registry (default: $REGISTRY)
    -p, --repository REPO       Repository name (default: $REPOSITORY)
    -v, --version VERSION       Version tag (default: $VERSION)
    -t, --Platforms PlatformS   Target Platforms (default: $PlatformS)
    --push                      Push to registry
    --no-load                   Don't load image locally
    --no-cache                  Build without cache
    --build-args ARGS           Additional build arguments
    -h, --help                  Show this help

EXAMPLES:
    # Build locally
    $0

    # Build and push to Docker Hub
    $0 --registry docker.io --repository myuser/pgsquash --push

    # Build for multiple Platforms
    $0 --Platforms "linux/amd64,linux/arm64,linux/arm/v7" --push

    # Build specific version
    $0 --version v1.2.3 --push

ENVIRONMENT VARIABLES:
    DOCKER_REGISTRY     Default registry
    DOCKER_REPOSITORY   Default repository
    DOCKER_PlatformS    Default Platforms
    DOCKER_PUSH         Push images (true/false)
    DOCKER_LOAD         Load images locally (true/false)
    DOCKER_NO_CACHE     Disable build cache (true/false)
    BUILD_VERSION       Build version override

EOF
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -p|--repository)
            REPOSITORY="$2"
            shift 2
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        -t|--Platforms)
            PlatformS="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --no-load)
            LOAD=false
            shift
            ;;
        --no-cache)
            NO_CACHE=true
            shift
            ;;
        --build-args)
            BUILD_ARGS="$2"
            shift 2
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            ;;
    esac
done

# Construct full image name
if [[ -n "$REGISTRY" ]]; then
    FULL_IMAGE_NAME="$REGISTRY/$REPOSITORY"
else
    FULL_IMAGE_NAME="$REPOSITORY"
fi

# Setup buildx
setup_buildx() {
    log_step "Setting up Docker buildx..."

    # Create builder if it doesn't exist
    if ! docker buildx inspect "$BUILDER_NAME" &> /dev/null; then
        log_info "Creating buildx builder: $BUILDER_NAME"
        docker buildx create --name "$BUILDER_NAME" --use --bootstrap
    else
        log_info "Using existing buildx builder: $BUILDER_NAME"
        docker buildx use "$BUILDER_NAME"
    fi

    log_success "Docker buildx ready"
}

# Pre-build checks
pre_build_checks() {
    log_step "Performing pre-build checks..."

    # Check if we're in the right directory
    if [[ ! -f "$PROJECT_ROOT/Dockerfile" ]]; then
        log_error "Dockerfile not found. Please run from project root."
    fi

    # Check if go.mod exists
    if [[ ! -f "$PROJECT_ROOT/go.mod" ]]; then
        log_error "go.mod not found. This doesn't appear to be a Go project."
    fi

    # Check Docker buildx
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed or not in PATH"
    fi

    if ! docker buildx version &> /dev/null; then
        log_error "Docker buildx is not available"
    fi

    log_success "Pre-build checks passed"
}

# Build the image
build_image() {
    log_step "Building Docker image..."

    local build_args_list=""
    local cache_args=""
    local output_args=""
    local Platform_args=""

    # Prepare build arguments
    build_args_list+=" --build-arg BUILD_VERSION=$VERSION"
    build_args_list+=" --build-arg BUILD_DATE=$BUILD_DATE"
    build_args_list+=" --build-arg GIT_COMMIT=$GIT_COMMIT"

    if [[ -n "$BUILD_ARGS" ]]; then
        build_args_list+=" $BUILD_ARGS"
    fi

    # Cache configuration
    if [[ "$NO_CACHE" == "true" ]]; then
        cache_args=" --no-cache"
    else
        cache_args=" --cache-from=type=local,src=/tmp/.buildx-cache"
        cache_args+=" --cache-to=type=local,dest=/tmp/.buildx-cache-new,mode=max"
    fi

    # Platform configuration
    if [[ -n "$PlatformS" ]]; then
        Platform_args=" --Platform $PlatformS"
    fi

    # Output configuration
    if [[ "$PUSH" == "true" ]]; then
        output_args=" --push"
    elif [[ "$LOAD" == "true" ]] && [[ "$PlatformS" == "linux/amd64" || "$PlatformS" =~ ^linux/amd64, || ! "$PlatformS" =~ , ]]; then
        output_args=" --load"
    else
        log_warning "Multi-Platform build detected. Images will not be loaded locally."
        output_args=""
    fi

    # Build tags
    local tags=""
    tags+=" -t $FULL_IMAGE_NAME:$VERSION"
    tags+=" -t $FULL_IMAGE_NAME:latest"

    if [[ -n "$GIT_TAG" ]]; then
        tags+=" -t $FULL_IMAGE_NAME:$GIT_TAG"
    fi

    # Execute build
    log_info "Building with tags: $tags"
    log_info "Platforms: $PlatformS"
    log_info "Push: $PUSH"
    log_info "Load: $LOAD"

    eval docker buildx build \
        "$Platform_args" \
        "$cache_args" \
        "$build_args_list" \
        "$tags" \
        "$output_args" \
        "$PROJECT_ROOT"

    # Move cache
    if [[ "$NO_CACHE" != "true" ]]; then
        rm -rf /tmp/.buildx-cache
        mv /tmp/.buildx-cache-new /tmp/.buildx-cache 2>/dev/null || true
    fi

    log_success "Docker image built successfully"
}

# Test the image
test_image() {
    if [[ "$LOAD" != "true" ]]; then
        log_warning "Skipping image tests (image not loaded locally)"
        return 0
    fi

    log_step "Testing Docker image..."

    # Basic functionality test
    log_info "Testing basic functionality..."
    if ! docker run --rm "$FULL_IMAGE_NAME:$VERSION" --help > /dev/null 2>&1; then
        log_error "Basic functionality test failed"
    fi

    # Test help for specific command
    log_info "Testing help command..."
    if ! docker run --rm "$FULL_IMAGE_NAME:$VERSION" squash --help > /dev/null 2>&1; then
        log_error "Help command test failed"
    fi

    # Test init-config command
    log_info "Testing init-config command..."
    if ! docker run --rm "$FULL_IMAGE_NAME:$VERSION" init-config > /dev/null 2>&1; then
        log_warning "Init-config test failed (non-critical)"
    fi

    log_success "Image tests completed"
}

# Security scanning
security_scan() {
    if ! command -v trivy &> /dev/null; then
        log_warning "Trivy not found - skipping security scan"
        return 0
    fi

    log_step "Performing security scan..."

    if ! trivy image --exit-code 1 "$FULL_IMAGE_NAME:$VERSION"; then
        log_warning "Security vulnerabilities found (check output above)"
    else
        log_success "Security scan passed"
    fi
}

# Generate image metadata
generate_metadata() {
    log_step "Generating image metadata..."

    local metadata_file="$PROJECT_ROOT/image-metadata.json"

    cat > "$metadata_file" << EOF
{
    "image": "$FULL_IMAGE_NAME:$VERSION",
    "version": "$VERSION",
    "build_date": "$BUILD_DATE",
    "git_commit": "$GIT_COMMIT",
    "git_tag": "$GIT_TAG",
    "Platforms": "$PlatformS",
    "registry": "$REGISTRY",
    "repository": "$REPOSITORY",
    "pushed": $PUSH,
    "tags": [
        "$FULL_IMAGE_NAME:$VERSION",
        "$FULL_IMAGE_NAME:latest"
EOF

    if [[ -n "$GIT_TAG" ]]; then
        echo ",        \"$FULL_IMAGE_NAME:$GIT_TAG\"" >> "$metadata_file"
    fi

    echo "    ]" >> "$metadata_file"
    echo "}" >> "$metadata_file"

    log_success "Metadata saved to: $metadata_file"
}

# Show build summary
show_summary() {
    log_success "🎉 Build completed successfully!"
    echo
    log_info "📊 Build Summary:"
    log_info "  ► Image: $FULL_IMAGE_NAME:$VERSION"
    log_info "  ► Platforms: $PlatformS"
    log_info "  ► Build Date: $BUILD_DATE"
    log_info "  ► Git Commit: $GIT_COMMIT"
    if [[ -n "$GIT_TAG" ]]; then
        log_info "  ► Git Tag: $GIT_TAG"
    fi
    log_info "  ► Pushed to Registry: $PUSH"
    log_info "  ► Loaded Locally: $LOAD"
    echo
    if [[ "$PUSH" == "true" ]]; then
        log_info "🚀 Image published and ready to use:"
        log_info "  docker pull $FULL_IMAGE_NAME:$VERSION"
    else
        log_info "🏠 Image available locally:"
        log_info "  docker run --rm $FULL_IMAGE_NAME:$VERSION --help"
    fi
    echo
    log_info "💡 Quick start with Docker Compose:"
    log_info "  docker-compose up pgsquash"
}

# Main execution
main() {
    cd "$PROJECT_ROOT"

    show_banner

    log_info "🔧 Configuration:"
    log_info "  ► Registry: ${REGISTRY:-"(none)"}"
    log_info "  ► Repository: $REPOSITORY"
    log_info "  ► Version: $VERSION"
    log_info "  ► Platforms: $PlatformS"
    log_info "  ► Push: $PUSH"
    log_info "  ► Load: $LOAD"
    echo

    pre_build_checks
    setup_buildx
    build_image
    test_image
    security_scan
    generate_metadata
    show_summary
}

# Execute main function
main "$@"
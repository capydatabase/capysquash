#!/bin/bash
set -euo pipefail

# pgsquash Universal Initialization Script
# This script sets up a complete pgsquash environment

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

log_substep() {
    echo -e "${BLUE}  →${NC} $1" >&2
}

# ASCII art banner
show_banner() {
    cat << 'EOF'
                                                                      $$╲
                                                                      $$ │
 $$$$$$╲   $$$$$$╲   $$$$$$$╲  $$$$$$╲  $$╲   $$╲  $$$$$$╲   $$$$$$$╲ $$$$$$$╲
$$  __$$╲ $$  __$$╲ $$  _____│$$  __$$╲ $$ │  $$ │ ╲____$$╲ $$  _____│$$  __$$╲
$$ ╱  $$ │$$ ╱  $$ │╲$$$$$$╲  $$ ╱  $$ │$$ │  $$ │ $$$$$$$ │╲$$$$$$╲  $$ │  $$ │
$$ │  $$ │$$ │  $$ │ ╲____$$╲ $$ │  $$ │$$ │  $$ │$$  __$$ │ ╲____$$╲ $$ │  $$ │
$$$$$$$  │╲$$$$$$$ │$$$$$$$  │╲$$$$$$$ │╲$$$$$$  │╲$$$$$$$ │$$$$$$$  │$$ │  $$ │
$$  ____╱  ╲____$$ │╲_______╱  ╲____$$ │ ╲______╱  ╲_______│╲_______╱ ╲__│  ╲__│
$$ │      $$╲   $$ │                $$ │
$$ │      ╲$$$$$$  │                $$ │
╲__│       ╲______╱                 ╲__│
by
                  ▌
▛▘▀▌▛▌▌▌▛▘▛▌▌▌▀▌▛▘▛▌
▙▖█▌▙▌▙▌▄▌▙▌▙▌█▌▄▌▌▌
    ▌ ▄▌   ▌

   🚀 PostgreSQL Migration Squasher - Universal Initialization

EOF
}

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/pgsquash}"
DATA_DIR="${DATA_DIR:-$HOME/.local/share/pgsquash}"

# Runtime options
DOCKER_COMPOSE_PROFILES="${DOCKER_COMPOSE_PROFILES:-}"
SKIP_DOCKER_PULL="${SKIP_DOCKER_PULL:-false}"
ENABLE_SAMPLE_DATA="${ENABLE_SAMPLE_DATA:-false}"
INTERACTIVE_MODE="${INTERACTIVE_MODE:-true}"
INSTALL_MODE="${INSTALL_MODE:-auto}" # auto, docker, native, dev

# Show help
show_help() {
    cat << EOF
USAGE:
    $0 [OPTIONS] [COMMAND]

COMMANDS:
    install     Install pgsquash (default)
    update      Update existing installation
    uninstall   Remove pgsquash
    dev         Set up development environment
    test        Run setup tests

OPTIONS:
    --install-mode MODE     Installation mode: auto|docker|native|dev (default: auto)
    --install-dir DIR       Installation directory (default: $INSTALL_DIR)
    --config-dir DIR        Configuration directory (default: $CONFIG_DIR)
    --data-dir DIR          Data directory (default: $DATA_DIR)
    --profiles PROFILES     Docker Compose profiles (default: none)
    --skip-docker-pull      Skip pulling Docker images
    --enable-samples        Create sample migration data
    --non-interactive       Run without user interaction
    -h, --help              Show this help

EXAMPLES:
    # Automatic installation (detects best method)
    $0

    # Force Docker-based installation
    $0 --install-mode docker

    # Development environment setup
    $0 dev

    # Install with monitoring and management tools
    $0 --profiles "monitoring,management"

    # Clean install with samples
    $0 --enable-samples --non-interactive

ENVIRONMENT VARIABLES:
    INSTALL_DIR             Override installation directory
    CONFIG_DIR              Override configuration directory
    DATA_DIR                Override data directory
    DOCKER_COMPOSE_PROFILES Override Docker profiles
    PGSQUASH_VERSION        Specific version to install

EOF
}

# Parse command line arguments
parse_args() {
    local command="install"

    while [[ $# -gt 0 ]]; do
        case $1 in
            install|update|uninstall|dev|test)
                command="$1"
                shift
                ;;
            --install-mode)
                INSTALL_MODE="$2"
                shift 2
                ;;
            --install-dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            --config-dir)
                CONFIG_DIR="$2"
                shift 2
                ;;
            --data-dir)
                DATA_DIR="$2"
                shift 2
                ;;
            --profiles)
                DOCKER_COMPOSE_PROFILES="$2"
                shift 2
                ;;
            --skip-docker-pull)
                SKIP_DOCKER_PULL="true"
                shift
                ;;
            --enable-samples)
                ENABLE_SAMPLE_DATA="true"
                shift
                ;;
            --non-interactive)
                INTERACTIVE_MODE="false"
                shift
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

    echo "$command"
}

# Detect installation method
detect_install_mode() {
    if [[ "$INSTALL_MODE" != "auto" ]]; then
        return 0
    fi

    log_step "Detecting optimal installation method..."

    # Check if we're in development environment
    if [[ -f "$PROJECT_ROOT/go.mod" && -f "$PROJECT_ROOT/Dockerfile" ]]; then
        log_substep "Development environment detected"
        INSTALL_MODE="dev"
        return 0
    fi

    # Check Docker availability
    if command -v docker &> /dev/null && command -v docker-compose &> /dev/null; then
        log_substep "Docker environment detected"
        INSTALL_MODE="docker"
        return 0
    fi

    # Check Go availability for native build
    if command -v go &> /dev/null; then
        log_substep "Go environment detected"
        INSTALL_MODE="native"
        return 0
    fi

    # Fallback to Docker (will prompt for installation if needed)
    log_substep "Defaulting to Docker installation"
    INSTALL_MODE="docker"
}

# Install system dependencies
install_system_deps() {
    log_step "Installing system dependencies..."

    # Detect OS
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        if command -v brew &> /dev/null; then
            log_substep "Installing dependencies via Homebrew..."
            brew install docker docker-compose git curl jq || log_warning "Some dependencies may have failed"
        else
            log_warning "Homebrew not found. Please install Docker and docker-compose manually."
        fi
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        if command -v apt-get &> /dev/null; then
            log_substep "Installing dependencies via apt-get..."
            sudo apt-get update
            sudo apt-get install -y docker.io docker-compose git curl jq
        elif command -v yum &> /dev/null; then
            log_substep "Installing dependencies via yum..."
            sudo yum install -y docker docker-compose git curl jq
        elif command -v pacman &> /dev/null; then
            log_substep "Installing dependencies via pacman..."
            sudo pacman -S --noconfirm docker docker-compose git curl jq
        else
            log_warning "Package manager not recognized. Please install Docker and docker-compose manually."
        fi
    fi

    log_success "System dependencies installed"
}

# Create directory structure
create_directories() {
    log_step "Creating directory structure..."

    local dirs=(
        "$INSTALL_DIR"
        "$CONFIG_DIR"
        "$DATA_DIR"
        "$DATA_DIR/migrations"
        "$DATA_DIR/output"
        "$DATA_DIR/logs"
        "$DATA_DIR/cache"
        "$DATA_DIR/backups"
    )

    for dir in "${dirs[@]}"; do
        log_substep "Creating: $dir"
        mkdir -p "$dir"
    done

    log_success "Directory structure created"
}

# Download and setup Docker Compose
setup_docker_compose() {
    log_step "Setting up Docker Compose environment..."

    # Download docker-compose.yml
    log_substep "Downloading docker-compose.yml..."
    curl -sSL "https://raw.githubusercontent.com/capysquash/pgsquash/main/docker-compose.yml" \
        -o "$CONFIG_DIR/docker-compose.yml"

    # Download .env.example and create .env
    log_substep "Setting up environment configuration..."
    curl -sSL "https://raw.githubusercontent.com/capysquash/pgsquash/main/.env.example" \
        -o "$CONFIG_DIR/.env.example"

    if [[ ! -f "$CONFIG_DIR/.env" ]]; then
        cp "$CONFIG_DIR/.env.example" "$CONFIG_DIR/.env"

        # Customize .env with secure passwords
        log_substep "Generating secure passwords..."
        local postgres_password=$(openssl rand -base64 32)
        local redis_password=$(openssl rand -base64 32)
        local minio_password=$(openssl rand -base64 32)

        sed -i.bak \
            -e "s/pgsquash_secure_password_change_me/$postgres_password/g" \
            -e "s/pgsquash_redis_change_me/$redis_password/g" \
            -e "s/pgsquash_minio_password_change_me/$minio_password/g" \
            "$CONFIG_DIR/.env"

        rm "$CONFIG_DIR/.env.bak"
    fi

    # Set profiles if provided
    if [[ -n "$DOCKER_COMPOSE_PROFILES" ]]; then
        echo "COMPOSE_PROFILES=$DOCKER_COMPOSE_PROFILES" >> "$CONFIG_DIR/.env"
    fi

    log_success "Docker Compose environment configured"
}

# Pull Docker images
pull_docker_images() {
    if [[ "$SKIP_DOCKER_PULL" == "true" ]]; then
        log_substep "Skipping Docker image pull"
        return 0
    fi

    log_step "Pulling Docker images..."

    cd "$CONFIG_DIR"

    # Pull base images
    local images=(
        "postgres:17"
        "postgres:17"
        "redis:7"
        "ghcr.io/capysquash/pgsquash:latest"
    )

    for image in "${images[@]}"; do
        log_substep "Pulling: $image"
        docker pull "$image" || log_warning "Failed to pull $image"
    done

    # Pull compose images
    log_substep "Pulling compose services..."
    docker-compose pull || log_warning "Some compose images may have failed"

    log_success "Docker images ready"
}

# Install native binary
install_native() {
    log_step "Installing native binary..."

    # Check if Go is available
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed. Please install Go 1.21+ or use Docker installation."
    fi

    # Clone or update repository
    local repo_dir="$DATA_DIR/src"
    if [[ -d "$repo_dir" ]]; then
        log_substep "Updating existing repository..."
        cd "$repo_dir"
        git pull origin main
    else
        log_substep "Cloning repository..."
        git clone https://github.com/CAPYSQUASH/pgsquash-engine.git "$repo_dir"
        cd "$repo_dir"
    fi

    # Build binary
    log_substep "Building binary..."
    go build -ldflags="-w -s" -o "$INSTALL_DIR/pgsquash" cmd/pgsquash/main.go

    # Make executable
    chmod +x "$INSTALL_DIR/pgsquash"

    log_success "Native binary installed"
}

# Setup development environment
setup_development() {
    log_step "Setting up development environment..."

    if [[ ! -f "$PROJECT_ROOT/go.mod" ]]; then
        log_error "Not in a pgsquash development directory"
    fi

    cd "$PROJECT_ROOT"

    # Install dependencies
    log_substep "Installing Go dependencies..."
    go mod download

    # Build development binary
    log_substep "Building development binary..."
    go build -o "$INSTALL_DIR/pgsquash-dev" cmd/pgsquash/main.go
    chmod +x "$INSTALL_DIR/pgsquash-dev"

    # Setup development compose
    log_substep "Setting up development Docker Compose..."
    cp docker-compose.yml "$CONFIG_DIR/docker-compose.dev.yml"
    cp .env.example "$CONFIG_DIR/.env.dev"

    # Run tests
    log_substep "Running tests..."
    go test ./... || log_warning "Some tests failed"

    log_success "Development environment ready"
}

# Create wrapper scripts
create_wrappers() {
    log_step "Creating wrapper scripts..."

    # Docker wrapper
    cat > "$INSTALL_DIR/pgsquash-docker" << 'EOF'
#!/bin/bash
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/pgsquash}"
DATA_DIR="${DATA_DIR:-$HOME/.local/share/pgsquash}"

# Change to config directory
cd "$CONFIG_DIR"

# Run pgsquash via Docker Compose
exec docker-compose run --rm \
    -v "$PWD:/app/migrations" \
    -v "$DATA_DIR/output:/app/output" \
    -v "$DATA_DIR/logs:/app/logs" \
    pgsquash "$@"
EOF

    # Native wrapper (if exists)
    if [[ -f "$INSTALL_DIR/pgsquash" ]]; then
        cat > "$INSTALL_DIR/pgsquash-native" << EOF
#!/bin/bash
exec "$INSTALL_DIR/pgsquash" "\$@"
EOF
    fi

    # Main wrapper that detects available method
    cat > "$INSTALL_DIR/pgsquash" << 'EOF'
#!/bin/bash
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/pgsquash}"

# Try native first, then Docker
if [[ -f "$INSTALL_DIR/pgsquash-native" ]] && [[ -x "$INSTALL_DIR/pgsquash-native" ]]; then
    exec "$INSTALL_DIR/pgsquash-native" "$@"
elif [[ -f "$INSTALL_DIR/pgsquash-docker" ]] && [[ -x "$INSTALL_DIR/pgsquash-docker" ]]; then
    exec "$INSTALL_DIR/pgsquash-docker" "$@"
else
    echo "ERROR: No pgsquash installation found" >&2
    exit 1
fi
EOF

    # Make all wrappers executable
    find "$INSTALL_DIR" -name "pgsquash*" -type f -exec chmod +x {} \;

    log_success "Wrapper scripts created"
}

# Generate sample data
generate_sample_data() {
    if [[ "$ENABLE_SAMPLE_DATA" != "true" ]]; then
        return 0
    fi

    log_step "Generating sample migration data..."

    local sample_script="$DATA_DIR/create-samples.sh"

    # Download sample generator
    curl -sSL "https://raw.githubusercontent.com/capysquash/pgsquash/main/docker/init-scripts/create-sample-migrations.sh" \
        -o "$sample_script"
    chmod +x "$sample_script"

    # Generate samples
    "$sample_script" "$DATA_DIR/migrations"

    log_success "Sample migration data generated"
}

# Setup shell integration
setup_shell_integration() {
    log_step "Setting up shell integration..."

    # Add to PATH if not already there
    local shell_rc=""
    case "$SHELL" in
        */bash)
            shell_rc="$HOME/.bashrc"
            ;;
        */zsh)
            shell_rc="$HOME/.zshrc"
            ;;
        */fish)
            shell_rc="$HOME/.config/fish/config.fish"
            ;;
    esac

    if [[ -n "$shell_rc" ]] && [[ -f "$shell_rc" ]]; then
        if ! grep -q "$INSTALL_DIR" "$shell_rc"; then
            log_substep "Adding $INSTALL_DIR to PATH in $shell_rc"
            echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$shell_rc"
        fi
    fi

    # Create bash completion
    if command -v pgsquash &> /dev/null; then
        local completion_dir="$HOME/.local/share/bash-completion/completions"
        mkdir -p "$completion_dir"

        log_substep "Installing bash completion..."
        pgsquash completion bash > "$completion_dir/pgsquash" 2>/dev/null || true
    fi

    log_success "Shell integration configured"
}

# Start services
start_services() {
    if [[ "$INSTALL_MODE" != "docker" ]]; then
        return 0
    fi

    log_step "Starting pgsquash services..."

    cd "$CONFIG_DIR"

    # Start essential services
    log_substep "Starting PostgreSQL..."
    docker-compose up -d postgres-primary

    # Wait for PostgreSQL to be ready
    log_substep "Waiting for PostgreSQL to be ready..."
    timeout 60s bash -c 'until docker-compose exec -T postgres-primary pg_isready -U pgsquash; do sleep 2; done'

    # Start additional services based on profiles
    if [[ -n "$DOCKER_COMPOSE_PROFILES" ]]; then
        log_substep "Starting additional services..."
        docker-compose up -d
    fi

    log_success "Services started successfully"
}

# Run verification tests
run_verification() {
    log_step "Running verification tests..."

    # Test binary availability
    if command -v pgsquash &> /dev/null; then
        log_substep "☑ Binary accessible"
        pgsquash --version
    else
        log_error "Binary not accessible. Check PATH configuration."
    fi

    # Test configuration
    if [[ -f "$CONFIG_DIR/.env" ]]; then
        log_substep "☑ Configuration found"
    else
        log_warning "Configuration file not found"
    fi

    # Test Docker services (if applicable)
    if [[ "$INSTALL_MODE" == "docker" ]]; then
        cd "$CONFIG_DIR"
        if docker-compose ps | grep -q "postgres-primary"; then
            log_substep "☑ Docker services running"
        else
            log_warning "Docker services not running"
        fi
    fi

    # Test sample data (if generated)
    if [[ "$ENABLE_SAMPLE_DATA" == "true" ]]; then
        if [[ -d "$DATA_DIR/migrations" ]] && [[ -n "$(ls -A "$DATA_DIR/migrations")" ]]; then
            log_substep "☑ Sample data available"
        else
            log_warning "Sample data not found"
        fi
    fi

    log_success "Verification completed"
}

# Show installation summary
show_summary() {
    log_success "🎉 pgsquash installation completed!"
    echo
    log_info "📁 Installation Details:"
    log_info "  ► Install Directory: $INSTALL_DIR"
    log_info "  ► Config Directory:  $CONFIG_DIR"
    log_info "  ► Data Directory:    $DATA_DIR"
    log_info "  ► Install Mode:      $INSTALL_MODE"
    if [[ -n "$DOCKER_COMPOSE_PROFILES" ]]; then
        log_info "  ► Docker Profiles:   $DOCKER_COMPOSE_PROFILES"
    fi
    echo
    log_info "🚀 Quick Start Commands:"
    log_info "  # Check installation"
    log_info "  pgsquash --version"
    echo
    log_info "  # Initialize with samples"
    log_info "  pgsquash init --sample"
    echo
    log_info "  # Squash migrations"
    log_info "  pgsquash squash migrations/*.sql --output output/"
    echo
    log_info "  # Full workflow with validation"
    log_info "  pgsquash workflow --input migrations/ --output output/ --validate"
    echo

    if [[ "$INSTALL_MODE" == "docker" ]]; then
        log_info "🐳 Docker Management:"
        log_info "  # View services status"
        log_info "  cd $CONFIG_DIR && docker-compose ps"
        echo
        log_info "  # View logs"
        log_info "  cd $CONFIG_DIR && docker-compose logs -f"
        echo
        log_info "  # Stop services"
        log_info "  cd $CONFIG_DIR && docker-compose down"
    fi

    echo
    log_info "📚 Documentation: https://github.com/CAPYSQUASH/pgsquash-engine"
    log_info "💬 Support: https://github.com/CAPYSQUASH/pgsquash-engine/issues"

    if [[ "$INTERACTIVE_MODE" == "true" ]]; then
        echo
        read -p "Press Enter to continue..."
    fi
}

# Interactive configuration
interactive_config() {
    if [[ "$INTERACTIVE_MODE" != "true" ]]; then
        return 0
    fi

    echo
    log_step "Interactive Configuration"
    echo

    # Installation mode
    if [[ "$INSTALL_MODE" == "auto" ]]; then
        echo "Select installation mode:"
        echo "  1) Docker (recommended)"
        echo "  2) Native binary"
        echo "  3) Development environment"
        read -p "Choose [1-3]: " choice
        case $choice in
            1) INSTALL_MODE="docker" ;;
            2) INSTALL_MODE="native" ;;
            3) INSTALL_MODE="dev" ;;
            *) INSTALL_MODE="docker" ;;
        esac
    fi

    # Docker profiles
    if [[ "$INSTALL_MODE" == "docker" ]]; then
        echo
        echo "Select additional services:"
        echo "  1) None (minimal)"
        echo "  2) Monitoring (Grafana, Prometheus)"
        echo "  3) Management (pgAdmin, File Browser)"
        echo "  4) All services"
        read -p "Choose [1-4]: " choice
        case $choice in
            1) DOCKER_COMPOSE_PROFILES="" ;;
            2) DOCKER_COMPOSE_PROFILES="monitoring" ;;
            3) DOCKER_COMPOSE_PROFILES="management" ;;
            4) DOCKER_COMPOSE_PROFILES="monitoring,management,storage,proxy" ;;
            *) DOCKER_COMPOSE_PROFILES="" ;;
        esac
    fi

    # Sample data
    echo
    read -p "Generate sample migration data? [y/N]: " choice
    case $choice in
        [yY]|[yY][eE][sS]) ENABLE_SAMPLE_DATA="true" ;;
        *) ENABLE_SAMPLE_DATA="false" ;;
    esac
}

# Uninstall function
uninstall_pgsquash() {
    log_step "Uninstalling pgsquash..."

    # Stop Docker services
    if [[ -f "$CONFIG_DIR/docker-compose.yml" ]]; then
        cd "$CONFIG_DIR"
        docker-compose down -v || true
    fi

    # Remove binaries
    rm -f "$INSTALL_DIR/pgsquash"*

    # Remove directories (with confirmation)
    if [[ "$INTERACTIVE_MODE" == "true" ]]; then
        read -p "Remove configuration and data directories? [y/N]: " choice
        case $choice in
            [yY]|[yY][eE][sS])
                rm -rf "$CONFIG_DIR" "$DATA_DIR"
                ;;
        esac
    fi

    log_success "pgsquash uninstalled"
}

# Main execution
main() {
    local command

    show_banner

    # Parse arguments
    command=$(parse_args "$@")

    log_info "🔧 Configuration:"
    log_info "  ► Command: $command"
    log_info "  ► Install Mode: $INSTALL_MODE"
    log_info "  ► Install Dir: $INSTALL_DIR"
    log_info "  ► Config Dir: $CONFIG_DIR"
    log_info "  ► Data Dir: $DATA_DIR"
    echo

    # Execute command
    case "$command" in
        "install")
            interactive_config
            detect_install_mode
            install_system_deps
            create_directories

            case "$INSTALL_MODE" in
                "docker")
                    setup_docker_compose
                    pull_docker_images
                    start_services
                    ;;
                "native")
                    install_native
                    ;;
                "dev")
                    setup_development
                    ;;
            esac

            create_wrappers
            generate_sample_data
            setup_shell_integration
            run_verification
            show_summary
            ;;

        "uninstall")
            uninstall_pgsquash
            ;;

        "update")
            # Re-run install with existing configuration
            INTERACTIVE_MODE="false"
            main install
            ;;

        "test")
            run_verification
            ;;

        *)
            log_error "Unknown command: $command"
            ;;
    esac
}

# Trap handler
cleanup() {
    log_info "🧹 Cleaning up..."
}

trap cleanup EXIT

# Execute main function
main "$@"
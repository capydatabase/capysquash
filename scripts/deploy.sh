#!/bin/bash

# pgsquash Deployment Script
# This script handles the complete deployment of the integrated system

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
ENV_FILE="${PROJECT_ROOT}/.env.production"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        exit 1
    fi

    # Check Docker Compose
    if ! command -v docker compose &> /dev/null; then
        log_error "Docker Compose is not installed"
        exit 1
    fi

    # Check Go binary
    if [ ! -f "${PROJECT_ROOT}/pgsquash" ]; then
        log_warning "Go binary not found, building..."
        cd "${PROJECT_ROOT}"
        go build -o pgsquash cmd/pgsquash/main.go
        log_success "Go binary built"
    fi

    log_success "Prerequisites check completed"
}

# Load environment variables
load_env() {
    log_info "Loading environment configuration..."

    if [ -f "$ENV_FILE" ]; then
        export $(cat "$ENV_FILE" | grep -v '#' | xargs)
        log_success "Environment loaded from $ENV_FILE"
    else
        log_warning "Production environment file not found at $ENV_FILE"
        log_info "Using development configuration..."
        if [ -f "${PROJECT_ROOT}/web-app/.env.local" ]; then
            export $(cat "${PROJECT_ROOT}/web-app/.env.local" | grep -v '#' | xargs)
        fi
    fi
}

# Build and deploy
deploy() {
    log_info "Starting deployment..."

    cd "$PROJECT_ROOT"

    # Build and start services
    log_info "Building and starting services..."
    # Pass environment file to Docker Compose
    if [ -f "$ENV_FILE" ]; then
        docker compose -f docker-compose.webapp.yml --env-file "$ENV_FILE" up --build -d
    else
        docker compose -f docker-compose.webapp.yml up --build -d
    fi

    # Wait for services to be healthy
    log_info "Waiting for services to be ready..."
    sleep 10

    # Run database migrations
    log_info "Running database migrations..."
    if [ -n "$DATABASE_URL" ]; then
        cd web-app
        # Use Docker to run migrations with correct environment
        docker compose -f ../docker-compose.webapp.yml exec webapp npx drizzle-kit push
        cd "$PROJECT_ROOT"
    else
        log_warning "DATABASE_URL not set, skipping migrations"
    fi

    # Health check
    log_info "Performing health checks..."
    if curl -f http://localhost:3000 > /dev/null 2>&1; then
        log_success "Web application is running"
    else
        log_error "Web application health check failed"
        exit 1
    fi

    log_success "Deployment completed successfully!"
    log_info "Access your application at: http://localhost:3000"

    # Show service status
    docker compose -f docker-compose.webapp.yml ps
}

# Cleanup function
cleanup() {
    log_info "Cleaning up old deployments..."
    docker compose -f docker-compose.webapp.yml down --remove-orphans
    docker system prune -f
    log_success "Cleanup completed"
}

# Show logs
show_logs() {
    log_info "Showing application logs..."
    docker compose -f docker-compose.webapp.yml logs -f webapp
}

# Stop deployment
stop() {
    log_info "Stopping services..."
    docker compose -f docker-compose.webapp.yml down
    log_success "Services stopped"
}

# Main script logic
case "${1:-deploy}" in
    "deploy")
        check_prerequisites
        load_env
        deploy
        ;;
    "cleanup")
        cleanup
        ;;
    "logs")
        show_logs
        ;;
    "stop")
        stop
        ;;
    "restart")
        stop
        sleep 5
        check_prerequisites
        load_env
        deploy
        ;;
    *)
        echo "Usage: $0 {deploy|cleanup|logs|stop|restart}"
        echo ""
        echo "Commands:"
        echo "  deploy   - Deploy the complete system (default)"
        echo "  cleanup  - Clean up old deployments and images"
        echo "  logs     - Show application logs"
        echo "  stop     - Stop all services"
        echo "  restart  - Stop and restart services"
        exit 1
        ;;
esac
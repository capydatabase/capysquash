# pgsquash API Server

**⚠️ IMPORTANT: This API server has been moved to a separate repository**

The HTTP API server is now maintained as an independent module for better modularity and independent versioning.

## 👉 New Location

**[capysquash-api](https://github.com/CAPYSQUASH/capysquash-api)**

All API server code, documentation, and deployment guides are now in the dedicated repository above.

---

## Why the Move?

The API server was separated from the engine for several strategic reasons:

- **Better Modularity**: Clean separation between core library and HTTP API
- **Independent Versioning**: API can evolve independently from the engine
- **Easier Deployment**: Simplified Docker images and deployment processes
- **Clearer Responsibilities**: Engine focuses on consolidation logic, API on HTTP layer

---

## Quick Links

### 📖 Documentation
- **Main README**: https://github.com/CAPYSQUASH/capysquash-api
- **API Reference**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/API.md
- **GitHub Integration**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/GITHUB.md

### 🚀 Deployment
- **Docker Guide**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/DOCKER_DEPLOYMENT.md
- **Quick Start**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/QUICK_START.md
- **Security Best Practices**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/SECURITY.md

### 🔧 Development
- **Repository**: https://github.com/CAPYSQUASH/capysquash-api
- **Issues**: https://github.com/CAPYSQUASH/capysquash-api/issues
- **Contributing**: https://github.com/CAPYSQUASH/capysquash-api/blob/main/CONTRIBUTING.md

---

## Quick Migration Guide

If you were using the old API server location:

### Old Way (Deprecated)
```bash
cd pgsquash-engine/docker/api-server
docker compose up
```

### New Way (Current)
```bash
# Clone the API server repository
git clone https://github.com/CAPYSQUASH/capysquash-api
cd capysquash-api

# Follow the README for setup
docker compose up
```

---

## The CAPYSQUASH Ecosystem

```
┌─────────────────────────────────────┐
│  CAPYSQUASH Platform                │  ← Web application
│  https://capysquash.dev             │
└──────────┬──────────────────────────┘
           │
           ├── HTTP API
           │
┌──────────▼──────────┐
│   capysquash-api    │  ← Separate repository (NEW LOCATION)
│   REST API Server   │
│   - JWT auth        │
│   - GitHub webhooks │
│   - AI analysis     │
└──────────┬──────────┘
           │
           ├── Uses pkg/ APIs
           │
┌──────────▼──────────┐
│  pgsquash-engine    │  ← This repository
│  - SQL parsing      │
│  - Consolidation    │
│  - Validation       │
└─────────────────────┘
```

---

## Historical Note

This directory previously contained the API server implementation (`cmd/api-server`).

**Migration Date**: October 29, 2025
**Reason**: Modularization for better maintainability and independent versioning

All functionality has been preserved and enhanced in the new location.

---

## Need Help?

- **API Server Issues**: https://github.com/CAPYSQUASH/capysquash-api/issues
- **Engine Issues**: https://github.com/CAPYSQUASH/pgsquash-engine/issues
- **General Support**: support@capysquash.dev


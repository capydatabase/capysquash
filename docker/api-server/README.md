# pgsquash API Server

This directory does not contain the maintained HTTP API server implementation.
Use `capysquash-api` instead.

## 👉 New Location

**[capysquash-api](https://github.com/CAPYSQUASH/capysquash-api)**

All API server code, documentation, and deployment guides are now in the dedicated repository above.

---

## Current Status

The API server lives in a separate repository:

- `pgsquash-engine` contains the parser, consolidation logic, and validation code.
- `capysquash-api` contains the HTTP API server, deployment files, and API-focused documentation.
- This directory is kept only as a pointer to the current repository layout.

---

## Quick Links

### 📖 Documentation

- **Main README**: <https://github.com/CAPYSQUASH/capysquash-api>
- **API Reference**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/API.md>
- **GitHub Integration**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/GITHUB.md>

### 🚀 Deployment

- **Docker Guide**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/DOCKER_DEPLOYMENT.md>
- **Quick Start**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/QUICK_START.md>
- **Security Best Practices**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/SECURITY.md>

### 🔧 Development

- **Repository**: <https://github.com/CAPYSQUASH/capysquash-api>
- **Issues**: <https://github.com/CAPYSQUASH/capysquash-api/issues>
- **Contributing**: <https://github.com/CAPYSQUASH/capysquash-api/blob/main/CONTRIBUTING.md>

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

## Migration Note

Before October 29, 2025, the API server lived in-repo at `cmd/api-server`.

**Migration Date**: October 29, 2025
**Current Location**: `capysquash-api`

---

## Need Help?

- **API Server Issues**: <https://github.com/CAPYSQUASH/capysquash-api/issues>
- **Engine Issues**: <https://github.com/capysquash/pgsquash-engine/issues>
- **General Support**: <support@capysquash.dev>

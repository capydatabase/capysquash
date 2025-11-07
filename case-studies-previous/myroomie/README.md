# MyRoomie v3 - Living as a Service Platform

<div align="center">

![MyRoomie Logo](public/images/logo.png)

**Home Happens Here** 🏠

[![Next.js](https://img.shields.io/badge/Next.js-15+-black?style=flat-square&logo=next.js)](https://nextjs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5+-blue?style=flat-square&logo=typescript)](https://www.typescriptlang.org/)
[![Supabase](https://img.shields.io/badge/Supabase-PostgreSQL-green?style=flat-square&logo=supabase)](https://supabase.com/)
[![Vercel](https://img.shields.io/badge/Vercel-Deployed-black?style=flat-square&logo=vercel)](https://vercel.com/)

Next-generation Living as a Service (LaaS) platform connecting roommates and communities across Europe with AI-powered matching, property search, and enterprise relocation services.

[Live Demo](https://new.myroomieapp.com) • [Documentation](./docs) • [API Docs](#api-documentation)

</div>

---

## 📋 Table of Contents

- [Overview](#-overview)
- [Business Model](#-business-model)
- [Key Features](#-key-features)
- [Tech Stack](#-tech-stack)
- [Architecture](#-architecture)
- [Getting Started](#-getting-started)
- [Development](#-development)
- [Deployment](#-deployment)
- [Project Structure](#-project-structure)
- [Contributing](#-contributing)
- [License](#-license)

---

## 🌟 Overview

MyRoomie is a comprehensive **Living as a Service (LaaS)** platform that revolutionizes how people find homes and roommates across Europe. Built with modern web technologies, MyRoomie provides:

- 🤖 **AI-Powered Matching** - 75% compatibility accuracy using MBTI, lifestyle preferences, and behavioral patterns
- 🏘️ **Cross-Country Property Search** - Find homes across 6+ European markets with Google Places v2 integration
- 💬 **Real-Time Messaging** - Connect with potential roommates instantly
- 🏢 **B2B Property Management** - Professional tools for landlords and property managers
- 🌍 **Enterprise Relocation** - Corporate housing coordination for global companies
- 🎓 **Student Housing Ecosystem** - University partnerships and student-specific features

### Markets

**Launch Markets** (2025):
- 🇬🇷 Greece
- 🇵🇱 Poland
- 🇦🇹 Austria
- 🇩🇪 Germany
- 🇨🇿 Czech Republic
- 🇳🇱 Netherlands

**Expansion Markets** (2026+): Belgium, France, Spain, Italy, Portugal

---

## 💰 Business Model

MyRoomie operates on a **three-pillar revenue model**:

### Pillar 1: MyRoomie Living (B2C) - €7/month

**Target**: 1.2M users by 2031 | 12% conversion rate

**Tiers**:
- **Free Essentials**: 1 listing, 5 messages/day, basic matching
- **Plus Boost**: €7.99 one-time - 3 listings, verified badge, 30-day duration
- **Plus Pro**: €5.99/month - Unlimited messaging, advanced AI matching, priority access

**Revenue Streams**:
- Premium subscriptions (€7/month average)
- Profile boosts (€2.99-9.99/week)
- Featured listings
- Priority placement

### Pillar 2: MyRoomie Manage (B2B) - €500/year/property

**Target**: 7,500 properties by 2031

**Features**:
- Bulk property import and management
- Professional analytics dashboards
- Tenant screening tools
- Maintenance request management
- Automated marketing

**Revenue**: €500/year per property managed

### Pillar 3: MyRoomie Pro (Enterprise) - €300-500/relocation

**Target**: 240 enterprise clients by 2031

**Services**:
- Corporate relocation management
- Employee housing coordination
- White-label solutions
- API access for HR systems
- Dedicated account management

**Revenue**: €300-500 per relocation + annual contracts

### Projected Revenue (2031)
- **B2C**: €10.1M annually (144,000 premium users @ €7/month)
- **B2B**: €3.75M annually (7,500 properties @ €500/year)
- **Enterprise**: €2.4M annually (240 clients @ €10K/year average)
- **Total**: €16.25M ARR by 2031

---

## ✨ Key Features

### 🎯 AI-Powered Roommate Matching

- **75% compatibility accuracy** using multi-factor analysis
- MBTI personality assessment integration
- Lifestyle preference matching (sleep schedule, cleanliness, social habits)
- Dynamic compatibility scores with real-time updates
- AI chat assistant (Rumi) for personalized recommendations

**Tech**: Vercel AI SDK, Azure OpenAI, Custom matching algorithms

### 🔍 Advanced Property Search

- **Cross-country search** with Google Places API v2
- Smart regional bias for local results
- Comprehensive location analysis (transit, air quality, amenities)
- Map-based search with real-time filters
- Saved searches with instant notifications

**Tech**: Google Maps Platform (Places v2, Maps JS, Geocoding)

### 💬 Real-Time Communication

- Unified messaging system with conversations and chat
- Typing indicators and read receipts
- File sharing and media attachments
- In-app notifications
- Message encryption

**Tech**: Supabase Realtime, PostgreSQL pub/sub

### 🏢 Property Management Tools

- Bulk property import/export (CSV, Excel)
- Portfolio analytics and performance tracking
- Tenant screening and background checks
- Maintenance request workflows
- Automated rent collection reminders

### 🎓 Student Housing

- University verification system
- Campus-specific listings
- Student discount pricing
- Academic year lease matching
- Roommate matching by major/year

### 🌍 Enterprise Features

- Corporate relocation workflow management
- Employee onboarding coordination
- Multi-country housing search
- Integration with HR systems (webhooks, API)
- White-label solutions

### 🔐 Security & Trust

- Clerk authentication with JWT
- Supabase Row-Level Security (RLS)
- Identity verification (ID, student status)
- Profile verification badges
- Scam detection algorithms
- GDPR compliance

---

## 🛠️ Tech Stack

### Frontend

| Technology | Version | Purpose |
|------------|---------|---------|
| **Next.js** | 15+ (canary) | React framework with App Router |
| **React** | 19+ (next) | UI library |
| **TypeScript** | 5+ (next) | Type safety |
| **Tailwind CSS** | 4.1+ | Styling framework |
| **shadcn/ui** | 2.8+ | Component library (Radix UI) |
| **Framer Motion** | 12+ | Animations |
| **React Hook Form** | 7.65+ | Form handling |
| **Zod** | 4.1+ | Schema validation |
| **Zustand** | 5+ | State management |

### Backend

| Technology | Version | Purpose |
|------------|---------|---------|
| **Supabase** | Latest | PostgreSQL database + auth |
| **PostgreSQL** | 17+ | Primary database |
| **Clerk** | 6.34+ | Authentication platform |
| **Stripe** | 19+ | Payment processing |
| **Vercel AI SDK** | beta | AI integration |
| **Azure OpenAI** | Latest | AI models |
| **Anthropic Claude** | Latest | AI models |

### Infrastructure

| Technology | Purpose |
|------------|---------|
| **Vercel** | Hosting and deployment |
| **Supabase** | Database, auth, storage, realtime |
| **Google Cloud** | Maps Platform (Places v2, Maps JS, Geocoding) |
| **Stripe** | Payment processing and subscriptions |
| **PostHog** | Analytics and feature flags |
| **Biome** | Code linting and formatting |
| **Vitest** | Testing framework |

### Key Libraries

```json
{
  "@ai-sdk/anthropic": "beta",
  "@clerk/nextjs": "^6.34.0",
  "@supabase/supabase-js": "2.76.1",
  "@radix-ui/react-*": "latest",
  "@tanstack/react-query": "5.90.5",
  "stripe": "^19.1.0",
  "date-fns": "^4.1.0",
  "lucide-react": "^0.548.0",
  "next-intl": "^4.4.0"
}
```

---

## 🏗️ Architecture

### Three-Tier Authentication System

```
┌─────────────────────────────────────────────────────────┐
│                    Client Browser                        │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Next.js App Router (React Server Components)    │  │
│  │  - Client Components use useSupabaseClient()     │  │
│  │  - Server Components use createServerClient()    │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────┐
│                   Clerk Auth Layer                       │
│  ┌───────────────────────────────────────────────────┐  │
│  │  JWT Token Generation & Session Management       │  │
│  │  - User authentication                           │  │
│  │  - Session claims (metadata, email)              │  │
│  │  - Role-based access control                     │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────┐
│                 Middleware Layer                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │  Route Protection & Role Validation              │  │
│  │  - Protected routes (dashboard, messages, etc.)  │  │
│  │  - Admin routes (/admin/*)                       │  │
│  │  - Public API routes (properties, places)        │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────┐
│              Supabase RLS Security Layer                 │
│  ┌───────────────────────────────────────────────────┐  │
│  │  PostgreSQL Row-Level Security                   │  │
│  │  - Validates Clerk JWT tokens                    │  │
│  │  - Enforces data access policies                 │  │
│  │  - User-scoped queries                           │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Database Architecture

**Schema Conventions**:
- Database: `snake_case` (e.g., `user_id`, `created_at`, `furnishing_status`)
- TypeScript: `camelCase` (e.g., `userId`, `createdAt`, `furnishingStatus`)

**Key Tables**:
- `profiles` - User profiles (linked to Clerk via webhook)
- `properties` - Property listings
- `rooms` - Individual rooms within properties
- `matches` - Roommate compatibility matches
- `compatibility_scores` - Precomputed compatibility matrix
- `conversations` + `messages` - Unified messaging system
- `user_subscriptions` - Subscription management
- `subscription_plans` - Plan configurations
- `ai_models` - Database-driven AI platform
- `enterprise_relocations` - Corporate relocation tracking

**Migrations**: 50+ sequential migrations in `supabase/migrations/`

### API Architecture

**Route Structure**:
```
/api
├── properties/          # Property CRUD and search
├── matches/            # Roommate matching
├── messages/           # Real-time messaging
├── places/             # Google Maps integration
├── subscriptions/      # Stripe subscription management
├── ai-chat/            # AI assistant
├── admin/              # Admin-only endpoints
├── enterprise/         # Enterprise features
└── webhooks/           # External service webhooks
```

**Patterns**:
- All API routes use `createServerClient()` for authenticated requests
- Zod validation on request bodies
- Consistent error responses (400/401/403/404/500)
- Database column names in snake_case

### AI Platform Architecture

```
┌─────────────────────────────────────────────────────────┐
│                 AI Configuration (DB)                    │
│  - ai_models: Provider, costs, tier access              │
│  - ai_routing_rules: Dynamic routing logic              │
│  - ai_config: Temperature, max_tokens settings          │
│  - ai_usage_tracking: Per-user cost tracking            │
└─────────────────────────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────┐
│              Dynamic Model Selection                     │
│  Free users    → Cheap models (Claude Haiku)            │
│  Premium users → Smart models (Claude Sonnet)           │
│  Complex queries → Reasoning models (o1-mini)           │
└─────────────────────────────────────────────────────────┘
                           ▼
┌─────────────────────────────────────────────────────────┐
│                  Tool Execution                          │
│  - searchProperties: Real-time property search          │
│  - findRoommates: Compatibility matching                │
│  - getLocationInfo: Google Places integration           │
└─────────────────────────────────────────────────────────┘
```

**Subscription Enforcement**: 3 messages/day for free users, unlimited for premium

---

## 🚀 Getting Started

### Prerequisites

- **Node.js**: 22.x or higher
- **pnpm**: 8.x or higher (preferred) or bun
- **Docker**: For local Supabase development
- **Supabase CLI**: For database migrations

### Environment Setup

1. **Clone the repository**

```bash
git clone https://github.com/idominikosgr/myroomiev3.git
cd myroomiev3
```

2. **Install dependencies**

```bash
pnpm install
```

3. **Set up environment variables**

Copy `.env.example` to `.env.local` and configure:

```bash
# Clerk Authentication
NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_...
CLERK_SECRET_KEY=sk_test_...
ADMIN_EMAILS=admin@example.com

# Supabase
NEXT_PUBLIC_SUPABASE_URL=https://your-project.supabase.co
NEXT_PUBLIC_SUPABASE_ANON_KEY=eyJ...
SUPABASE_SERVICE_ROLE_KEY=eyJ...

# Stripe
NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY=pk_test_...
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# Google Maps
NEXT_PUBLIC_GOOGLE_MAPS_API_KEY=AIza...

# AI Providers
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
AZURE_OPENAI_API_KEY=...  # Optional
```

4. **Set up local Supabase**

```bash
# Start Supabase Docker containers
supabase start

# Run migrations
supabase db reset --experimental

# Generate TypeScript types
pnpm types:local
```

5. **Start development server**

```bash
pnpm dev
# or with Turbopack (faster)
pnpm dev:turbopack
```

6. **Open browser**

Navigate to [http://localhost:3000](http://localhost:3000)

---

## 💻 Development

### Available Scripts

```bash
# Development
pnpm dev                    # Start dev server
pnpm dev:turbopack         # Start with Turbopack (faster)
pnpm build                 # Production build
pnpm start                 # Start production server

# Code Quality
pnpm check                 # Lint + format check
pnpm check:fix            # Auto-fix issues
pnpm lint                 # Run Biome linter
pnpm lint:fix            # Fix linting issues
pnpm format              # Format code
pnpm typecheck           # TypeScript type check

# Testing
pnpm test:integration        # Integration tests with local Supabase
pnpm test:integration:watch  # Watch mode
pnpm test:browser           # Browser tests
pnpm test:ci               # CI pipeline tests

# Database
pnpm types:local           # Generate types from local Supabase
pnpm types:remote          # Generate types from production
supabase db reset --experimental  # Reset local database
supabase migration new <name>     # Create new migration

# Validation
pnpm validate             # Types + lint + format
pnpm validate:full        # Full validation + tests + build
```

### Code Style

**Formatter**: Biome (NOT Prettier/ESLint)

**Rules** (configured in `biome.json`):
- 2-space indentation
- Single quotes for strings
- Semicolons: ASNeeded
- Max line width: 100 characters
- Import sorting: automatic

**Example**:
```typescript
// ✅ Good
import { createServerClient } from '@/lib/supabase/server'
import type { Database } from '@/types/database.types'

export async function GET(request: NextRequest) {
  const supabase = await createServerClient()
  const { data } = await supabase.from('properties').select('*')
  return NextResponse.json({ data })
}

// ❌ Bad - double quotes, missing types
import { createServerClient } from "@/lib/supabase/server";

export async function GET(request) {
  // ...
}
```

### Database Development

**Migration Workflow**:

1. Make schema changes in new migration file:
```bash
supabase migration new add_feature_column
```

2. Write SQL in `supabase/migrations/XX_migration_name.sql`

3. Test locally:
```bash
supabase db reset --experimental
pnpm dev
```

4. Generate types:
```bash
pnpm types:local
pnpm typecheck
```

5. Commit and deploy

**Important**: Never edit existing migrations in production. Always create new migrations.

### Testing

**Integration Tests** (Preferred):

```typescript
// src/test/integration/properties.test.ts
import { describe, it, expect, beforeAll } from 'vitest'
import { createServerClient } from '@/lib/supabase/server'

describe('Property API', () => {
  let supabase: SupabaseClient

  beforeAll(async () => {
    supabase = await createServerClient(false)
  })

  it('should fetch properties', async () => {
    const { data, error } = await supabase
      .from('properties')
      .select('*')
      .limit(10)

    expect(error).toBeNull()
    expect(data).toHaveLength(10)
  })
})
```

**Requirements**:
- Docker Supabase must be running (`supabase start`)
- Use `createServerClient(false)` for unauthenticated tests
- Test files in `src/test/integration/`

---

## 🌐 Deployment

### Vercel Deployment

**Automatic**:
- Push to `main` branch triggers production deployment
- Push to `ai` branch triggers preview deployment

**Manual**:
```bash
vercel --prod
```

### Environment Variables

Configure in Vercel dashboard:
- All variables from `.env.local`
- Set `NEXT_PUBLIC_APP_URL` to production domain
- Configure Stripe webhook secret from Stripe dashboard

### Database Migrations

**Production migrations**:

1. Test locally first
2. Backup production database
3. Apply via Supabase dashboard or CLI:
```bash
supabase db push --linked
```

### Vercel Configuration

Key settings in `vercel.json`:
- Build command: `pnpm run build`
- Framework: Next.js
- Node version: 22.x
- Cron jobs for compatibility recalculation

---

## 📁 Project Structure

```
myroomiev3/
├── .github/                      # GitHub configuration
│   └── copilot-instructions.md   # AI agent instructions
├── docs/                         # Documentation
│   ├── ai-chat/                  # AI implementation docs
│   ├── audit/                    # Comprehensive audits
│   ├── architecture/             # Architecture diagrams
│   └── *.md                      # Feature documentation
├── public/                       # Static assets
│   ├── icons/                    # App icons
│   └── images/                   # Images
├── src/
│   ├── app/                      # Next.js App Router
│   │   ├── (auth)/              # Auth-related routes
│   │   ├── (dashboard)/         # Protected dashboard routes
│   │   ├── (manage)/            # Property management routes
│   │   ├── (public)/            # Public marketing pages
│   │   ├── admin/               # Admin-only routes
│   │   ├── api/                 # API routes
│   │   │   ├── properties/      # Property endpoints
│   │   │   ├── matches/         # Matching endpoints
│   │   │   ├── messages/        # Messaging endpoints
│   │   │   ├── places/          # Google Maps endpoints
│   │   │   ├── subscriptions/   # Stripe endpoints
│   │   │   ├── ai-chat/         # AI assistant
│   │   │   └── webhooks/        # External webhooks
│   │   ├── globals.css          # Global styles
│   │   ├── layout.tsx           # Root layout
│   │   └── page.tsx             # Landing page
│   ├── components/              # React components
│   │   ├── ui/                  # shadcn/ui components
│   │   ├── properties/          # Property components
│   │   ├── matching/            # Matching components
│   │   ├── chat/                # Messaging components
│   │   ├── admin/               # Admin components
│   │   └── ...                  # Domain-specific components
│   ├── hooks/                   # Custom React hooks
│   │   ├── use-properties.ts
│   │   ├── use-matches.ts
│   │   └── use-realtime-messages.ts
│   ├── lib/                     # Utility libraries
│   │   ├── supabase/           # Supabase clients
│   │   │   ├── client.ts        # Client-side singleton
│   │   │   └── server.ts        # Server-side client
│   │   ├── ai/                  # AI integration
│   │   ├── matching/            # Matching algorithms
│   │   ├── stripe/              # Stripe integration
│   │   ├── google-places-v2.ts  # Google Maps
│   │   └── utils.ts             # Utilities
│   ├── stores/                  # Zustand stores
│   ├── test/                    # Test files
│   │   ├── integration/         # Integration tests
│   │   └── unit/                # Unit tests
│   └── types/                   # TypeScript types
│       └── database.types.ts    # Generated DB types
├── supabase/
│   ├── migrations/              # 50+ sequential migrations
│   └── functions/               # Edge functions
├── biome.json                   # Biome configuration
├── next.config.ts               # Next.js configuration
├── package.json                 # Dependencies
├── tailwind.config.ts           # Tailwind configuration
├── tsconfig.json                # TypeScript configuration
└── vitest.integration.config.ts # Test configuration
```

---

## 📊 Key Metrics & KPIs

### Platform Metrics (2025 Targets)

- **Active Users**: 120K (10% of 1.2M goal)
- **Premium Conversion**: 4.7% → 12% by 2031
- **Properties Managed**: 250 → 7,500 by 2031
- **Enterprise Clients**: 13 → 240 by 2031
- **AI Matching Accuracy**: 75%+
- **Average Response Time**: <500ms
- **Mobile Usage**: 68%

### Technical Metrics

- **Lighthouse Score**: 95+ (Performance, Accessibility, Best Practices, SEO)
- **Core Web Vitals**:
  - LCP: <2.5s
  - FID: <100ms
  - CLS: <0.1
- **Uptime**: 99.9%
- **Test Coverage**: 70%+ (target)

---

## 🤝 Contributing

We welcome contributions! Please follow these guidelines:

1. **Fork the repository**
2. **Create a feature branch** (`git checkout -b feature/amazing-feature`)
3. **Follow code style** (run `pnpm check:fix` before committing)
4. **Write tests** for new features
5. **Commit with conventional commits** (`feat:`, `fix:`, `docs:`, etc.)
6. **Push to your branch** (`git push origin feature/amazing-feature`)
7. **Open a Pull Request**

### Commit Convention

```
feat: Add AI-powered property recommendations
fix: Resolve subscription enforcement bug
docs: Update API documentation
style: Format code with Biome
refactor: Consolidate matching algorithms
test: Add integration tests for messaging
chore: Update dependencies
```

### Development Philosophy

**Athens to Crete Paradigm**: Don't build roads across seas. Don't invent shipcopters. Use boats.

- Understand the actual problem before implementing
- Map constraints and requirements
- Identify existing solutions to leverage
- Ensure complete, logical paths from start to finish
- Syntactic correctness ≠ semantic coherence

**Never** create files/interfaces with prefixes like "enhanced", "improved", "new", "v2" - always refactor existing code.

---

## 📚 Documentation

- **[AI Agent Instructions](.github/copilot-instructions.md)** - Complete guide for AI coding agents
- **[Comprehensive Audit](docs/audit/00_COMPREHENSIVE_AUDIT_REPORT.md)** - Full platform audit with findings
- **[Business Domains](MyRoomie%20Business%20Domains%20and%20File%20Mapping.md)** - Domain mapping
- **[Persona Guide](docs/myroomie_complete_persona_guide.md)** - User personas and journeys
- **[AI Implementation](docs/ai-chat/AI_IMPLEMENTATION_ROADMAP_V2.md)** - AI platform roadmap
- **[Google Maps Integration](docs/GOOGLE_PLACES_V2_IMPLEMENTATION.md)** - Places API v2 docs
- **[Search Architecture](docs/MYROOMIE_SEARCH_ARCHITECTURE.md)** - Cross-country search

---

## 🐛 Known Issues & Roadmap

### Critical Issues (Fixed in Q4 2025)
- ✅ Subscription webhook column name mismatches
- ✅ Property search wrong column names
- ✅ Messaging schema consolidated
- ✅ Compatibility scores population

### In Progress
- 🔄 AI recommendation engine
- 🔄 Read receipts for messaging
- 🔄 Advanced analytics dashboards
- 🔄 Mobile app (PWA → Native)

### Roadmap (2026)
- International expansion (5 new markets)
- White-label enterprise solutions
- Advanced AI features (voice, video analysis)
- Blockchain-based identity verification
- Carbon-neutral housing initiatives

---

## 📄 License

This project is proprietary and confidential. All rights reserved.

© 2025 MyRoomie. Unauthorized copying, distribution, or use is strictly prohibited.

---

## 🙏 Acknowledgments

Built with ❤️ by the MyRoomie team

**Technologies**: Next.js, React, TypeScript, Supabase, Clerk, Stripe, Vercel AI SDK, Google Maps Platform

**Special Thanks**: To all our early users, beta testers, and partners across Europe

---

## 📞 Contact

- **Website**: [myroomieapp.com](https://myroomieapp.com)
- **Email**: hello@myroomieapp.com
- **Support**: support@myroomieapp.com
- **Twitter**: [@myroomieapp](https://twitter.com/myroomieapp)
- **LinkedIn**: [MyRoomie](https://linkedin.com/company/myroomie)

---

<div align="center">

**Home Happens Here** 🏠

Made with Next.js, deployed on Vercel, powered by Supabase

</div>

# VDK Hub

<div align="center">

![VDK Hub Logo](public/images/logo.png)

**Discover, share, and collaborate on AI context blueprints**

Web platform for browsing 109 expert-curated blueprints, creating custom packages, and sharing configurations across teams. Built on the Universal AI Context Schema v2.1.0 standard.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![VDK Standards](https://img.shields.io/badge/VDK-v2.1.0-success)](https://docs.vdk.tools)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.8-blue)](https://www.typescriptlang.org/)
[![Next.js](https://img.shields.io/badge/Next.js-15-black)](https://nextjs.org/)

[🚀 Live Demo](https://vdk.tools) • [📖 Documentation](https://wiki.vdk.tools/) • [💬 Community](https://github.com/entro314-labs/VDK-Hub/discussions) • [🐛 Report Bug](https://github.com/entro314-labs/VDK-Hub/issues)

</div>

---

## ✨ What is VDK Hub?

VDK Hub is a web platform for browsing 109 expert-curated AI assistant blueprints, creating custom packages, and sharing configurations across teams. Browse with smart search and filtering, generate custom packages with the 7-step wizard, and collaborate through team collections.

### Why Use VDK Hub?

- **🎯 Project-Aware AI**: Make your AI assistant understand your specific tech stack and coding standards
- **🏆 Curated Quality**: Access professionally crafted rules tested by the community
- **⚡ Time-Saving**: No need to write AI rules from scratch or configure from zero
- **🌐 Multi-Platform**: Works with Claude Code, Cursor, Windsurf, GitHub Copilot, and 20+ AI platforms
- **⚡ Smart Limits**: Platform-specific character limits with intelligent truncation (4K-8K range)
- **👥 Team Ready**: Share configurations across teams for consistent development experience
- **🔗 VDK Ecosystem**: Full compliance with VDK CLI and universal blueprint standards

## 🚀 Key Features

- **📚 Blueprint Catalog**: Browse our collection of AI assistant blueprints organized by category
- **🔍 Smart Search**: Find exactly what you need with advanced filtering and search
- **⚙️ Blueprint Generator**: Create custom packages tailored to your tech stack
- **👤 Personal Collections**: Save and organize your favorite blueprints
- **📦 One-Click Download**: Get ready-to-use configuration packages
- **🔄 GitHub Integration**: Automatic synchronization with blueprint repositories
- **🌓 Modern Interface**: Clean, responsive design with dark/light themes
- **👥 Team Collaboration**: Share blueprints and standardize across teams
- **🔧 VDK CLI Integration**: Seamless sync with VDK CLI for local development
- **📐 Smart Truncation**: Preserves content structure while meeting platform constraints
- **🌟 Community Blueprints**: User-generated content with voting and trending systems

## 🎬 Quick Demo

```bash
# 1. Browse blueprints at https://vdk.tools
# 2. Use the generator to create a custom package
# 3. Download and integrate with your AI assistant
# 4. Enjoy project-aware AI with automatic platform optimization!

# Or use with VDK CLI for seamless integration:
vdk init                    # Auto-detect your project
vdk deploy hub:blueprint-id # Deploy from VDK Hub
```

**Before VDK Hub:**

```
You: "Create a React component"
AI: Generic component with basic patterns
```

**After VDK Hub:**

```
You: "Create a React component"
AI: TypeScript component with your team's patterns,
    proper styling, accessibility, best practices,
    optimized for your specific framework (Next.js/Vite),
    and automatically formatted for your platform limits
```

## 🛠 Technology Stack

### Frontend

- **Toolkit**: [Next.js 15](https://nextjs.org/) with App Router
- **UI Library**: [React 19](https://react.dev/)
- **Components**: [shadcn/ui](https://ui.shadcn.com/) with [Radix UI](https://www.radix-ui.com/) primitives
- **Styling**: [Tailwind CSS 4](https://tailwindcss.com/)
- **State Management**: React Context API + [React Hook Form](https://react-hook-form.com/)
- **Type Safety**: [TypeScript 5.8](https://www.typescriptlang.org/)

### Backend & Database

- **Backend**: [Supabase](https://supabase.io/) (PostgreSQL, Authentication, Storage)
- **ORM**: Supabase Client with generated TypeScript types
- **Authentication**: Supabase Auth with GitHub OAuth support
- **File Processing**: Gray Matter for frontmatter parsing

### Development & Build Tools

- **Package Manager**: [pnpm](https://pnpm.io/)
- **Build Tool**: Next.js with custom webpack configuration
- **Testing**: [Vitest](https://vitest.dev/) + [React Testing Library](https://testing-library.com/)
- **Code Quality**: ESLint + TypeScript strict mode
- **Version Control**: Git with conventional commits

## 📁 Project Structure

```
├── app/                          # Next.js App Router
│   ├── (auth)/                  # Authentication pages group
│   ├── admin/                   # Admin dashboard pages
│   ├── api/                     # API routes
│   │   ├── admin/              # Admin API endpoints
│   │   ├── auth/               # Authentication endpoints
│   │   └── rules/              # Rule management APIs
│   ├── collections/            # User collections pages
│   ├── profile/                # User profile pages
│   ├── rules/                  # Rule browsing and display
│   │   ├── [category]/        # Category pages
│   │   └── r/                 # Rule redirect handling
│   ├── setup/                 # Configuration wizard
│   ├── globals.css            # Global styles and CSS variables
│   ├── layout.tsx             # Root layout component
│   └── page.tsx               # Homepage
├── components/                 # React components
│   ├── auth/                  # Authentication components
│   ├── collections/           # Collection management
│   ├── rules/                 # Rule-specific components
│   ├── search/                # Search functionality
│   ├── setup/                 # Rule Generator components
│   └── ui/                    # Reusable UI components (shadcn/ui)
├── lib/                       # Utilities and services
│   ├── actions/               # Server actions
│   ├── services/              # Business logic services
│   │   ├── github/           # GitHub integration
│   │   └── auth-service.ts   # Authentication service
│   ├── supabase/             # Supabase configuration
│   ├── error-handling.ts     # Error handling utilities
│   ├── types.ts              # TypeScript type definitions
│   └── utils.ts              # General utilities
├── scripts/                   # Build and maintenance scripts
├── public/                    # Static assets
├── .env.template             # Environment variables template
├── next.config.ts            # Next.js configuration
├── tailwind.config.ts        # Tailwind CSS configuration
└── tsconfig.json             # TypeScript configuration
```

## 🚦 Prerequisites

- **Node.js**: v18.0.0 or higher
- **pnpm**: v8.0.0 or higher (recommended) or npm/yarn
- **Supabase Account**: For database and authentication services
- **GitHub Account**: For OAuth authentication (optional)

## ⚡ Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/entro314-labs/VDK-Hub.git
cd VDK-Hub
```

### 2. Install Dependencies

```bash
pnpm install
```

### 3. Environment Setup

```bash
cp .env.template .env.local
```

Edit `.env.local` with your configuration:

```env
# Supabase Configuration
NEXT_PUBLIC_SUPABASE_URL=your_supabase_project_url
NEXT_PUBLIC_SUPABASE_ANON_KEY=your_supabase_anon_key
NEXT_SUPABASE_SERVICE_ROLE_KEY=your_supabase_service_role_key

# GitHub Integration (Optional)
GITHUB_TOKEN=your_github_token
GITHUB_REPO_OWNER=repository_owner
GITHUB_REPO_NAME=repository_name

# Webhook Security
GITHUB_WEBHOOK_SECRET=your_webhook_secret

# API Security
API_SECRET_KEY=your_api_secret_key
```

### 4. Database Setup

The application uses Supabase with VDK-compliant schema design:

- `categories` - Blueprint categories and hierarchies
- `blueprints` - Individual blueprint definitions with VDK metadata
- `blueprint_platforms` - Platform compatibility and character limits
- `blueprint_relationships` - Dependencies (requires/suggests/conflicts)
- `community_blueprints` - User-generated content with voting
- `profiles` - User profile information
- `collections` - User-created blueprint collections
- `sync_logs` - Synchronization operation history

### 5. Development Server

```bash
pnpm dev
```

Open <http://localhost:3000> to view the application.

## 📋 Available Scripts

### Development

```bash
pnpm dev              # Start development server
pnpm build            # Build for production
pnpm start            # Start production server
pnpm lint             # Run ESLint
```

### Database & Sync Operations

```bash
pnpm sync-rules       # Sync blueprints from filesystem to database
pnpm import-rules     # Import blueprints with progress tracking
pnpm quick-sync       # Quick synchronization utility
```

### Maintenance

```bash
pnpm test            # Run test suite
pnpm test:watch      # Run tests in watch mode
```

## 🔧 Configuration

### Environment Variables

| Variable                         | Description                  | Required |
| -------------------------------- | ---------------------------- | -------- |
| `NEXT_PUBLIC_SUPABASE_URL`       | Supabase project URL         | ✅        |
| `NEXT_PUBLIC_SUPABASE_ANON_KEY`  | Supabase anonymous key       | ✅        |
| `NEXT_SUPABASE_SERVICE_ROLE_KEY` | Supabase service role key    | ✅        |
| `GITHUB_TOKEN`                   | GitHub personal access token | ❌        |
| `GITHUB_WEBHOOK_SECRET`          | GitHub webhook secret        | ❌        |
| `API_SECRET_KEY`                 | API endpoint protection key  | ❌        |

### Supabase Setup

1. Create a new Supabase project
2. Run the database migrations (SQL files in `supabase/` directory)
3. Configure authentication providers (GitHub OAuth recommended)
4. Set up Row Level Security (RLS) policies
5. Enable realtime subscriptions for live updates

### GitHub Integration (Optional)

For automatic blueprint synchronization:

1. Create a GitHub personal access token with repo permissions
2. Set up webhook endpoint: `https://your-domain.com/api/webhooks/github`
3. Configure webhook to trigger on push events to main branch

## 🧪 Testing

### Running Tests

```bash
# Run all tests
pnpm test

# Run tests in watch mode
pnpm test:watch

# Run tests with coverage
pnpm test:coverage
```

### Testing Strategy

- **Unit Tests**: Component logic and utility functions
- **Integration Tests**: API endpoints and database operations
- **E2E Tests**: Critical user workflows (planned)

## 🚀 Deployment

### Vercel (Recommended)

1. Connect your GitHub repository to Vercel
2. Configure environment variables in Vercel dashboard
3. Deploy automatically on push to main branch

### Manual Deployment

```bash
pnpm build
pnpm start
```

### Docker Deployment

```dockerfile
# Dockerfile example
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
RUN npm run build
EXPOSE 3000
CMD ["npm", "start"]
```

## 🔍 API Overview

### VDK CLI Integration Endpoints

- `GET /api/cli/sync/blueprints` - CLI blueprint synchronization (v2.1.0)
- `POST /api/cli/generate` - Custom package generation for CLI
- `POST /api/cli/telemetry/*` - Usage analytics and error tracking
- `GET /api/health` - Hub health check for CLI connectivity

### Core Endpoints

#### Blueprint Management

- `GET /api/blueprints` - List blueprints with filtering and pagination
- `GET /api/blueprints/[category]` - Get blueprints by category
- `GET /api/blueprints/[category]/[blueprintId]` - Get specific blueprint
- `POST /api/blueprints/r` - Blueprint lookup and redirect

#### Community System

- `GET /api/community/blueprints` - Community-generated blueprints
- `POST /api/community/blueprints/[id]/vote` - Vote on community content
- `GET /api/community/blueprints/trending` - Trending community blueprints

#### Authentication

- `GET /api/auth/callback` - OAuth callback handler
- `POST /api/auth/logout` - User logout

#### Admin Operations

- `POST /api/admin/sync` - Trigger blueprint synchronization
- `GET /api/admin/sync-logs` - View synchronization history
- `POST /api/webhooks/github` - GitHub webhook handler

### Response Formats

#### VDK CLI Sync Response

```json
{
  "blueprints": [...],
  "lastSyncTime": "2025-01-11T10:00:00Z",
  "totalBlueprints": 108,
  "changes": {
    "added": [],
    "updated": [],
    "removed": []
  },
  "metadata": {
    "syncType": "incremental",
    "version": "2.1.0"
  }
}
```

#### Success Response

```json
{
  "data": {...},
  "pagination": {
    "page": 1,
    "pageSize": 20,
    "totalCount": 100,
    "totalPages": 5
  }
}
```

#### Error Response

```json
{
  "error": "Error message",
  "details": "Additional error details",
  "status": 400
}
```

## 🛡 Security Considerations

### Authentication & Authorization

- Supabase Auth with Row Level Security (RLS)
- GitHub OAuth integration
- Admin role-based access control
- API endpoint protection with secret keys

### Data Protection

- Input validation with Zod schemas
- SQL injection protection via Supabase ORM
- XSS protection through React's built-in sanitization
- Secure cookie handling for sessions

### API Security

- Rate limiting on public endpoints (planned)
- CORS configuration for API routes
- Webhook signature verification
- Environment variable protection

## 🤝 Contributing

### Development Workflow

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Make your changes following the coding standards
4. Add tests for new functionality
5. Run the test suite: `pnpm test`
6. Commit using conventional commits: `git commit -m 'feat: add amazing feature'`
7. Push to your branch: `git push origin feature/amazing-feature`
8. Open a Pull Request

### Coding Standards

- Follow TypeScript strict mode requirements
- Use Prettier for code formatting
- Follow ESLint rules
- Write tests for new features
- Document complex logic with comments
- Use semantic commit messages

### Code Review Process

- All changes require PR approval
- Automated tests must pass
- No TypeScript errors or ESLint warnings
- Performance impact assessment for large changes

## 🐛 Troubleshooting

### Common Issues

#### Supabase Connection Issues

```bash
# Check environment variables
echo $NEXT_PUBLIC_SUPABASE_URL
echo $NEXT_PUBLIC_SUPABASE_ANON_KEY

# Verify Supabase project status
# Check Supabase dashboard for any outages
```

#### Build Failures

```bash
# Clear Next.js cache
rm -rf .next

# Reinstall dependencies
rm -rf node_modules pnpm-lock.yaml
pnpm install
```

#### Authentication Problems

- Verify Supabase auth configuration
- Check GitHub OAuth app settings
- Ensure callback URLs are correctly configured
- Review browser console for auth errors

#### Blueprint Sync Issues

- Check GitHub token permissions
- Verify webhook endpoint configuration
- Review sync logs in admin dashboard
- Ensure blueprint files follow VDK universal format
- Check VDK CLI integration endpoints for connectivity
- Verify platform character limits aren't causing truncation issues

### Performance Issues

- Monitor database query performance in Supabase dashboard
- Use React DevTools Profiler for client-side performance
- Check bundle size with Next.js analyzer
- Monitor Core Web Vitals in production

## 📊 Monitoring & Analytics

### Application Monitoring

- Supabase built-in analytics
- Next.js built-in analytics
- Error tracking with Supabase functions
- Performance monitoring via Web Vitals

### Key Metrics

- Page load times
- API response times
- Database query performance
- User engagement metrics
- Blueprint download statistics
- VDK CLI integration usage
- Platform compatibility metrics
- Character limit truncation rates

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- **Next.js Team** - For the excellent React framework
- **Supabase Team** - For the comprehensive backend platform
- **shadcn** - For the beautiful UI component library
- **Tailwind CSS** - For the utility-first CSS framework
- **Vercel** - For the deployment platform
- **React Team** - For the foundational UI library

## 📞 Support

- **Documentation**: Check this README and inline code comments
- **Issues**: Open a GitHub issue for bugs or feature requests
- **Discussions**: Use GitHub Discussions for questions and ideas
- **Email**: Contact the maintainers for security issues

---

## 📋 VDK Standards Compliance

VDK Hub is fully compliant with **VDK Universal Standards v2.1.0**, ensuring seamless integration across the entire VDK ecosystem:

### ✅ **API Standards**

- **CLI Integration**: Full `/api/cli/sync/blueprints` endpoint compliance
- **Version Headers**: Consistent `X-VDK-Hub-Version: 2.1.0` across all endpoints
- **Response Format**: Standardized JSON responses with metadata and pagination
- **Health Checks**: VDK-compliant health endpoint for CLI connectivity

### ✅ **Blueprint Standards**

- **Universal Format**: MDC-compliant frontmatter with VDK metadata
- **Platform Compatibility**: Automatic adaptation for 33+ AI assistants
- **Character Limits**: Smart enforcement (Claude: 4K, Cursor/Windsurf: 6K, Copilot: 8K)
- **Relationship Support**: Full requires/suggests/conflicts/supersedes system

### ✅ **Community Standards**

- **Voting System**: Community validation with quality signals
- **Trending Algorithm**: Usage-based ranking with success rate weighting
- **Content Validation**: Security scanning and quality assessment
- **Attribution**: Proper author recognition and contribution tracking

### 🔄 **Sync Compatibility**

- **Incremental Sync**: Timestamp-based efficient synchronization
- **Change Detection**: Automatic added/updated/removed tracking
- **Platform Adaptation**: Real-time format conversion for target platforms
- **Rollback Support**: Version control and change history

---

## 🔗 VDK Ecosystem

This project is part of the VDK (Vibe Development Kit) ecosystem, fully compliant with universal standards v2.1.0:

- **[VDK CLI](https://github.com/entro314-labs/VDK-CLI)** - Command-line interface for VDK blueprint management
- **[VDK Hub](https://github.com/entro314-labs/VDK-Hub)** - Web platform for browsing and managing VDK blueprints ⭐ **You are here**
- **[VDK Blueprints](https://github.com/entro314-labs/VDK-Blueprints)** - Repository of AI assistant blueprints and community contributions
- **[VDK Wiki](https://wiki.vdk.tools)** - Comprehensive documentation and implementation guides

### 🎯 **VDK Hub Features**

- **Universal Compatibility**: Works with 33+ AI assistants and IDEs
- **Smart Platform Adaptation**: Automatic character limit enforcement (4K-8K range)
- **CLI Integration**: Seamless sync with VDK CLI via standardized endpoints
- **Community Driven**: User-generated blueprints with voting and trending systems
- **Enterprise Ready**: Team collaboration, analytics, and admin dashboards

**Built with ❤️ by the VDK Hub team**

---

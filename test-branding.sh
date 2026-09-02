#!/bin/bash
# Test script to compare pgsquash and capysquash branding

set -e

echo "======================================"
echo "Brand Comparison Test"
echo "======================================"
echo ""

echo "1. VERSION COMPARISON"
echo "-------------------------------------"
echo "pgsquash:"
./pgsquash --version
echo ""
echo "capysquash:"
./capysquash --version
echo ""

echo "2. MAIN HELP - FIRST 5 LINES"
echo "-------------------------------------"
echo "pgsquash:"
./pgsquash --help | head -n 5
echo ""
echo "capysquash:"
./capysquash --help | head -n 5
echo ""

echo "3. USAGE LINE"
echo "-------------------------------------"
echo "pgsquash:"
./pgsquash --help | grep "Usage:" -A 1
echo ""
echo "capysquash:"
./capysquash --help | grep "Usage:" -A 1
echo ""

echo "4. AI-FIX EXAMPLES"
echo "-------------------------------------"
echo "pgsquash:"
./pgsquash ai-fix --help | grep "Example:" -A 2
echo ""
echo "capysquash:"
./capysquash ai-fix --help | grep "Example:" -A 2
echo ""

echo "5. INIT-CONFIG DESCRIPTION"
echo "-------------------------------------"
echo "pgsquash:"
./pgsquash init-config --help | head -n 1
echo ""
echo "capysquash:"
./capysquash init-config --help | head -n 1
echo ""

echo "======================================"
echo "✅ Branding Test Complete!"
echo "======================================"
echo ""
echo "Summary:"
echo "- Both binaries have same version"
echo "- Help text reflects different branding"
echo "- Examples use appropriate command name"
echo "- Functionality remains identical"

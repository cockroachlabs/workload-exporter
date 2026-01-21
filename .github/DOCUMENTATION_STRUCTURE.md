# Documentation Structure

## Overview

The workload-exporter documentation has been organized to focus the main README on the primary audience (CockroachDB customers using pre-built releases) while providing comprehensive supporting documentation in the `docs/` directory.

## Main README (Customer-Focused)

**File:** `README.md`

**Target Audience:** CockroachDB customers who will download and use pre-built releases

**Focus Areas:**
- ✅ What the tool does
- ✅ Where to get pre-built binaries (releases page)
- ✅ Quick start with minimal flags
- ✅ Clear explanation of what data is collected
- ✅ How to inspect exports before sharing
- ✅ Privacy and security considerations
- ✅ Common use cases
- ✅ Links to detailed docs

**Key Sections:**
1. What It Does - Clear value proposition
2. Installation - Download links for macOS, Linux, Windows
3. Quick Start - Simple examples
4. Command Options - All available flags
5. What Data is Collected - Complete transparency
6. Inspecting the Export - How to review before sharing
7. Privacy and Security - Builds trust
8. Requirements - Prerequisites and permissions
9. Common Use Cases - Real-world examples
10. Getting Help - Troubleshooting and support links

## Supporting Documentation (`docs/`)

### For Users

#### `docs/TROUBLESHOOTING.md`
Comprehensive troubleshooting guide covering:
- Connection issues (SSL, authentication, network)
- Permission problems
- Time format errors
- Empty exports
- Debug logging
- Common command patterns

#### `docs/COMPATIBILITY.md`
Version compatibility information:
- Supported CockroachDB versions (24.1+)
- Version-specific behavior (26.1+ auto-enables allow_unsafe_internals)
- Required permissions per version
- Forward/backward compatibility
- Upgrade considerations

### For Developers

#### `docs/DEVELOPMENT.md`
Development and contribution guide:
- Building from source
- Development workflow
- Make commands
- Code structure
- Contributing guidelines
- Release process

#### `docs/TESTING.md`
Testing documentation:
- Unit tests
- Integration tests (cross-version)
- CI/CD integration
- Performance expectations
- Troubleshooting test issues

#### `docs/INTEGRATION_TEST_SUMMARY.md`
Quick reference for the integration test implementation

#### `docs/LIBRARY.md`
Go library usage:
- Installation
- API documentation
- Example code
- Advanced usage patterns
- Error handling

#### `docs/README.md`
Documentation index with links to all docs

## Documentation Principles

### 1. Customer-First README
The main README assumes the user:
- Wants to download a pre-built binary
- Needs to understand what data will be collected
- Wants to quickly get started
- May need to share exports with Cockroach Labs support
- Cares about privacy and security

### 2. Progressive Disclosure
- Essential information in main README
- Detailed information in topic-specific docs
- Clear navigation between docs

### 3. Transparency
- Explicit listing of all collected data
- Instructions for inspecting exports
- Privacy and security section
- No hidden behavior

### 4. Separation of Concerns
- **User docs** (troubleshooting, compatibility) separate from **developer docs** (development, testing, library)
- Each doc has a single, clear purpose
- Minimal duplication

## File Organization

```
workload-exporter/
├── README.md                          # Customer-focused main README
├── LICENSE                            # MIT license
├── Makefile                           # Build automation
├── go.mod                             # Go dependencies
├── cmd/                               # CLI code
├── pkg/                               # Core library code
├── docs/                              # All supporting documentation
│   ├── README.md                      # Documentation index
│   ├── COMPATIBILITY.md               # Version compatibility
│   ├── TROUBLESHOOTING.md             # Solutions to common issues
│   ├── DEVELOPMENT.md                 # Building and contributing
│   ├── TESTING.md                     # Testing guide
│   ├── INTEGRATION_TEST_SUMMARY.md    # Integration test reference
│   └── LIBRARY.md                     # Go library usage
└── .github/
    └── DOCUMENTATION_STRUCTURE.md     # This file
```

## Content Migration

### What Stayed in README
- Installation (download links)
- Basic usage examples
- What data is collected
- How to inspect exports
- Privacy/security assurances
- Common use cases

### What Moved to docs/
- **TROUBLESHOOTING.md** - All troubleshooting content
- **COMPATIBILITY.md** - Version compatibility details
- **DEVELOPMENT.md** - Building from source, contributing
- **LIBRARY.md** - Using as a Go library
- **TESTING.md** - Testing guides (already existed)

## Benefits of This Structure

### For Customers
✅ Quick path to download and use the tool
✅ Clear understanding of what data is collected
✅ Transparency builds trust
✅ Easy to find troubleshooting help
✅ Links to detailed docs when needed

### For Developers
✅ Development docs separated from user docs
✅ Clear contribution guidelines
✅ Testing documentation preserved
✅ Library usage well-documented

### For Maintainers
✅ Easier to update specific topics
✅ Less duplication
✅ Clear organization
✅ README stays focused and concise

## Navigation

All docs include:
- Clear links to related documentation
- Link back to main README
- Link to GitHub issues for support
- Link to releases page

The main README links to:
- All topic-specific docs in "Additional Documentation" section
- Troubleshooting guide in "Getting Help" section
- Compatibility guide for version-specific behavior

## Keeping Documentation Current

When making changes:

1. **README updates** - Ensure main README stays customer-focused
2. **Topic docs** - Update specific docs in `docs/` for detailed changes
3. **Cross-references** - Update links when moving content
4. **Index** - Update `docs/README.md` if adding new docs
5. **Version info** - Update `COMPATIBILITY.md` for version-specific changes

## Review Checklist

When updating documentation:

- [ ] Is the main README still customer-focused?
- [ ] Are download links prominent?
- [ ] Is it clear what data is collected?
- [ ] Are privacy/security concerns addressed?
- [ ] Do links to docs/ work correctly?
- [ ] Is technical/developer content in docs/?
- [ ] Is the docs/README.md index up to date?
- [ ] Are examples tested and working?

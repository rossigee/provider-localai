# Pre-Commit Hooks Setup Guide

This project uses [pre-commit](https://pre-commit.com/) to automatically validate and fix code before commits.

## Why Pre-Commit Hooks?

- **Prevent lint issues** from being committed
- **Enforce code formatting** consistently
- **Catch security issues** before push
- **Reduce CI/CD failures** by fixing issues locally first
- **Save time** - automatic fixes applied before commit

## Installation

### 1. Install pre-commit Framework

```bash
pip install pre-commit
# or on macOS with Homebrew:
brew install pre-commit
```

### 2. Install the Git Hook Scripts

```bash
cd /path/to/provider-keycloak
pre-commit install
pre-commit install --hook-type push  # For security checks at push time
```

### 3. Verify Installation

```bash
pre-commit run --all-files
```

## Usage

### Automatic (Recommended)

Once installed, hooks run automatically on `git commit`:

```bash
git add .
git commit -m "your message"
# Hooks run automatically before commit
```

If any checks fail:
1. Hooks automatically fix what they can (fmt, trailing whitespace, etc.)
2. You must manually fix remaining issues
3. `git add` the fixed files
4. `git commit` again

### Manual

Run hooks on demand:

```bash
# Run all hooks on all files
pre-commit run --all-files

# Run specific hook
pre-commit run go-fmt --all-files
pre-commit run golangci-lint --all-files

# Update hook versions
pre-commit autoupdate
```

## What Each Hook Does

### Commit-Time Hooks (Run Before `git commit`)

| Hook | Purpose | Auto-Fix? |
|------|---------|-----------|
| `go-fmt` | Format Go code | ✅ Yes |
| `go-vet` | Analyze Go code for errors | ❌ Manual fix |
| `golangci-lint` | Comprehensive linting | ⚠️ Some fixes |
| `trailing-whitespace` | Remove trailing spaces | ✅ Yes |
| `end-of-file-fixer` | Fix file endings | ✅ Yes |
| `check-yaml` | Validate YAML syntax | ❌ Manual fix |
| `check-added-large-files` | Prevent large files (>500KB) | ❌ Manual fix |
| `check-merge-conflict` | Detect merge conflict markers | ❌ Manual fix |
| `markdownlint` | Check markdown formatting | ⚠️ Some fixes |
| `prettier` | Format YAML/JSON | ✅ Yes |

### Push-Time Hooks (Run Before `git push`)

| Hook | Purpose |
|------|---------|
| `semgrep` | Security vulnerability scanning |

## Common Workflows

### First-Time Setup

```bash
# Install pre-commit
pip install pre-commit

# Install hooks in this repo
cd /path/to/provider-keycloak
pre-commit install
pre-commit install --hook-type push

# Run on all existing files to fix any issues
pre-commit run --all-files
```

### Normal Development

```bash
# Make changes
nano apis/openidclient/v1beta1/types.go

# Stage changes
git add apis/openidclient/v1beta1/types.go

# Commit - hooks run automatically
git commit -m "docs: update client types"

# If hooks fail, they show what to fix
# Fix manually if needed, then:
git add .
git commit -m "docs: update client types"
```

### Skipping Hooks (Not Recommended)

```bash
# Skip commit hooks
git commit --no-verify

# Skip push hooks
git push --no-verify
```

**Note:** Only skip when absolutely necessary. Pre-commit hooks prevent issues from reaching CI/CD.

## Troubleshooting

### Hook Fails With "Command Not Found"

```bash
# Install missing tool, e.g., golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Or for system-level tools
brew install [tool-name]

# Then retry
pre-commit run --all-files
```

### Hook Takes Too Long

Some hooks (especially linting) can be slow on first run. Subsequent runs are cached.

```bash
# Clear cache to ensure fresh check
pre-commit clean
pre-commit run --all-files
```

### Hooks Modify Files I Didn't Change

Some hooks auto-fix formatting issues in all files they scan. This is intentional to maintain consistency.

```bash
# Review changes
git diff

# Stage the auto-fixed files
git add .

# Commit with the formatting fixes
git commit --amend --no-edit
```

### Specific Hook Not Running

```bash
# Check hook configuration
cat .pre-commit-config.yaml

# Run specific hook
pre-commit run [hook-id] --all-files

# Update hook versions
pre-commit autoupdate
```

## Customization

### Disable Specific Hooks

Edit `.pre-commit-config.yaml` and set `enabled: false`:

```yaml
- repo: ...
  hooks:
    - id: markdownlint
      enabled: false  # Disable this hook
```

### Modify Hook Arguments

```yaml
- repo: local
  hooks:
    - id: golangci-lint
      entry: golangci-lint run --fix --allow-serial-runners
      # Changed arguments here
```

### Skip Hooks for Specific Files

```yaml
- repo: ...
  hooks:
    - id: check-yaml
      exclude: ^vendor/  # Skip vendor directory
```

## CI/CD Integration

Pre-commit runs **locally** before push. CI/CD should also enforce:

```bash
# In CI pipeline
pre-commit run --all-files --hook-stage push
```

This ensures nothing slips through if developers skip local hooks.

## Best Practices

1. **Install immediately** - Catch issues from the start
2. **Commit frequently** - Smaller commits are easier to review
3. **Review hook changes** - Some hooks auto-fix; review diffs
4. **Don't skip hooks** - They're there to help
5. **Keep hooks updated** - Run `pre-commit autoupdate` quarterly

## Git Commit Workflow with Pre-Commit

```bash
# 1. Make changes
nano file.go

# 2. Stage changes
git add file.go

# 3. Commit (hooks run automatically)
git commit -m "fix: something"
# Pre-commit runs...
# If it fails, fix and try again
# If it passes, commit is created

# 4. Push (security hooks run)
git push origin feature-branch
# Semgrep runs...
# If it fails, fix and try again
# If it passes, push completes
```

## Hooks Configuration Reference

### Stages

- `commit`: Run before `git commit`
- `push`: Run before `git push`  
- `manual`: Run only with `pre-commit run [hook-id]`

### Types

- `go`: Go files
- `yaml`: YAML files
- `json`: JSON files
- `markdown`: Markdown files

## Additional Resources

- [pre-commit documentation](https://pre-commit.com/)
- [Available hooks](https://pre-commit.com/hooks.html)
- [Creating custom hooks](https://pre-commit.com/#creating-new-hooks)

## Support

If you encounter issues with pre-commit:

1. Check `.pre-commit-config.yaml` syntax
2. Run `pre-commit clean && pre-commit run --all-files`
3. Update hooks: `pre-commit autoupdate`
4. Reinstall: `pre-commit uninstall && pre-commit install`

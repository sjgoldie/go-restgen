#!/bin/bash
set -e

echo "Setting up development environment..."

# Check if pre-commit is installed
if ! command -v pre-commit &> /dev/null; then
    echo "Installing pre-commit..."
    if command -v brew &> /dev/null; then
        brew install pre-commit
    elif command -v pip3 &> /dev/null; then
        pip3 install pre-commit
    else
        echo "Please install pre-commit manually: https://pre-commit.com/#install"
        exit 1
    fi
fi

# Install pre-commit hooks
echo "Installing pre-commit hooks..."
pre-commit install

# Install golangci-lint
if ! command -v golangci-lint &> /dev/null; then
    echo "Installing golangci-lint..."
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
fi

# Install goimports
if ! command -v goimports &> /dev/null; then
    echo "Installing goimports..."
    go install golang.org/x/tools/cmd/goimports@latest
fi

# Install Bruno CLI (optional)
if command -v npm &> /dev/null; then
    echo "Installing Bruno CLI..."
    npm install -g @usebruno/cli
else
    echo "Skipping Bruno CLI (npm not found)"
fi

echo ""
echo "✅ Development environment setup complete!"
echo ""
echo "Next steps:"
echo "  - Run 'go test ./...' to verify tests pass"
echo "  - Run 'golangci-lint run' to check code quality"
echo "  - Pre-commit hooks will run automatically on git commit"
echo ""

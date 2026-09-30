# The VS Code extension (editors/vscode) starts `mydumper-lint server`. It needs
# Node 22 or later (@vscode/vsce) and npm. Packages (*.vsix), the executable
# they bundle (editors/vscode/bin/) and the VS Code the tests download
# (editors/vscode/.vscode-test/) are not committed.
#
# A release (.github/workflows/release.yml) packages one VSIX per platform from
# goreleaser's archives with editors/vscode/scripts/package-release.sh, and
# publishes them to the Visual Studio Marketplace and Open VSX.

VSCODE_DIR := editors/vscode
# This machine's VS Code target (vsce --target), from Go's platform names.
VSCODE_OS := $(subst windows,win32,$(shell $(GO) env GOOS))
VSCODE_TARGET ?= $(VSCODE_OS)-$(subst amd64,x64,$(shell $(GO) env GOARCH))
VSCODE_EXE := mydumper-lint$(if $(filter win32,$(VSCODE_OS)),.exe)
VSCODE_HOST_VSIX := mydumper-lint-$(VSCODE_TARGET).vsix
# VS Code for make vscode-test: "stable", or a version such as 1.91.0 (the
# oldest the extension supports, engines.vscode in package.json).
VSCODE_TEST_VERSION ?= stable
# VS Code needs a display: on Linux without one, a virtual one.
VSCODE_XVFB := $(if $(and $(filter linux,$(VSCODE_OS)),$(if $(DISPLAY),,y)),xvfb-run -a)

.PHONY: vscode vscode-deps vscode-host vscode-test vscode-release-check vscode-icon

vscode-deps:
	cd $(VSCODE_DIR) && npm ci --no-audit --no-fund

vscode: vscode-deps ## Test the VS Code extension and package it without executable (editors/vscode/mydumper-lint.vsix)
	rm -rf $(VSCODE_DIR)/bin
	cd $(VSCODE_DIR) && npm test && npm run package

vscode-host: vscode-deps ## Package the extension for this machine with a mydumper-lint built from the tree
	rm -rf $(VSCODE_DIR)/bin
	CGO_ENABLED=0 $(GO) build -trimpath -o $(VSCODE_DIR)/bin/$(VSCODE_EXE) ./cmd/mydumper-lint
	cd $(VSCODE_DIR) && node_modules/.bin/vsce package --no-dependencies --target $(VSCODE_TARGET) --out $(VSCODE_HOST_VSIX)
	rm -rf $(VSCODE_DIR)/bin

vscode-test: vscode-host ## Run the packaged extension in VS Code (downloaded) on a sample workspace
	cd $(VSCODE_DIR) && VSIX=$(VSCODE_HOST_VSIX) VSCODE_TEST_VERSION=$(VSCODE_TEST_VERSION) \
		$(VSCODE_XVFB) node test/vscode/run.js

# goreleaser's archives only (no image, signature or SBOM), then every
# platform's VSIX from them, as release.yml does: into dist/vscode/.
vscode-release-check: vscode-deps ## Package the extension for every platform from a goreleaser snapshot (dist/vscode)
	$(GORELEASER) release --snapshot --clean --skip=docker,sign,sbom
	SNAPSHOT=1 $(VSCODE_DIR)/scripts/package-release.sh dist dist/vscode

vscode-icon: ## Render editors/vscode/images/icon.png from icon.svg (rsvg-convert)
	rsvg-convert -w 128 -h 128 $(VSCODE_DIR)/images/icon.svg -o $(VSCODE_DIR)/images/icon.png
